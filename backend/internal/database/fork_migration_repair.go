package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"

	"github.com/pressly/goose/v3"
)

// This fork's GitOps commit-injection migration has shipped under eight version
// numbers that upstream later claimed for its own migrations:
//
//   - Fork commits f3b8e1e..130b45f (2026-08-01..2026-08-03) shipped it as 069.
//     Upstream then claimed 069 (container_registries.repository_names) and 070
//     (passkeys/MFA), so the fork migration was renumbered to 071.
//   - Fork builds between 130b45f and the 2026-08-15 rebase shipped it as 071.
//     Upstream then claimed 071 (volume-workspace legacy key renames) and 072
//     (project tags), so the fork migration was renumbered again, to 073.
//   - Fork builds between the 2026-08-15 and 2026-08-22 rebases shipped it as
//     073. Upstream then claimed 073 (S3/system backup support), so the fork
//     migration was renumbered a third time, to 074.
//   - Fork builds between the 2026-08-22 and 2026-08-29 rebases shipped it as
//     074. Upstream then claimed 074 (GitOps pull/redeploy-after-sync flags),
//     075 and 076, so the fork migration was renumbered a fourth time, to 077.
//   - Fork builds between the 2026-08-29 and 2026-09-09 rebases shipped it as
//     077. Upstream then claimed 077 (Apple push notification devices/outbox)
//     through 084, so the fork migration was renumbered a fifth time, to 085.
//   - Fork builds between the 2026-09-09 and 2026-09-19 rebases shipped it as
//     085. Upstream then claimed 085 (GitOps backup mode), 086 (git repository
//     commit identity) and 087 (volume backup remote instance), so the fork
//     migration was renumbered a sixth time, to 088.
//   - Fork builds between the 2026-09-19 and 2026-09-26 rebases shipped it as
//     088. Upstream then claimed 088 (vulnerability risk scoring) and 089
//     (vulnerability scoring version), so the fork migration was renumbered a
//     seventh time, to 090.
//   - Fork builds between the 2026-09-26 and 2026-10-10 rebases shipped it as
//     090. Upstream then claimed 090 (events.deduplication_key), 091 (role
//     assignment uniqueness by source), 092 (orphaned authorization records)
//     and 093 (template registry icon_url), so the fork migration was
//     renumbered an eighth time, to 094.
//
// Goose keys its bookkeeping on the version number alone, so a database migrated
// by a build from any of those windows is wrong in two ways: the upstream
// migration that now owns the recorded number is treated as applied and silently
// skipped, and gitops_syncs.inject_commit_env already exists, so re-running the
// fork migration under its new number aborts with "duplicate column name:
// inject_commit_env" and Arcane refuses to start.
//
// repairPreRenumberForkMigrationInternal detects all of those states and repairs
// them in place, without touching the operator's data: it applies everything
// below the fork migration's current number through Goose, replays the upstream
// migration(s) the stale bookkeeping made Goose skip, and records the fork
// migration as applied instead of re-running its DDL. It runs outside Goose's
// Postgres session lock, which is harmless: a second instance re-runs the same
// checks and finds nothing to do, and the worst a genuine race can leave behind
// is a duplicate version row, which Goose collapses when it reads its state. It
// can be deleted once no database migrated by a pre-094 fork build is left in
// the wild.
//
// The 094 renumbering added a second failure mode that no earlier era had, and
// it is dialect-specific. Upstream's SQLite 092 rebuilds gitops_syncs from an
// explicit column list that cannot name inject_commit_env, so the Goose run
// drops the column — and the operator's per-sync values with it — before 094
// can re-add it. On SQLite the repair therefore lets 094 run for real and
// restores the captured values afterwards; on Postgres, whose 092 is a plain
// DELETE sweep, the column survives and 094 is recorded as applied as before.
const (
	forkCommitEnvMigrationVersion             int64 = 94
	forkCommitEnvDeduplicationRenumberVersion int64 = 90
	forkCommitEnvRiskRenumberVersion          int64 = 88
	forkCommitEnvBackupModeRenumberVersion    int64 = 85
	forkCommitEnvApnsRenumberVersion          int64 = 77
	forkCommitEnvLastRenumberVersion          int64 = 74
	forkCommitEnvLateRenumberVersion          int64 = 73
	forkCommitEnvMidRenumberVersion           int64 = 71
	forkCommitEnvPreRenumberVersion           int64 = 69
)

type sqlExecerInternal interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// forkCommitEnvRenumberErasInternal records which of the fork migration's old
// version numbers a database has recorded as applied. Each true flag means Goose
// will skip the upstream migration that now owns that number, so the repair has
// to replay it by hand.
type forkCommitEnvRenumberErasInternal struct {
	mid           bool // 71 — upstream's volume-workspace legacy key renames
	late          bool // 73 — upstream's S3/system backup support
	last          bool // 74 — upstream's GitOps pull/redeploy-after-sync flags
	apns          bool // 77 — upstream's Apple push notification tables
	backupMode    bool // 85 — upstream's GitOps backup mode
	risk          bool // 88 — upstream's vulnerability risk scoring tables
	deduplication bool // 90 — upstream's events.deduplication_key
}

func repairPreRenumberForkMigrationInternal(ctx context.Context, db *sql.DB, dbProvider string, provider *goose.Provider, currentVersion, requiredVersion int64) error {
	if requiredVersion < forkCommitEnvMigrationVersion || currentVersion >= forkCommitEnvMigrationVersion {
		return nil
	}

	needsRepair, err := hasPreRenumberForkMigrationStateInternal(ctx, db, dbProvider, currentVersion)
	if err != nil || !needsRepair {
		return err
	}

	// Decide *before* the UpTo below which of the old numbers are already
	// recorded: on a 069-era database none is, and the UpTo applies upstream's
	// real migrations itself (recording them); on any later era the recorded
	// number belongs to the fork's own migration, so Goose skips the upstream
	// migration that now owns it and its statements have to be replayed by hand.
	eras, err := recordedRenumberEraVersionsInternal(ctx, db, dbProvider)
	if err != nil {
		return err
	}

	// Captured before the Goose run, because upstream's SQLite 092 rebuilds
	// gitops_syncs without inject_commit_env and takes the values with it.
	enabledSyncIDs, err := injectCommitEnvEnabledSyncIDsInternal(ctx, db)
	if err != nil {
		return err
	}

	slog.WarnContext(ctx, "Detected a database migrated by a pre-renumber fork build; repairing migration state",
		"provider", dbProvider, "currentVersion", currentVersion, "forkMigrationVersion", forkCommitEnvMigrationVersion)

	if err := replaySkippedMigrationsBeforeUpToInternal(ctx, db, dbProvider, eras); err != nil {
		return err
	}

	// Everything below the fork migration has to be applied first: recording
	// version 94 below raises the Goose version past the intermediate
	// migrations, after which UpTo would skip them.
	belowForkVersion := forkCommitEnvMigrationVersion - 1
	if currentVersion < belowForkVersion {
		if _, err := provider.UpTo(ctx, belowForkVersion); err != nil {
			return fmt.Errorf("failed to apply embedded Goose migrations up to version %d for %s while repairing pre-renumber fork migration state: %w", belowForkVersion, dbProvider, err)
		}
	}

	repositoryNamesPresent, err := columnExistsInternal(ctx, db, dbProvider, "container_registries", "repository_names")
	if err != nil {
		return err
	}

	// Decide outside the transaction which columns the guarded replays still have
	// to add: SQLite's ALTER TABLE has no IF NOT EXISTS, and on a database where a
	// crashed earlier repair already let Goose apply the real migration, re-adding
	// an existing column would abort the whole repair.
	missingColumns, err := missingReplayColumnsInternal(ctx, db, dbProvider, eras)
	if err != nil {
		return err
	}

	// Upstream's SQLite 092 has just dropped the column, so the fork migration
	// has real work to do and must not be recorded as a no-op.
	forkColumnPresent, err := columnExistsInternal(ctx, db, dbProvider, "gitops_syncs", "inject_commit_env")
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start pre-renumber fork migration repair transaction for %s: %w", dbProvider, err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if !repositoryNamesPresent {
		if err := addSkippedRegistryRepositoryNamesColumnInternal(ctx, tx, dbProvider); err != nil {
			return err
		}
	}

	if err := replaySkippedUpstreamMigrationsInternal(ctx, tx, dbProvider, eras, missingColumns); err != nil {
		return err
	}

	// The column the fork migration adds is already present, so record it as
	// applied rather than re-running its DDL, which would fail on the duplicate
	// column. When 092 dropped it, 094 is left for Goose to apply for real.
	if forkColumnPresent {
		if err := insertGooseMigrationVersionInternal(ctx, tx, dbProvider, forkCommitEnvMigrationVersion); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit pre-renumber fork migration repair for %s: %w", dbProvider, err)
	}

	if !forkColumnPresent {
		if err := reapplyForkCommitEnvColumnInternal(ctx, db, dbProvider, provider, enabledSyncIDs); err != nil {
			return err
		}
	}

	slog.InfoContext(ctx, "Repaired pre-renumber fork migration state",
		"provider", dbProvider, "forkMigrationVersion", forkCommitEnvMigrationVersion,
		"restoredSkippedMigration", !repositoryNamesPresent, "replayedVolumeWorkspaceRename", eras.mid,
		"replayedBackupSupport", eras.late, "replayedPullRedeploy", eras.last, "replayedApns", eras.apns,
		"replayedGitOpsBackupMode", eras.backupMode, "replayedVulnerabilityRisk", eras.risk,
		"replayedEventDeduplication", eras.deduplication,
		"reappliedForkColumn", !forkColumnPresent, "restoredInjectCommitEnvSyncs", len(enabledSyncIDs))
	return nil
}

// injectCommitEnvEnabledSyncIDsInternal lists the syncs with commit injection
// switched on. A database that has not shipped the column yet yields none.
func injectCommitEnvEnabledSyncIDsInternal(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT id FROM gitops_syncs WHERE inject_commit_env")
	if err != nil {
		// The column is what the whole repair keys on, so its absence here means
		// a shape this function has nothing to preserve.
		return nil, nil
	}
	defer func() {
		_ = rows.Close()
	}()

	var ids []string
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("failed to read commit-injection sync ids: %w", scanErr)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate commit-injection sync ids: %w", err)
	}
	return ids, nil
}

// reapplyForkCommitEnvColumnInternal lets Goose apply the fork migration for real
// after upstream's SQLite 092 rebuilt gitops_syncs without the column, then
// restores the per-sync values captured before the rebuild.
func reapplyForkCommitEnvColumnInternal(ctx context.Context, db *sql.DB, dbProvider string, provider *goose.Provider, enabledSyncIDs []string) error {
	if _, err := provider.UpTo(ctx, forkCommitEnvMigrationVersion); err != nil {
		return fmt.Errorf("failed to re-apply the fork commit-injection migration %d for %s after upstream rebuilt gitops_syncs: %w", forkCommitEnvMigrationVersion, dbProvider, err)
	}

	for _, id := range enabledSyncIDs {
		query, args, err := sqlWithProviderPlaceholderInternal(dbProvider, "UPDATE gitops_syncs SET inject_commit_env = true WHERE id = %s", id)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to restore commit injection for sync %s on %s: %w", id, dbProvider, err)
		}
	}
	return nil
}

// recordedRenumberEraVersionsInternal reports which of the fork migration's old
// version numbers (71, 73, 74, 77, 85, 88, 90) are recorded as applied — for
// each, the signature of the corresponding renumber era whose skipped upstream
// migration the repair has to replay by hand.
func recordedRenumberEraVersionsInternal(ctx context.Context, db *sql.DB, dbProvider string) (forkCommitEnvRenumberErasInternal, error) {
	var eras forkCommitEnvRenumberErasInternal
	for _, era := range []struct {
		version int64
		flag    *bool
	}{
		{forkCommitEnvMidRenumberVersion, &eras.mid},
		{forkCommitEnvLateRenumberVersion, &eras.late},
		{forkCommitEnvLastRenumberVersion, &eras.last},
		{forkCommitEnvApnsRenumberVersion, &eras.apns},
		{forkCommitEnvBackupModeRenumberVersion, &eras.backupMode},
		{forkCommitEnvRiskRenumberVersion, &eras.risk},
		{forkCommitEnvDeduplicationRenumberVersion, &eras.deduplication},
	} {
		applied, err := gooseMigrationVersionAppliedInternal(ctx, db, dbProvider, era.version)
		if err != nil {
			return forkCommitEnvRenumberErasInternal{}, err
		}
		*era.flag = applied
	}
	return eras, nil
}

// forkCommitEnvReplayColumnsInternal carries the column lists the guarded replays
// still have to add, computed before the repair transaction opens.
type forkCommitEnvReplayColumnsInternal struct {
	backupSupport []string
	deduplication []string
}

// missingReplayColumnsInternal probes the columns each recorded era's replay would
// add, so the replays inside the transaction can skip the ones already present.
func missingReplayColumnsInternal(ctx context.Context, db *sql.DB, dbProvider string, eras forkCommitEnvRenumberErasInternal) (forkCommitEnvReplayColumnsInternal, error) {
	var missing forkCommitEnvReplayColumnsInternal
	var err error
	if eras.late {
		if missing.backupSupport, err = missingBackupSupportColumnsInternal(ctx, db, dbProvider); err != nil {
			return forkCommitEnvReplayColumnsInternal{}, err
		}
	}
	if eras.deduplication {
		if missing.deduplication, err = missingEventDeduplicationColumnsInternal(ctx, db, dbProvider); err != nil {
			return forkCommitEnvReplayColumnsInternal{}, err
		}
	}
	return missing, nil
}

// replaySkippedUpstreamMigrationsInternal replays, for every recorded era, the
// upstream migration Goose will skip because that version number is already
// recorded for the fork's own migration. Every replay is idempotent — guarded by
// IF NOT EXISTS or by the pre-computed missing-column lists — so replaying on a
// database that already carries the schema (a crashed earlier repair) is a no-op.
func replaySkippedUpstreamMigrationsInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string, eras forkCommitEnvRenumberErasInternal, missing forkCommitEnvReplayColumnsInternal) error {
	// 071: volume-workspace legacy key renames.
	if eras.mid {
		if err := replaySkippedVolumeWorkspaceRenameInternal(ctx, execer, dbProvider); err != nil {
			return err
		}
	}
	// 073: S3/system backup support.
	if eras.late {
		if err := replaySkippedBackupSupportInternal(ctx, execer, dbProvider, missing.backupSupport); err != nil {
			return err
		}
	}
	// 077: Apple push notification devices and outbox.
	if eras.apns {
		if err := replaySkippedApnsInternal(ctx, execer, dbProvider); err != nil {
			return err
		}
	}
	// 090: event deduplication key.
	if eras.deduplication {
		if err := replaySkippedEventDeduplicationInternal(ctx, execer, missing.deduplication); err != nil {
			return err
		}
	}
	return nil
}

// missingEventDeduplicationColumnsInternal probes the one column upstream's 090
// adds, so the replay can skip it on a database that already has it.
func missingEventDeduplicationColumnsInternal(ctx context.Context, db *sql.DB, dbProvider string) ([]string, error) {
	var missing []string
	present, err := columnExistsInternal(ctx, db, dbProvider, "events", "deduplication_key")
	if err != nil {
		return nil, err
	}
	if !present {
		missing = append(missing, "deduplication_key")
	}
	return missing, nil
}

// replaySkippedEventDeduplicationInternal replays upstream's 090, which both
// dialects write identically. The index carries an IF NOT EXISTS guard the
// migration itself does not write, so a crashed earlier repair re-runs as a no-op.
func replaySkippedEventDeduplicationInternal(ctx context.Context, execer sqlExecerInternal, missingColumns []string) error {
	for _, column := range missingColumns {
		if column != "deduplication_key" {
			continue
		}
		if _, err := execer.ExecContext(ctx, "ALTER TABLE events ADD COLUMN deduplication_key TEXT"); err != nil {
			return fmt.Errorf("failed to replay skipped event deduplication column: %w", err)
		}
	}
	if _, err := execer.ExecContext(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS idx_events_deduplication_key ON events (deduplication_key)"); err != nil {
		return fmt.Errorf("failed to replay skipped event deduplication index: %w", err)
	}
	return nil
}

func appliedLiteralInternal(dbProvider string) string {
	if dbProvider == dbProviderPostgres {
		return "true"
	}
	return "1"
}

func sqlWithProviderPlaceholderInternal(dbProvider, queryFormat string, arg any) (string, []any, error) {
	switch dbProvider {
	case dbProviderSQLite:
		return fmt.Sprintf(queryFormat, "?"), []any{arg}, nil
	case dbProviderPostgres:
		return fmt.Sprintf(queryFormat, "$1"), []any{arg}, nil
	default:
		return "", nil, fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
}

func insertGooseMigrationVersionInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string, version int64) error {
	switch dbProvider {
	case dbProviderSQLite:
		if _, err := execer.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (version_id, is_applied) VALUES (?, ?)", gooseVersionTable), version, true); err != nil {
			return fmt.Errorf("failed to insert Goose migration version %d for sqlite: %w", version, err)
		}
	case dbProviderPostgres:
		if _, err := execer.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (version_id, is_applied) VALUES ($1, $2)", gooseVersionTable), version, true); err != nil {
			return fmt.Errorf("failed to insert Goose migration version %d for postgres: %w", version, err)
		}
	default:
		return fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
	return nil
}

func gooseMigrationVersionAppliedInternal(ctx context.Context, db *sql.DB, dbProvider string, version int64) (bool, error) {
	queryFormat := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE is_applied = %s AND version_id = %%s", gooseVersionTable, appliedLiteralInternal(dbProvider))
	query, args, err := sqlWithProviderPlaceholderInternal(dbProvider, queryFormat, version)
	if err != nil {
		return false, err
	}

	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to check Goose migration version %d for %s: %w", version, dbProvider, err)
	}
	return count > 0, nil
}

// columnExistsInternal reports whether a column exists, returning false rather than an
// error when the table itself is absent.
// gooseVersionTableExistsInternal reports whether Goose's bookkeeping table is
// present; a database that has never been migrated has nothing to repair.
func gooseVersionTableExistsInternal(ctx context.Context, db *sql.DB, dbProvider string) (bool, error) {
	switch dbProvider {
	case dbProviderSQLite:
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, gooseVersionTable).Scan(&count); err != nil {
			return false, fmt.Errorf("failed to check Goose version table for sqlite: %w", err)
		}
		return count > 0, nil
	case dbProviderPostgres:
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, gooseVersionTable).Scan(&exists); err != nil {
			return false, fmt.Errorf("failed to check Goose version table for postgres: %w", err)
		}
		return exists, nil
	default:
		return false, fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
}

func columnExistsInternal(ctx context.Context, db *sql.DB, dbProvider, table, column string) (bool, error) {
	switch dbProvider {
	case dbProviderSQLite:
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count); err != nil {
			return false, fmt.Errorf("failed to inspect column %s.%s for sqlite: %w", table, column, err)
		}
		return count > 0, nil
	case dbProviderPostgres:
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2)`, table, column).Scan(&exists); err != nil {
			return false, fmt.Errorf("failed to inspect column %s.%s for postgres: %w", table, column, err)
		}
		return exists, nil
	default:
		return false, fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
}

func missingBackupSupportColumnsInternal(ctx context.Context, db *sql.DB, dbProvider string) ([]string, error) {
	var missing []string
	for _, column := range backupSupportVolumeBackupColumnsInternal {
		present, err := columnExistsInternal(ctx, db, dbProvider, "volume_backups", column.name)
		if err != nil {
			return nil, err
		}
		if !present {
			missing = append(missing, column.name)
		}
	}
	return missing, nil
}

// hasPreRenumberForkMigrationStateInternal reports whether gitops_syncs.inject_commit_env
// exists without version 90 being recorded — the signature of a fork build that applied
// that migration under one of its old version numbers.
func hasPreRenumberForkMigrationStateInternal(ctx context.Context, db *sql.DB, dbProvider string, currentVersion int64) (bool, error) {
	if currentVersion < forkCommitEnvPreRenumberVersion {
		return false, nil
	}

	gooseStateExists, err := gooseVersionTableExistsInternal(ctx, db, dbProvider)
	if err != nil || !gooseStateExists {
		return false, err
	}

	forkVersionApplied, err := gooseMigrationVersionAppliedInternal(ctx, db, dbProvider, forkCommitEnvMigrationVersion)
	if err != nil || forkVersionApplied {
		return false, err
	}

	// The fork column still being present is the ordinary signature: the fork
	// migration ran under an old number and its current number is unrecorded.
	columnPresent, err := columnExistsInternal(ctx, db, dbProvider, "gitops_syncs", "inject_commit_env")
	if err != nil || columnPresent {
		return columnPresent, err
	}

	// Upstream's SQLite 092 rebuilds gitops_syncs without the column, so a
	// database that got partway through the Goose run has lost the evidence
	// above while still carrying the damage. Fall back to the fingerprint that
	// a clean upstream database can never show.
	return forkRenumberDamageDetectedInternal(ctx, db, dbProvider)
}

// forkRenumberDamageDetectedInternal reports whether an upstream migration whose
// version number the fork once used is recorded as applied while the schema that
// migration creates is absent. That combination is unambiguous: a clean upstream
// database records the same version numbers but carries their schema, and only a
// pre-renumber fork build could have recorded one of them for its own migration.
//
// Era 071 is deliberately absent — it renames settings keys rather than adding
// schema, so there is nothing cheap to probe. A 071-era database that has also
// lost the fork column goes unrepaired; it is the one gap in this fallback.
func forkRenumberDamageDetectedInternal(ctx context.Context, db *sql.DB, dbProvider string) (bool, error) {
	for _, era := range []struct {
		version int64
		table   string
		column  string
	}{
		{forkCommitEnvPreRenumberVersion, "container_registries", "repository_names"},
		{forkCommitEnvLateRenumberVersion, "s3_destinations", "bucket"},
		{forkCommitEnvLastRenumberVersion, "gitops_syncs", "pull_image_after_sync"},
		{forkCommitEnvApnsRenumberVersion, "apns_devices", "recipient_id"},
		{forkCommitEnvBackupModeRenumberVersion, "gitops_syncs", "mode"},
		{forkCommitEnvRiskRenumberVersion, "vulnerability_risk_snapshots", "snapshot_date"},
		{forkCommitEnvDeduplicationRenumberVersion, "events", "deduplication_key"},
	} {
		applied, err := gooseMigrationVersionAppliedInternal(ctx, db, dbProvider, era.version)
		if err != nil {
			return false, err
		}
		if !applied {
			continue
		}
		present, err := columnExistsInternal(ctx, db, dbProvider, era.table, era.column)
		if err != nil {
			return false, err
		}
		if !present {
			return true, nil
		}
	}
	return false, nil
}

// addSkippedRegistryRepositoryNamesColumnInternal replays the one statement of
// 069_add_container_registry_repository_names.sql, which Goose skipped because the
// pre-renumber fork build had already recorded version 69.
func addSkippedRegistryRepositoryNamesColumnInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string) error {
	var query string
	switch dbProvider {
	case dbProviderSQLite:
		query = `ALTER TABLE container_registries ADD COLUMN repository_names TEXT NOT NULL DEFAULT '[]'`
	case dbProviderPostgres:
		query = `ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS repository_names TEXT NOT NULL DEFAULT '[]'`
	default:
		return fmt.Errorf("unsupported database provider: %s", dbProvider)
	}

	if _, err := execer.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to restore skipped container_registries.repository_names column for %s: %w", dbProvider, err)
	}
	return nil
}

// replaySkippedVolumeWorkspaceRenameInternal replays the Up statements of
// 071_rename_volume_workspace_legacy_keys.sql, which Goose skipped because a
// 071-era fork build had already recorded version 71 for its own migration. The
// statements are copied from that migration and, like it, are idempotent: each
// only touches rows still carrying a legacy name.
func replaySkippedVolumeWorkspaceRenameInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string) error {
	var queries []string
	switch dbProvider {
	case dbProviderSQLite:
		queries = []string{
			`INSERT OR IGNORE INTO settings (key, value)
SELECT 'volumeHelperIdleTimeout', value
FROM settings
WHERE key = 'volumeBrowserHelperIdleTimeout'`,
			`DELETE FROM settings WHERE key = 'volumeBrowserHelperIdleTimeout'`,
			`UPDATE roles
SET permissions = (
    SELECT json_group_array(permission)
    FROM (
        SELECT DISTINCT CASE value
            WHEN 'volumes:browse' THEN 'volumes:read'
            ELSE value
        END AS permission
        FROM json_each(roles.permissions)
    )
)
WHERE EXISTS (
    SELECT 1 FROM json_each(roles.permissions) WHERE value = 'volumes:browse'
)`,
			`DELETE FROM api_key_permissions AS legacy
WHERE legacy.permission = 'volumes:browse'
  AND EXISTS (
      SELECT 1
      FROM api_key_permissions AS current
      WHERE current.api_key_id = legacy.api_key_id
        AND current.permission = 'volumes:read'
        AND COALESCE(current.environment_id, '') = COALESCE(legacy.environment_id, '')
  )`,
			`UPDATE api_key_permissions
SET permission = 'volumes:read'
WHERE permission = 'volumes:browse'`,
		}
	case dbProviderPostgres:
		queries = []string{
			`INSERT INTO settings (key, value)
SELECT 'volumeHelperIdleTimeout', value
FROM settings
WHERE key = 'volumeBrowserHelperIdleTimeout'
ON CONFLICT (key) DO NOTHING`,
			`DELETE FROM settings WHERE key = 'volumeBrowserHelperIdleTimeout'`,
			`UPDATE roles
SET permissions = (
    SELECT jsonb_agg(permission ORDER BY permission)
    FROM (
        SELECT DISTINCT CASE value
            WHEN 'volumes:browse' THEN 'volumes:read'
            ELSE value
        END AS permission
        FROM jsonb_array_elements_text(roles.permissions)
    ) AS migrated_permissions
)
WHERE permissions ? 'volumes:browse'`,
			`DELETE FROM api_key_permissions AS legacy
WHERE legacy.permission = 'volumes:browse'
  AND EXISTS (
      SELECT 1
      FROM api_key_permissions AS current
      WHERE current.api_key_id = legacy.api_key_id
        AND current.permission = 'volumes:read'
        AND COALESCE(current.environment_id, '') = COALESCE(legacy.environment_id, '')
  )`,
			`UPDATE api_key_permissions
SET permission = 'volumes:read'
WHERE permission = 'volumes:browse'`,
		}
	default:
		return fmt.Errorf("unsupported database provider: %s", dbProvider)
	}

	for _, query := range queries {
		if _, err := execer.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to replay skipped volume-workspace rename migration for %s: %w", dbProvider, err)
		}
	}
	return nil
}

// backupSupportVolumeBackupColumnsInternal lists the columns
// 073_add_backup_support.sql adds to volume_backups. The definitions are
// identical in both dialects.
var backupSupportVolumeBackupColumnsInternal = []struct {
	name       string
	definition string
}{
	{"status", `TEXT NOT NULL DEFAULT 'succeeded'`},
	{"trigger", `TEXT NOT NULL DEFAULT 'manual'`},
	{"s3_destination_id", `TEXT`},
	{"error", `TEXT`},
	{"destination", `TEXT NOT NULL DEFAULT 'local'`},
	{"local_snapshot_id", `TEXT`},
	{"remote_snapshot_id", `TEXT`},
	{"policy_id", `TEXT`},
	// Pre-existing rows are tar.gz archive backups; new Rustic-snapshot rows set 'rustic'.
	{"format", `TEXT NOT NULL DEFAULT 'archive'`},
}

// replaySkippedBackupSupportInternal replays the Up statements of
// 073_add_backup_support.sql, which Goose skipped because a 073-era fork build
// had already recorded version 73 for its own migration. The statements are
// copied from that migration with IF NOT EXISTS guards added (and the
// volume_backups columns filtered to missingColumns, computed by the caller),
// so replaying on a database that already carries part or all of the schema —
// an earlier repair that crashed after Goose applied the real 073 — is a no-op.
func replaySkippedBackupSupportInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string, missingColumns []string) error {
	var tables []string
	switch dbProvider {
	case dbProviderSQLite:
		tables = []string{
			`CREATE TABLE IF NOT EXISTS s3_destinations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    endpoint TEXT,
    bucket TEXT NOT NULL,
    region TEXT NOT NULL,
    access_key_id TEXT NOT NULL,
    secret_access_key TEXT NOT NULL,
    prefix TEXT,
    use_ssl INTEGER NOT NULL DEFAULT 1,
    force_path_style INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
)`,
			`CREATE TABLE IF NOT EXISTS volume_backup_policies (
    id TEXT PRIMARY KEY,
    volume_name TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    stop_containers INTEGER NOT NULL DEFAULT 0,
    local_enabled INTEGER NOT NULL DEFAULT 1,
    s3_enabled INTEGER NOT NULL DEFAULT 0,
    s3_destination_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
)`,
			`CREATE TABLE IF NOT EXISTS system_backup_runs (
    id TEXT PRIMARY KEY,
    size INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    status TEXT NOT NULL,
    trigger TEXT NOT NULL,
    destination TEXT NOT NULL,
    local_snapshot_id TEXT,
    remote_snapshot_id TEXT,
    s3_destination_id TEXT,
    policy_id TEXT,
    error TEXT
)`,
			`CREATE TABLE IF NOT EXISTS system_backup_policies (
    id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    local_enabled INTEGER NOT NULL DEFAULT 1,
    s3_enabled INTEGER NOT NULL DEFAULT 0,
    s3_destination_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
)`,
			`CREATE TABLE IF NOT EXISTS system_backup_recovery_config (
    id TEXT PRIMARY KEY,
    encrypted_recovery_key TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
)`,
		}
	case dbProviderPostgres:
		tables = []string{
			`CREATE TABLE IF NOT EXISTS s3_destinations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    endpoint TEXT,
    bucket TEXT NOT NULL,
    region TEXT NOT NULL,
    access_key_id TEXT NOT NULL,
    secret_access_key TEXT NOT NULL,
    prefix TEXT,
    use_ssl BOOLEAN NOT NULL DEFAULT TRUE,
    force_path_style BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
)`,
			`CREATE TABLE IF NOT EXISTS volume_backup_policies (
    id TEXT PRIMARY KEY,
    volume_name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    stop_containers BOOLEAN NOT NULL DEFAULT FALSE,
    local_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    s3_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    s3_destination_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
)`,
			`CREATE TABLE IF NOT EXISTS system_backup_runs (
    id TEXT PRIMARY KEY,
    size BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ,
    status TEXT NOT NULL,
    trigger TEXT NOT NULL,
    destination TEXT NOT NULL,
    local_snapshot_id TEXT,
    remote_snapshot_id TEXT,
    s3_destination_id TEXT,
    policy_id TEXT,
    error TEXT
)`,
			`CREATE TABLE IF NOT EXISTS system_backup_policies (
    id TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    local_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    s3_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    s3_destination_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
)`,
			`CREATE TABLE IF NOT EXISTS system_backup_recovery_config (
    id TEXT PRIMARY KEY,
    encrypted_recovery_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
)`,
		}
	default:
		return fmt.Errorf("unsupported database provider: %s", dbProvider)
	}

	queries := tables
	for _, column := range backupSupportVolumeBackupColumnsInternal {
		if !slices.Contains(missingColumns, column.name) {
			continue
		}
		queries = append(queries, fmt.Sprintf(`ALTER TABLE volume_backups ADD COLUMN "%s" %s`, column.name, column.definition))
	}
	queries = append(queries,
		`CREATE INDEX IF NOT EXISTS idx_volume_backups_s3_destination_id ON volume_backups(s3_destination_id)`,
		`CREATE INDEX IF NOT EXISTS idx_volume_backups_policy_id ON volume_backups(policy_id)`,
		`CREATE INDEX IF NOT EXISTS idx_volume_backup_policies_volume_name ON volume_backup_policies(volume_name)`,
		`CREATE INDEX IF NOT EXISTS idx_volume_backup_policies_s3_destination_id ON volume_backup_policies(s3_destination_id)`,
		`CREATE INDEX IF NOT EXISTS idx_system_backup_runs_s3_destination_id ON system_backup_runs(s3_destination_id)`,
		`CREATE INDEX IF NOT EXISTS idx_system_backup_runs_policy_id ON system_backup_runs(policy_id)`,
		`CREATE INDEX IF NOT EXISTS idx_system_backup_policies_s3_destination_id ON system_backup_policies(s3_destination_id)`,
	)

	for _, query := range queries {
		if _, err := execer.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to replay skipped backup-support migration for %s: %w", dbProvider, err)
		}
	}
	return nil
}

// pullRedeployGitOpsSyncColumnsInternal lists the columns
// 074_add_gitops_sync_pull_redeploy.sql adds to gitops_syncs. The definitions
// are identical in both dialects.
var pullRedeployGitOpsSyncColumnsInternal = []struct {
	name       string
	definition string
}{
	{"pull_image_after_sync", `BOOLEAN NOT NULL DEFAULT false`},
	{"redeploy_after_sync", `BOOLEAN NOT NULL DEFAULT false`},
}

// missingPullRedeployColumnsInternal lists the gitops_syncs columns from the
// pull/redeploy-after-sync migration that the database does not have yet.
func missingPullRedeployColumnsInternal(ctx context.Context, db *sql.DB, dbProvider string) ([]string, error) {
	var missing []string
	for _, column := range pullRedeployGitOpsSyncColumnsInternal {
		present, err := columnExistsInternal(ctx, db, dbProvider, "gitops_syncs", column.name)
		if err != nil {
			return nil, err
		}
		if !present {
			missing = append(missing, column.name)
		}
	}
	return missing, nil
}

// replaySkippedPullRedeployInternal replays the Up statements of
// 074_add_gitops_sync_pull_redeploy.sql, which Goose skipped because a 074-era
// fork build had already recorded version 74 for its own migration. The ALTERs
// are filtered to missingColumns (computed by the caller — SQLite's ALTER TABLE
// has no IF NOT EXISTS), so replaying on a database that already carries the
// columns is a no-op.
func replaySkippedPullRedeployInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string, missingColumns []string) error {
	switch dbProvider {
	case dbProviderSQLite, dbProviderPostgres:
	default:
		return fmt.Errorf("unsupported database provider: %s", dbProvider)
	}

	for _, column := range pullRedeployGitOpsSyncColumnsInternal {
		if !slices.Contains(missingColumns, column.name) {
			continue
		}
		query := fmt.Sprintf(`ALTER TABLE gitops_syncs ADD COLUMN "%s" %s`, column.name, column.definition)
		if _, err := execer.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to replay skipped GitOps pull/redeploy-after-sync migration for %s: %w", dbProvider, err)
		}
	}
	return nil
}

// backupModeGitOpsSyncColumnsInternal lists the columns
// 085_add_gitops_backup_mode.sql adds to gitops_syncs. Only the two timestamp
// columns differ between dialects.
var backupModeGitOpsSyncColumnsInternal = []struct {
	name       string
	definition string
	timestamp  bool
}{
	{name: "mode", definition: `TEXT NOT NULL DEFAULT 'deploy'`},
	{name: "backup_directory", definition: `TEXT NOT NULL DEFAULT ''`},
	{name: "backup_paths", definition: `TEXT`},
	{name: "backup_on_save", definition: `BOOLEAN NOT NULL DEFAULT true`},
	{name: "backup_pending", definition: `BOOLEAN NOT NULL DEFAULT false`},
	{name: "backup_pending_since", timestamp: true},
	{name: "backup_conflict", definition: `BOOLEAN NOT NULL DEFAULT false`},
	{name: "backup_failure_reason", definition: `TEXT`},
	{name: "last_backup_at", timestamp: true},
	{name: "last_backup_snapshot", definition: `TEXT`},
}

// missingBackupModeColumnsInternal lists the gitops_syncs columns from the
// GitOps backup-mode migration that the database does not have yet.
func missingBackupModeColumnsInternal(ctx context.Context, db *sql.DB, dbProvider string) ([]string, error) {
	var missing []string
	for _, column := range backupModeGitOpsSyncColumnsInternal {
		present, err := columnExistsInternal(ctx, db, dbProvider, "gitops_syncs", column.name)
		if err != nil {
			return nil, err
		}
		if !present {
			missing = append(missing, column.name)
		}
	}
	return missing, nil
}

// replaySkippedBackupModeInternal replays the Up statements of
// 085_add_gitops_backup_mode.sql, which Goose skipped because an 085-era fork
// build had already recorded version 85 for its own migration. The ALTERs are
// filtered to missingColumns (computed by the caller — SQLite's ALTER TABLE has
// no IF NOT EXISTS) and the partial unique index gains an IF NOT EXISTS guard,
// so replaying on a database that already carries part or all of the schema is a
// no-op.
func replaySkippedBackupModeInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string, missingColumns []string) error {
	var timestampType string
	switch dbProvider {
	case dbProviderSQLite:
		timestampType = "DATETIME"
	case dbProviderPostgres:
		timestampType = "TIMESTAMPTZ"
	default:
		return fmt.Errorf("unsupported database provider: %s", dbProvider)
	}

	for _, column := range backupModeGitOpsSyncColumnsInternal {
		if !slices.Contains(missingColumns, column.name) {
			continue
		}
		definition := column.definition
		if column.timestamp {
			definition = timestampType
		}
		query := fmt.Sprintf(`ALTER TABLE gitops_syncs ADD COLUMN "%s" %s`, column.name, definition)
		if _, err := execer.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to replay skipped GitOps backup-mode migration for %s: %w", dbProvider, err)
		}
	}

	// The migration writes this without IF NOT EXISTS; the replay adds the guard
	// so a crashed earlier repair can re-run it.
	index := `CREATE UNIQUE INDEX IF NOT EXISTS idx_gitops_syncs_backup_project ON gitops_syncs(project_id) WHERE mode = 'backup'`
	if _, err := execer.ExecContext(ctx, index); err != nil {
		return fmt.Errorf("failed to replay skipped GitOps backup-mode index for %s: %w", dbProvider, err)
	}
	return nil
}

// vulnerabilityRiskStatementsInternal holds the table and index statements of
// 088_add_vulnerability_risk.sql for each dialect. Every one is IF NOT EXISTS-
// guarded exactly as the migration writes it, so only the cvss_score column needs
// missing-object probing.
var vulnerabilityRiskStatementsInternal = map[string][]string{
	dbProviderSQLite: {
		`CREATE TABLE IF NOT EXISTS vulnerability_threat_intel (
    cve_id TEXT PRIMARY KEY,
    known_exploited BOOLEAN NOT NULL DEFAULT false,
    kev_date_added TEXT NOT NULL DEFAULT '',
    kev_due_date TEXT NOT NULL DEFAULT '',
    kev_ransomware BOOLEAN NOT NULL DEFAULT false,
    epss_score REAL,
    epss_percentile REAL,
    epss_checked_at DATETIME,
    updated_at DATETIME
)`,
		`CREATE INDEX IF NOT EXISTS idx_vulnerability_threat_intel_known_exploited ON vulnerability_threat_intel(known_exploited)`,
		`CREATE TABLE IF NOT EXISTS vulnerability_risk_snapshots (
    snapshot_date TEXT PRIMARY KEY,
    risk_score INTEGER NOT NULL DEFAULT 0,
    risk_band TEXT NOT NULL DEFAULT 'none',
    critical_count INTEGER NOT NULL DEFAULT 0,
    high_count INTEGER NOT NULL DEFAULT 0,
    medium_count INTEGER NOT NULL DEFAULT 0,
    low_count INTEGER NOT NULL DEFAULT 0,
    unknown_count INTEGER NOT NULL DEFAULT 0,
    findings_count INTEGER NOT NULL DEFAULT 0,
    known_exploited_count INTEGER NOT NULL DEFAULT 0,
    overdue_known_exploited_count INTEGER NOT NULL DEFAULT 0,
    high_epss_count INTEGER NOT NULL DEFAULT 0,
    exposed_critical_high_count INTEGER NOT NULL DEFAULT 0,
    fixable_count INTEGER NOT NULL DEFAULT 0,
    images_scanned INTEGER NOT NULL DEFAULT 0,
    images_total INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
)`,
	},
	dbProviderPostgres: {
		`CREATE TABLE IF NOT EXISTS vulnerability_threat_intel (
    cve_id TEXT PRIMARY KEY,
    known_exploited BOOLEAN NOT NULL DEFAULT FALSE,
    kev_date_added TEXT NOT NULL DEFAULT '',
    kev_due_date TEXT NOT NULL DEFAULT '',
    kev_ransomware BOOLEAN NOT NULL DEFAULT FALSE,
    epss_score DOUBLE PRECISION,
    epss_percentile DOUBLE PRECISION,
    epss_checked_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
)`,
		`CREATE INDEX IF NOT EXISTS idx_vulnerability_threat_intel_known_exploited ON vulnerability_threat_intel(known_exploited)`,
		`CREATE TABLE IF NOT EXISTS vulnerability_risk_snapshots (
    snapshot_date TEXT PRIMARY KEY,
    risk_score INTEGER NOT NULL DEFAULT 0,
    risk_band TEXT NOT NULL DEFAULT 'none',
    critical_count INTEGER NOT NULL DEFAULT 0,
    high_count INTEGER NOT NULL DEFAULT 0,
    medium_count INTEGER NOT NULL DEFAULT 0,
    low_count INTEGER NOT NULL DEFAULT 0,
    unknown_count INTEGER NOT NULL DEFAULT 0,
    findings_count INTEGER NOT NULL DEFAULT 0,
    known_exploited_count INTEGER NOT NULL DEFAULT 0,
    overdue_known_exploited_count INTEGER NOT NULL DEFAULT 0,
    high_epss_count INTEGER NOT NULL DEFAULT 0,
    exposed_critical_high_count INTEGER NOT NULL DEFAULT 0,
    fixable_count INTEGER NOT NULL DEFAULT 0,
    images_scanned INTEGER NOT NULL DEFAULT 0,
    images_total INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
)`,
	},
}

// replaySkippedVulnerabilityRiskBeforeUpToInternal replays upstream's
// 088_add_vulnerability_risk.sql for an 088-era database. Unlike every other era
// this one cannot wait for the repair transaction: upstream's 089 alters
// vulnerability_risk_snapshots, so the UpTo that applies it would fail on the
// missing table before the repair ever ran. It commits on its own and is
// idempotent, so a crashed earlier attempt re-runs as a no-op.
// replaySkippedMigrationsBeforeUpToInternal replays the eras whose skipped
// upstream migration a *later* upstream migration depends on, so the Goose run
// does not trip over the gap. Two independent reasons put an era here:
//
//   - Upstream's SQLite 092 rebuilds gitops_syncs from an explicit column list,
//     so it fails outright unless the columns 074 (pull/redeploy flags) and 085
//     (backup mode) add are already present. Both dialects replay them here; only
//     SQLite strictly needs it, and the replay is idempotent either way.
//   - Upstream's 089 alters vulnerability_risk_snapshots, which 088 creates.
//
// Each replay commits on its own, ahead of the repair's main transaction, and is
// idempotent, so a crashed run re-runs it as a no-op. Any future era whose number
// is followed by an upstream migration that builds on it belongs here too — check
// the migrations *above* the orphaned number, not just the one that claimed it.
func replaySkippedMigrationsBeforeUpToInternal(ctx context.Context, db *sql.DB, dbProvider string, eras forkCommitEnvRenumberErasInternal) error {
	if eras.last {
		missing, err := missingPullRedeployColumnsInternal(ctx, db, dbProvider)
		if err != nil {
			return err
		}
		if err := runReplayInTransactionInternal(ctx, db, dbProvider, "skipped pull/redeploy", func(tx sqlExecerInternal) error {
			return replaySkippedPullRedeployInternal(ctx, tx, dbProvider, missing)
		}); err != nil {
			return err
		}
	}

	if eras.backupMode {
		missing, err := missingBackupModeColumnsInternal(ctx, db, dbProvider)
		if err != nil {
			return err
		}
		if err := runReplayInTransactionInternal(ctx, db, dbProvider, "skipped GitOps backup mode", func(tx sqlExecerInternal) error {
			return replaySkippedBackupModeInternal(ctx, tx, dbProvider, missing)
		}); err != nil {
			return err
		}
	}

	return replaySkippedVulnerabilityRiskBeforeUpToInternal(ctx, db, dbProvider, eras)
}

// runReplayInTransactionInternal commits one replay on its own, ahead of the
// repair's main transaction.
func runReplayInTransactionInternal(ctx context.Context, db *sql.DB, dbProvider, what string, replay func(tx sqlExecerInternal) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start %s replay transaction for %s: %w", what, dbProvider, err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := replay(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit %s replay for %s: %w", what, dbProvider, err)
	}
	return nil
}

func replaySkippedVulnerabilityRiskBeforeUpToInternal(ctx context.Context, db *sql.DB, dbProvider string, eras forkCommitEnvRenumberErasInternal) error {
	if !eras.risk {
		return nil
	}

	scorePresent, err := columnExistsInternal(ctx, db, dbProvider, "vulnerability_scan_items", "cvss_score")
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start skipped vulnerability-risk replay transaction for %s: %w", dbProvider, err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := replaySkippedVulnerabilityRiskInternal(ctx, tx, dbProvider, scorePresent); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit skipped vulnerability-risk replay for %s: %w", dbProvider, err)
	}
	return nil
}

// replaySkippedVulnerabilityRiskInternal replays the Up statements of
// 088_add_vulnerability_risk.sql. The cvss_score ALTER and its backfill are
// skipped when the column is already there (SQLite's ALTER TABLE has no IF NOT
// EXISTS, and a column the database already carries may hold fresher scores).
func replaySkippedVulnerabilityRiskInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string, scorePresent bool) error {
	queries, ok := vulnerabilityRiskStatementsInternal[dbProvider]
	if !ok {
		return fmt.Errorf("unsupported database provider: %s", dbProvider)
	}

	if !scorePresent {
		for _, query := range vulnerabilityRiskScoreStatementsInternal[dbProvider] {
			if _, err := execer.ExecContext(ctx, query); err != nil {
				return fmt.Errorf("failed to replay skipped vulnerability cvss_score column for %s: %w", dbProvider, err)
			}
		}
	}

	for _, query := range queries {
		if _, err := execer.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to replay skipped vulnerability-risk migration for %s: %w", dbProvider, err)
		}
	}
	return nil
}

// vulnerabilityRiskScoreStatementsInternal holds the cvss_score ALTER and its
// backfill from 088_add_vulnerability_risk.sql, which only run when the column is
// still missing.
var vulnerabilityRiskScoreStatementsInternal = map[string][]string{
	dbProviderSQLite: {
		`ALTER TABLE vulnerability_scan_items ADD COLUMN cvss_score REAL`,
		`UPDATE vulnerability_scan_items
SET cvss_score = COALESCE(
    NULLIF(CAST(json_extract(details, '$.cvss.v3Score') AS REAL), 0),
    NULLIF(CAST(json_extract(details, '$.cvss.v2Score') AS REAL), 0)
)
WHERE details IS NOT NULL AND json_valid(details)`,
	},
	dbProviderPostgres: {
		`ALTER TABLE vulnerability_scan_items ADD COLUMN IF NOT EXISTS cvss_score DOUBLE PRECISION`,
		`UPDATE vulnerability_scan_items
SET cvss_score = COALESCE(
    NULLIF((CAST(details AS jsonb) #>> '{cvss,v3Score}')::double precision, 0),
    NULLIF((CAST(details AS jsonb) #>> '{cvss,v2Score}')::double precision, 0)
)
WHERE details IS NOT NULL AND details <> ''`,
	},
}

// apnsTableStatementsInternal holds the Up statements of 077_add_apns.sql for
// each dialect. Every statement is IF NOT EXISTS-guarded exactly as the migration
// writes it, so the replay needs no missing-object probing.
var apnsTableStatementsInternal = map[string][]string{
	dbProviderSQLite: {
		`CREATE TABLE IF NOT EXISTS apns_devices (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient_id TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    events TEXT NOT NULL DEFAULT '{}',
    environment_ids TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    last_seen_at DATETIME
)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_apns_devices_recipient ON apns_devices(recipient_id)`,
		`CREATE INDEX IF NOT EXISTS idx_apns_devices_user ON apns_devices(user_id)`,
		`CREATE TABLE IF NOT EXISTS apns_outbox (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL,
    envelope TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME NOT NULL,
    last_error TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
)`,
		`CREATE INDEX IF NOT EXISTS idx_apns_outbox_next_attempt ON apns_outbox(next_attempt_at)`,
	},
	dbProviderPostgres: {
		`CREATE TABLE IF NOT EXISTS apns_devices (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient_id TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    events JSONB NOT NULL DEFAULT '{}',
    environment_ids JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ
)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_apns_devices_recipient ON apns_devices(recipient_id)`,
		`CREATE INDEX IF NOT EXISTS idx_apns_devices_user ON apns_devices(user_id)`,
		`CREATE TABLE IF NOT EXISTS apns_outbox (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL,
    envelope TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
)`,
		`CREATE INDEX IF NOT EXISTS idx_apns_outbox_next_attempt ON apns_outbox(next_attempt_at)`,
	},
}

// replaySkippedApnsInternal replays the Up statements of 077_add_apns.sql, which
// Goose skipped because a 077-era fork build had already recorded version 77 for
// its own migration.
func replaySkippedApnsInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string) error {
	queries, ok := apnsTableStatementsInternal[dbProvider]
	if !ok {
		return fmt.Errorf("unsupported database provider: %s", dbProvider)
	}

	for _, query := range queries {
		if _, err := execer.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to replay skipped Apple push notification migration for %s: %w", dbProvider, err)
		}
	}
	return nil
}
