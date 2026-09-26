package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	dbtypes "github.com/getarcaneapp/arcane/types/v2/database"
	"github.com/libtnb/sqlite"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	kit "go.getarcane.app/kit/pkg"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	sqliteutil "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/sqlite"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
)

type DB struct {
	*gorm.DB
}

type MigrationOptions struct {
	AllowDowngrade bool
}

const (
	dbProviderSQLite   = "sqlite"
	dbProviderPostgres = "postgres"
	gooseVersionTable  = "goose_db_version"
	legacyVersionTable = "schema_migrations"

	// Prepared-statement cache bounds. GORM's PrepareStmt cache is a global LRU keyed
	// by SQL text. When PrepareStmtMaxSize/PrepareStmtTTL are left at zero, GORM falls
	// back to math.MaxInt entries with a 24h TTL, i.e. effectively unbounded. Because
	// this codebase emits highly variable SQL (dynamic filter/sort/pagination and
	// GORM's IN (?,?,...) slice expansion, whose placeholder count changes the query
	// text), the cache — and the modernc.org/sqlite compiled statements it retains on
	// the Go heap — grows steadily under normal use. Bounding size and TTL keeps hot
	// queries prepared while evicting the long tail (evicted statements are closed).
	preparedStmtMaxSize = 256
	preparedStmtTTL     = 15 * time.Minute
)

var customGormLogger logger.Interface

func SetGormLogger(l logger.Interface) {
	customGormLogger = l
}

func Initialize(ctx context.Context, databaseURL string, options MigrationOptions) (database *DB, err error) {
	db, err := connectDatabaseInternal(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	// Initialization opens a connection pool before it can fail on migrations or
	// provider detection; release it rather than leaking it into a failed startup.
	defer func() {
		if err == nil {
			return
		}
		if closeErr := db.Close(); closeErr != nil {
			slog.WarnContext(ctx, "Failed to close database after initialization failure", "error", closeErr)
		}
	}()

	if cancellationErr := ctx.Err(); cancellationErr != nil {
		return nil, cancellationErr
	}

	// Get underlying sql.DB for migrations
	sqlDB, err := db.DB.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	var dbProvider string
	switch {
	case strings.HasPrefix(databaseURL, "file:"):
		dbProvider = dbProviderSQLite
	case strings.HasPrefix(databaseURL, "postgres"):
		dbProvider = dbProviderPostgres
	default:
		return nil, fmt.Errorf("unsupported database type in URL: %s", databaseURL)
	}

	if migrateDatabaseErr := migrateDatabaseInternal(ctx, sqlDB, dbProvider, options); migrateDatabaseErr != nil {
		slog.Error("Failed to run migrations", "error", migrateDatabaseErr)
		return nil, fmt.Errorf("failed to run migrations: %w", migrateDatabaseErr)
	}

	// Set connection pool settings
	if db.Name() == "postgres" {
		sqlDB.SetMaxIdleConns(15)
		sqlDB.SetMaxOpenConns(50)
	} else {
		sqlDB.SetMaxIdleConns(5)
		sqlDB.SetMaxOpenConns(20)
	}
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	sqlDB.SetConnMaxIdleTime(3 * time.Minute)

	return db, nil
}

func connectDatabaseInternal(ctx context.Context, databaseURL string) (*DB, error) {
	var dialector gorm.Dialector

	switch {
	case strings.HasPrefix(databaseURL, "file:"):
		if err := sqliteutil.RegisterFunctions(); err != nil {
			return nil, fmt.Errorf("failed to register SQLite functions: %w", err)
		}
		connString, err := ParseSQLiteConnectionString(databaseURL, dbtypes.SQLiteConnectionOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to parse SQLite connection string: %w", err)
		}
		if ensureSQLiteDirectoryErr := ensureSQLiteDirectoryInternal(connString); ensureSQLiteDirectoryErr != nil {
			return nil, fmt.Errorf("failed to prepare SQLite directory: %w", ensureSQLiteDirectoryErr)
		}
		dialector = sqlite.Open(connString)
	case strings.HasPrefix(databaseURL, "postgres"):
		dialector = postgres.Open(databaseURL)
	default:
		return nil, fmt.Errorf("unsupported database type in URL: %s", databaseURL)
	}

	// Retry connection up to 3 times
	var db *gorm.DB
	var err error
	for i := 1; i <= 3; i++ {
		if retryCancellationErr := ctx.Err(); retryCancellationErr != nil {
			return nil, retryCancellationErr
		}
		db, err = gorm.Open(dialector, &gorm.Config{
			Logger: customGormLogger,
			NowFunc: func() time.Time {
				return time.Now().UTC()
			},
			PrepareStmt:                      true,
			PrepareStmtMaxSize:               preparedStmtMaxSize,
			PrepareStmtTTL:                   preparedStmtTTL,
			IgnoreRelationshipsWhenMigrating: true,
		})
		if err == nil {
			return &DB{db}, nil
		}

		slog.Info("Failed to initialize database", "attempt", i)
		if i < 3 {
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	return nil, err
}

func migrateDatabaseInternal(ctx context.Context, db *sql.DB, dbProvider string, options MigrationOptions) error {
	requiredVersion, err := getHighestEmbeddedMigrationVersionInternal(dbProvider)
	if err != nil {
		return fmt.Errorf("failed to determine target migration version for %s: %w", dbProvider, err)
	}

	return migrateDatabaseToVersionInternal(ctx, db, dbProvider, options, requiredVersion)
}

func migrateDatabaseToVersionInternal(ctx context.Context, db *sql.DB, dbProvider string, options MigrationOptions, requiredVersion int64) error {
	if err := adoptLegacyMigrationStateInternal(ctx, db, dbProvider, options); err != nil {
		return err
	}

	provider, err := newGooseProviderInternal(db, dbProvider)
	if err != nil {
		return fmt.Errorf("failed to create goose provider for %s: %w", dbProvider, err)
	}

	currentVersion, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to determine current migration version for %s: %w", dbProvider, err)
	}

	slog.Info("Resolved database migration state", "provider", dbProvider, "currentVersion", currentVersion, "requiredVersion", requiredVersion)

	if currentVersion > requiredVersion {
		if !options.AllowDowngrade {
			return fmt.Errorf(
				"database schema version %d is newer than this Arcane binary supports (target %d for %s); downgrade "+
					"requires ALLOW_DOWNGRADE=true and a database backup before startup",
				currentVersion,
				requiredVersion,
				dbProvider,
			)
		}

		missingVersions, missingEmbeddedDowngradeMigrationsErr := missingEmbeddedDowngradeMigrationsInternal(ctx, db, dbProvider, requiredVersion)
		if missingEmbeddedDowngradeMigrationsErr != nil {
			return missingEmbeddedDowngradeMigrationsErr
		}
		if len(missingVersions) > 0 {
			return fmt.Errorf("cannot downgrade database from version %d to %d for %s: embedded Goose migrations are missing for "+
				"applied version(s) %v, so the rollback SQL is unavailable in this Arcane binary; "+
				"ALLOW_DOWNGRADE=true is not sufficient, restore the database from a backup taken before the newer "+
				"schema was applied",
				currentVersion,
				requiredVersion,
				dbProvider,
				missingVersions,
			)
		}

		if _, downToErr := provider.DownTo(ctx, requiredVersion); downToErr != nil {
			return fmt.Errorf("failed to downgrade database from version %d to %d for %s using embedded Goose migrations: %w", currentVersion, requiredVersion, dbProvider, downToErr)
		}

		slog.Info("Database downgrade completed successfully", "provider", dbProvider, "fromVersion", currentVersion, "toVersion", requiredVersion)
		return nil
	}

	if currentVersion == requiredVersion {
		slog.Info("Database schema is up to date", "provider", dbProvider, "migrationVersion", currentVersion)
		return nil
	}

	if err := repairPreRenumberForkMigrationInternal(ctx, db, dbProvider, provider, currentVersion, requiredVersion); err != nil {
		return err
	}

	if _, upToErr := provider.UpTo(ctx, requiredVersion); upToErr != nil {
		return fmt.Errorf("failed to apply embedded Goose migrations for %s: %w", dbProvider, upToErr)
	}

	slog.Info("Database migrations completed successfully", "provider", dbProvider, "targetVersion", requiredVersion)
	return nil
}

// This fork's GitOps commit-injection migration has shipped under six version
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
//
// Goose keys its bookkeeping on the version number alone, so a database migrated
// by a build from any of those windows is wrong in two ways: the upstream
// migration that now owns the recorded number is treated as applied and silently
// skipped, and gitops_syncs.inject_commit_env already exists, so re-running the
// fork migration under its new number aborts with "duplicate column name:
// inject_commit_env" and Arcane refuses to start.
//
// repairPreRenumberForkMigrationInternal detects all of those states and repairs them
// in place, without touching the operator's data: it applies everything below the
// fork migration's current number through Goose, replays the upstream migration(s)
// the stale bookkeeping made Goose skip, and records the fork migration as applied
// instead of re-running its DDL. It runs outside Goose's Postgres session lock,
// which is harmless: a second instance re-runs the same checks and finds nothing to
// do, and the worst a genuine race can leave behind is a duplicate version row,
// which Goose collapses when it reads its state. It can be deleted once no database
// migrated by a pre-090 fork build is left in the wild.
const (
	forkCommitEnvMigrationVersion          int64 = 90
	forkCommitEnvRiskRenumberVersion       int64 = 88
	forkCommitEnvBackupModeRenumberVersion int64 = 85
	forkCommitEnvApnsRenumberVersion       int64 = 77
	forkCommitEnvLastRenumberVersion       int64 = 74
	forkCommitEnvLateRenumberVersion       int64 = 73
	forkCommitEnvMidRenumberVersion        int64 = 71
	forkCommitEnvPreRenumberVersion        int64 = 69
)

// forkCommitEnvRenumberErasInternal records which of the fork migration's old
// version numbers a database has recorded as applied. Each true flag means Goose
// will skip the upstream migration that now owns that number, so the repair has
// to replay it by hand.
type forkCommitEnvRenumberErasInternal struct {
	mid        bool // 71 — upstream's volume-workspace legacy key renames
	late       bool // 73 — upstream's S3/system backup support
	last       bool // 74 — upstream's GitOps pull/redeploy-after-sync flags
	apns       bool // 77 — upstream's Apple push notification tables
	backupMode bool // 85 — upstream's GitOps backup mode
	risk       bool // 88 — upstream's vulnerability risk scoring tables
}

func repairPreRenumberForkMigrationInternal(ctx context.Context, db *sql.DB, dbProvider string, provider *goose.Provider, currentVersion, requiredVersion int64) error {
	if requiredVersion < forkCommitEnvMigrationVersion || currentVersion >= forkCommitEnvMigrationVersion {
		return nil
	}

	needsRepair, err := hasPreRenumberForkMigrationStateInternal(ctx, db, dbProvider, currentVersion)
	if err != nil || !needsRepair {
		return err
	}

	// Decide *before* the UpTo below which of versions 71, 73, 74, 77, 85 and 88
	// are already recorded: on a 069-era database none is, and the UpTo applies
	// upstream's real 071, 073, 074, 077, 085 and 088 itself (recording them); on any
	// later era the recorded number belongs to the fork's own migration, so Goose skips
	// the upstream migration that now owns it and its statements have to be
	// replayed by hand.
	eras, err := recordedRenumberEraVersionsInternal(ctx, db, dbProvider)
	if err != nil {
		return err
	}

	slog.Warn("Detected a database migrated by a pre-renumber fork build; repairing migration state",
		"provider", dbProvider, "currentVersion", currentVersion, "forkMigrationVersion", forkCommitEnvMigrationVersion)

	if err := replaySkippedVulnerabilityRiskBeforeUpToInternal(ctx, db, dbProvider, eras); err != nil {
		return err
	}

	// Everything below the fork migration has to be applied first: recording version
	// 73 below raises the Goose version past the intermediate migrations, after
	// which UpTo would skip them.
	belowForkVersion := forkCommitEnvMigrationVersion - 1
	if currentVersion < belowForkVersion {
		if _, err := provider.UpTo(ctx, belowForkVersion); err != nil {
			return errors.WrapIff(err, "failed to apply embedded Goose migrations up to version %d for %s while repairing pre-renumber fork migration state", belowForkVersion, dbProvider)
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

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.WrapIff(err, "failed to start pre-renumber fork migration repair transaction for %s", dbProvider)
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

	// The column 088 adds is already present, so record it as applied rather than
	// re-running its DDL, which would fail on the duplicate column.
	if err := insertGooseMigrationVersionInternal(ctx, tx, dbProvider, forkCommitEnvMigrationVersion); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errors.WrapIff(err, "failed to commit pre-renumber fork migration repair for %s", dbProvider)
	}

	slog.Info("Repaired pre-renumber fork migration state",
		"provider", dbProvider, "forkMigrationVersion", forkCommitEnvMigrationVersion,
		"restoredSkippedMigration", !repositoryNamesPresent, "replayedVolumeWorkspaceRename", eras.mid,
		"replayedBackupSupport", eras.late, "replayedPullRedeploy", eras.last, "replayedApns", eras.apns,
		"replayedGitOpsBackupMode", eras.backupMode, "replayedVulnerabilityRisk", eras.risk)
	return nil
}

// recordedRenumberEraVersionsInternal reports which of the fork migration's old
// version numbers (71, 73, 74, 77, 85, 88) are recorded as applied — for each, the
// signature of the corresponding renumber era whose skipped upstream migration
// the repair has to replay by hand.
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
	pullRedeploy  []string
	backupMode    []string
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
	if eras.last {
		if missing.pullRedeploy, err = missingPullRedeployColumnsInternal(ctx, db, dbProvider); err != nil {
			return forkCommitEnvReplayColumnsInternal{}, err
		}
	}
	if eras.backupMode {
		if missing.backupMode, err = missingBackupModeColumnsInternal(ctx, db, dbProvider); err != nil {
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
	// 074: GitOps pull/redeploy-after-sync flags.
	if eras.last {
		if err := replaySkippedPullRedeployInternal(ctx, execer, dbProvider, missing.pullRedeploy); err != nil {
			return err
		}
	}
	// 077: Apple push notification devices and outbox.
	if eras.apns {
		if err := replaySkippedApnsInternal(ctx, execer, dbProvider); err != nil {
			return err
		}
	}
	// 085: GitOps backup mode.
	if eras.backupMode {
		if err := replaySkippedBackupModeInternal(ctx, execer, dbProvider, missing.backupMode); err != nil {
			return err
		}
	}
	// 088 (vulnerability risk) is replayed before the Goose UpTo instead, because
	// upstream's 089 alters the table it creates. See
	// replaySkippedVulnerabilityRiskBeforeUpToInternal.
	return nil
}

// missingBackupSupportColumnsInternal lists the volume_backups columns from the
// backup-support migration that the database does not have yet.
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

	return columnExistsInternal(ctx, db, dbProvider, "gitops_syncs", "inject_commit_env")
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
		return errors.Errorf("unsupported database provider: %s", dbProvider)
	}

	if _, err := execer.ExecContext(ctx, query); err != nil {
		return errors.WrapIff(err, "failed to restore skipped container_registries.repository_names column for %s", dbProvider)
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
		return errors.Errorf("unsupported database provider: %s", dbProvider)
	}

	for _, query := range queries {
		if _, err := execer.ExecContext(ctx, query); err != nil {
			return errors.WrapIff(err, "failed to replay skipped volume-workspace rename migration for %s", dbProvider)
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
		return errors.Errorf("unsupported database provider: %s", dbProvider)
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
			return errors.WrapIff(err, "failed to replay skipped backup-support migration for %s", dbProvider)
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
		return errors.Errorf("unsupported database provider: %s", dbProvider)
	}

	for _, column := range pullRedeployGitOpsSyncColumnsInternal {
		if !slices.Contains(missingColumns, column.name) {
			continue
		}
		query := fmt.Sprintf(`ALTER TABLE gitops_syncs ADD COLUMN "%s" %s`, column.name, column.definition)
		if _, err := execer.ExecContext(ctx, query); err != nil {
			return errors.WrapIff(err, "failed to replay skipped GitOps pull/redeploy-after-sync migration for %s", dbProvider)
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
		return errors.Errorf("unsupported database provider: %s", dbProvider)
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
			return errors.WrapIff(err, "failed to replay skipped GitOps backup-mode migration for %s", dbProvider)
		}
	}

	// The migration writes this without IF NOT EXISTS; the replay adds the guard
	// so a crashed earlier repair can re-run it.
	index := `CREATE UNIQUE INDEX IF NOT EXISTS idx_gitops_syncs_backup_project ON gitops_syncs(project_id) WHERE mode = 'backup'`
	if _, err := execer.ExecContext(ctx, index); err != nil {
		return errors.WrapIff(err, "failed to replay skipped GitOps backup-mode index for %s", dbProvider)
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
		return errors.WrapIff(err, "failed to start skipped vulnerability-risk replay transaction for %s", dbProvider)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := replaySkippedVulnerabilityRiskInternal(ctx, tx, dbProvider, scorePresent); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errors.WrapIff(err, "failed to commit skipped vulnerability-risk replay for %s", dbProvider)
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
		return errors.Errorf("unsupported database provider: %s", dbProvider)
	}

	if !scorePresent {
		for _, query := range vulnerabilityRiskScoreStatementsInternal[dbProvider] {
			if _, err := execer.ExecContext(ctx, query); err != nil {
				return errors.WrapIff(err, "failed to replay skipped vulnerability cvss_score column for %s", dbProvider)
			}
		}
	}

	for _, query := range queries {
		if _, err := execer.ExecContext(ctx, query); err != nil {
			return errors.WrapIff(err, "failed to replay skipped vulnerability-risk migration for %s", dbProvider)
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
		return errors.Errorf("unsupported database provider: %s", dbProvider)
	}

	for _, query := range queries {
		if _, err := execer.ExecContext(ctx, query); err != nil {
			return errors.WrapIff(err, "failed to replay skipped Apple push notification migration for %s", dbProvider)
		}
	}
	return nil
}

func newGooseProviderInternal(db *sql.DB, dbProvider string) (*goose.Provider, error) {
	migrationsFS, err := embeddedMigrationFSInternal(dbProvider)
	if err != nil {
		return nil, err
	}

	dialect, err := gooseDialectInternal(dbProvider)
	if err != nil {
		return nil, err
	}

	// Two Arcane processes pointed at the same Postgres (a rolling deploy, or a
	// replica set) would otherwise run migrations concurrently and race on the
	// same DDL. A session-level advisory lock serializes them; SQLite needs no
	// equivalent because it is single-writer by construction.
	var options []goose.ProviderOption
	if dialect == goose.DialectPostgres {
		sessionLocker, newPostgresSessionLockerErr := lock.NewPostgresSessionLocker()
		if newPostgresSessionLockerErr != nil {
			return nil, fmt.Errorf("failed to create Postgres migration session locker: %w", newPostgresSessionLockerErr)
		}
		options = append(options, goose.WithSessionLocker(sessionLocker))
	}

	return goose.NewProvider(dialect, db, migrationsFS, options...)
}

func embeddedMigrationFSInternal(dbProvider string) (fs.FS, error) {
	migrationsFS, err := fs.Sub(resources.FS, "migrations/"+dbProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to load embedded migrations for %s: %w", dbProvider, err)
	}

	return migrationsFS, nil
}

func gooseDialectInternal(dbProvider string) (goose.Dialect, error) {
	switch dbProvider {
	case dbProviderSQLite:
		return goose.DialectSQLite3, nil
	case dbProviderPostgres:
		return goose.DialectPostgres, nil
	default:
		return "", fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
}

func adoptLegacyMigrationStateInternal(ctx context.Context, db *sql.DB, dbProvider string, options MigrationOptions) error {
	legacyState, ok, err := legacyMigrationStateInternal(ctx, db, dbProvider)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	if legacyState.dirty {
		if !options.AllowDowngrade {
			return fmt.Errorf(
				"database schema version %d is dirty in legacy %s table; resolve it manually or set "+
					"ALLOW_DOWNGRADE=true after verifying the database state",
				legacyState.version,
				legacyVersionTable,
			)
		}

		if clearLegacyMigrationDirtyErr := clearLegacyMigrationDirtyInternal(ctx, db, dbProvider, legacyState.version); clearLegacyMigrationDirtyErr != nil {
			return clearLegacyMigrationDirtyErr
		}
		slog.Warn("Cleared dirty legacy migration state because ALLOW_DOWNGRADE=true", "provider", dbProvider, "version", legacyState.version)
	}

	hasGooseState, err := gooseVersionTableHasAppliedMigrationsInternal(ctx, db, dbProvider)
	if err != nil {
		return err
	}
	if hasGooseState {
		return nil
	}

	versions, err := getEmbeddedMigrationVersionsInternal(dbProvider)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start legacy migration adoption transaction for %s: %w", dbProvider, err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if createGooseVersionTableErr := createGooseVersionTableInternal(ctx, tx, dbProvider); createGooseVersionTableErr != nil {
		return createGooseVersionTableErr
	}

	if clearGooseVersionTableErr := clearGooseVersionTableInternal(ctx, tx, dbProvider); clearGooseVersionTableErr != nil {
		return clearGooseVersionTableErr
	}

	if insertGooseMigrationVersionErr := insertGooseMigrationVersionInternal(ctx, tx, dbProvider, 0); insertGooseMigrationVersionErr != nil {
		return insertGooseMigrationVersionErr
	}
	versionApplied := legacyState.version == 0
	for _, version := range versions {
		if version > legacyState.version {
			break
		}
		if insertMigrationVersionErr := insertGooseMigrationVersionInternal(ctx, tx, dbProvider, version); insertMigrationVersionErr != nil {
			return insertMigrationVersionErr
		}
		if version == legacyState.version {
			versionApplied = true
		}
	}
	if !versionApplied {
		if insertLegacyVersionErr := insertGooseMigrationVersionInternal(ctx, tx, dbProvider, legacyState.version); insertLegacyVersionErr != nil {
			return insertLegacyVersionErr
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("failed to commit legacy migration adoption for %s: %w", dbProvider, commitErr)
	}

	slog.Info("Adopted legacy migration state into Goose", "provider", dbProvider, "legacyVersion", legacyState.version)
	return nil
}

type legacyMigrationState struct {
	version int64
	dirty   bool
}

func legacyMigrationStateInternal(ctx context.Context, db *sql.DB, dbProvider string) (legacyMigrationState, bool, error) {
	exists, err := legacyVersionTableExistsInternal(ctx, db, dbProvider)
	if err != nil {
		return legacyMigrationState{}, false, err
	}
	if !exists {
		return legacyMigrationState{}, false, nil
	}

	var state legacyMigrationState
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT version, dirty FROM %s ORDER BY version DESC LIMIT 1", legacyVersionTable)).Scan(&state.version, &state.dirty)
	if errors.Is(err, sql.ErrNoRows) {
		return legacyMigrationState{}, false, nil
	}
	if err != nil {
		return legacyMigrationState{}, false, fmt.Errorf("failed to read legacy migration state for %s: %w", dbProvider, err)
	}

	return state, true, nil
}

func legacyVersionTableExistsInternal(ctx context.Context, db *sql.DB, dbProvider string) (bool, error) {
	switch dbProvider {
	case dbProviderSQLite:
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, legacyVersionTable).Scan(&count); err != nil {
			return false, fmt.Errorf("failed to check legacy migration table for sqlite: %w", err)
		}
		return count > 0, nil
	case dbProviderPostgres:
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, legacyVersionTable).Scan(&exists); err != nil {
			return false, fmt.Errorf("failed to check legacy migration table for postgres: %w", err)
		}
		return exists, nil
	default:
		return false, fmt.Errorf("unsupported database provider: %s", dbProvider)
	}
}

func clearLegacyMigrationDirtyInternal(ctx context.Context, db *sql.DB, dbProvider string, version int64) error {
	queryFormat := "UPDATE " + legacyVersionTable + " SET dirty = false WHERE version = %s"
	query, args, err := sqlWithProviderPlaceholderInternal(dbProvider, queryFormat, version)
	if err != nil {
		return err
	}
	if _, execContextErr := db.ExecContext(ctx, query, args...); execContextErr != nil {
		return fmt.Errorf("failed to clear legacy dirty migration state for %s version %d: %w", dbProvider, version, execContextErr)
	}
	return nil
}

func gooseVersionTableHasAppliedMigrationsInternal(ctx context.Context, db *sql.DB, dbProvider string) (bool, error) {
	exists, err := gooseVersionTableExistsInternal(ctx, db, dbProvider)
	if err != nil || !exists {
		return false, err
	}

	var version int64
	applied := kit.Ternary(dbProvider == dbProviderPostgres, "true", "1")
	if scanErr := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COALESCE(MAX(version_id), 0) FROM %s WHERE is_applied = %s", gooseVersionTable, applied)).Scan(&version); scanErr != nil {
		return false, fmt.Errorf("failed to read Goose migration state for %s: %w", dbProvider, scanErr)
	}
	return version > 0, nil
}

func gooseMigrationVersionAppliedInternal(ctx context.Context, db *sql.DB, dbProvider string, version int64) (bool, error) {
	queryFormat := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE is_applied = %s AND version_id = %%s", gooseVersionTable, appliedLiteralInternal(dbProvider))
	query, args, err := sqlWithProviderPlaceholderInternal(dbProvider, queryFormat, version)
	if err != nil {
		return false, err
	}

	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return false, errors.WrapIff(err, "failed to check Goose migration version %d for %s", version, dbProvider)
	}
	return count > 0, nil
}

// columnExistsInternal reports whether a column exists, returning false rather than an
// error when the table itself is absent.
func columnExistsInternal(ctx context.Context, db *sql.DB, dbProvider, table, column string) (bool, error) {
	switch dbProvider {
	case dbProviderSQLite:
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count); err != nil {
			return false, errors.WrapIff(err, "failed to inspect column %s.%s for sqlite", table, column)
		}
		return count > 0, nil
	case dbProviderPostgres:
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2)`, table, column).Scan(&exists); err != nil {
			return false, errors.WrapIff(err, "failed to inspect column %s.%s for postgres", table, column)
		}
		return exists, nil
	default:
		return false, errors.Errorf("unsupported database provider: %s", dbProvider)
	}
}

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

type sqlExecerInternal interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func createGooseVersionTableInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string) error {
	var query string
	switch dbProvider {
	case dbProviderSQLite:
		query = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	version_id INTEGER NOT NULL,
	is_applied INTEGER NOT NULL,
	tstamp TIMESTAMP DEFAULT (datetime('now'))
)`, gooseVersionTable)
	case dbProviderPostgres:
		query = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	id integer PRIMARY KEY GENERATED BY DEFAULT AS IDENTITY,
	version_id bigint NOT NULL,
	is_applied boolean NOT NULL,
	tstamp timestamp NOT NULL DEFAULT now()
)`, gooseVersionTable)
	default:
		return fmt.Errorf("unsupported database provider: %s", dbProvider)
	}

	if _, err := execer.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to create Goose version table for %s: %w", dbProvider, err)
	}
	return nil
}

func clearGooseVersionTableInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string) error {
	if _, err := execer.ExecContext(ctx, "DELETE FROM "+gooseVersionTable); err != nil {
		return fmt.Errorf("failed to clear Goose version table for %s: %w", dbProvider, err)
	}
	return nil
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

func getHighestEmbeddedMigrationVersionInternal(dbProvider string) (int64, error) {
	versions, err := getEmbeddedMigrationVersionsInternal(dbProvider)
	if err != nil {
		return 0, err
	}
	if len(versions) == 0 {
		return 0, fmt.Errorf("no embedded migrations found for %s", dbProvider)
	}

	return versions[len(versions)-1], nil
}

func getEmbeddedMigrationVersionsInternal(dbProvider string) ([]int64, error) {
	entries, err := resources.FS.ReadDir("migrations/" + dbProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded migrations for %s: %w", dbProvider, err)
	}

	versionsMap := make(map[int64]struct{})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		versionText, _, found := strings.Cut(entry.Name(), "_")
		if !found {
			continue
		}
		version, parseErr := strconv.ParseInt(versionText, 10, 64)
		if parseErr != nil {
			continue
		}

		versionsMap[version] = struct{}{}
	}

	versions := make([]int64, 0, len(versionsMap))
	for version := range versionsMap {
		versions = append(versions, version)
	}
	slices.Sort(versions)

	return versions, nil
}

// missingEmbeddedDowngradeMigrationsInternal returns the applied migration
// versions above requiredVersion that have no matching embedded migration file.
// Goose can only roll back migrations whose .Down SQL is embedded in this
// binary, so any such missing version makes an embedded-only downgrade
// impossible and signals that a restore from backup is required.
func missingEmbeddedDowngradeMigrationsInternal(ctx context.Context, db *sql.DB, dbProvider string, requiredVersion int64) ([]int64, error) {
	embeddedVersions, err := getEmbeddedMigrationVersionsInternal(dbProvider)
	if err != nil {
		return nil, err
	}

	embeddedSet := make(map[int64]struct{}, len(embeddedVersions))
	for _, version := range embeddedVersions {
		embeddedSet[version] = struct{}{}
	}

	queryFormat := "SELECT DISTINCT version_id FROM " + gooseVersionTable +
		" WHERE is_applied = " + kit.Ternary(dbProvider == dbProviderPostgres, "true", "1") +
		" AND version_id > %s ORDER BY version_id"
	query, args, err := sqlWithProviderPlaceholderInternal(dbProvider, queryFormat, requiredVersion)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to read applied migration versions for %s: %w", dbProvider, err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var missing []int64
	for rows.Next() {
		var version int64
		if scanErr := rows.Scan(&version); scanErr != nil {
			return nil, fmt.Errorf("failed to scan applied migration version for %s: %w", dbProvider, scanErr)
		}
		if _, ok := embeddedSet[version]; !ok {
			missing = append(missing, version)
		}
	}
	if iterateVersionsErr := rows.Err(); iterateVersionsErr != nil {
		return nil, fmt.Errorf("failed to iterate applied migration versions for %s: %w", dbProvider, iterateVersionsErr)
	}

	return missing, nil
}

func ParseSQLiteConnectionString(connString string, options dbtypes.SQLiteConnectionOptions) (string, error) {
	if !strings.HasPrefix(connString, "file:") {
		connString = "file:" + connString
	}

	connStringUrl, err := url.Parse(connString)
	if err != nil {
		return "", fmt.Errorf("failed to parse SQLite connection string: %w", err)
	}

	qs := make(url.Values, len(connStringUrl.Query()))
	for k, v := range connStringUrl.Query() {
		switch k {
		case "_auto_vacuum", "_vacuum":
			qs.Add("_pragma", "auto_vacuum("+v[0]+")")
		case "_busy_timeout", "_timeout":
			qs.Add("_pragma", "busy_timeout("+v[0]+")")
		case "_case_sensitive_like", "_cslike":
			qs.Add("_pragma", "case_sensitive_like("+v[0]+")")
		case "_foreign_keys", "_fk":
			qs.Add("_pragma", "foreign_keys("+v[0]+")")
		case "_locking_mode", "_locking":
			qs.Add("_pragma", "locking_mode("+v[0]+")")
		case "_secure_delete":
			qs.Add("_pragma", "secure_delete("+v[0]+")")
		case "_synchronous", "_sync":
			qs.Add("_pragma", "synchronous("+v[0]+")")
		case "_journal_mode":
			qs.Add("_pragma", "journal_mode("+v[0]+")")
		case "_txlock":
			qs.Add("_txlock", v[0])
		case "_pragma":
			qs["_pragma"] = append(qs["_pragma"], v...)
		default:
			qs[k] = v
		}
	}

	pragmas := make([]string, 0, len(qs["_pragma"])+2)
	journalMode, busyTimeout := false, false
	for _, pragma := range qs["_pragma"] {
		name, _, _ := strings.Cut(pragma, "(")
		name, _, _ = strings.Cut(name, "=")
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "foreign_keys":
			if options.IgnoreForeignKeys {
				continue
			}
		case "journal_mode":
			journalMode = true
		case "busy_timeout":
			busyTimeout = true
		}
		pragmas = append(pragmas, pragma)
	}
	if !journalMode && options.JournalMode != "" {
		pragmas = append(pragmas, "journal_mode("+options.JournalMode+")")
	}
	if !busyTimeout && options.BusyTimeout > 0 {
		pragmas = append(pragmas, "busy_timeout("+strconv.FormatInt(options.BusyTimeout.Milliseconds(), 10)+")")
	}
	if len(pragmas) > 0 {
		qs["_pragma"] = pragmas
	} else {
		qs.Del("_pragma")
	}

	connStringUrl.RawQuery = qs.Encode()
	return connStringUrl.String(), nil
}

// FindEnvironmentIDByApiKey finds the environment ID that is associated with the given API key.
// It queries the api_keys table to validate the key and find the associated environment.
func (db *DB) FindEnvironmentIDByApiKey(ctx context.Context, apiKey string) (string, error) {
	var envID string
	err := db.WithContext(ctx).Table("environments").
		Select("environments.id").
		Joins("INNER JOIN api_keys ON api_keys.id = environments.api_key_id").
		Where("api_keys.key = ?", apiKey).
		Pluck("environments.id", &envID).Error
	if err != nil {
		return "", err
	}
	if envID == "" {
		return "", gorm.ErrRecordNotFound
	}
	return envID, nil
}

func (db *DB) Close() error {
	sqlDB, err := db.SQLDB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (db *DB) SQLDB() (*sql.DB, error) {
	return db.DB.DB()
}

// Create parent directory for file-based SQLite if needed
func ensureSQLiteDirectoryInternal(connString string) error {
	if !strings.HasPrefix(connString, "file:") {
		return nil
	}
	pathPart, err := kit.SQLitePathFromDSN(connString)
	if err != nil {
		return fmt.Errorf("failed to parse SQLite DSN: %w", err)
	}
	if pathPart == "" || strings.HasPrefix(pathPart, ":memory:") {
		return nil
	}

	dir := filepath.Dir(pathPart)
	if dir == "" || dir == "." {
		return nil
	}
	// os.* rather than acfs: this creates the sqlite data directory itself,
	// which has to exist before any acfs root could be opened on it.
	return os.MkdirAll(dir, 0o755)
}
