package database

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrateDatabase_RepairsPreRenumberForkMigrationState reproduces the databases
// produced by fork builds between f3b8e1e and 130b45f, which applied the GitOps
// commit-injection migration as version 69 before it was renumbered (ultimately to
// 090). Such a database has inject_commit_env already (so re-running the fork
// migration aborts with "duplicate column name") and is missing
// container_registries.repository_names (because Goose treated upstream's 069 as
// applied and skipped it).
func TestMigrateDatabase_RepairsPreRenumberForkMigrationState(t *testing.T) {
	testCases := []struct {
		name           string
		reachedVersion int64
	}{
		// The database never left the pre-renumber build.
		{name: "left at the pre-renumber version", reachedVersion: forkCommitEnvPreRenumberVersion},
		// The database was started once on a later build whose fork migration failed:
		// everything below it applied, the fork migration itself did not.
		{name: "already advanced past 093", reachedVersion: forkCommitEnvMigrationVersion - 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-pre-renumber.db")
			seedPreRenumberForkDatabaseInternal(t, ctx, rawDB)

			if testCase.reachedVersion > forkCommitEnvPreRenumberVersion {
				require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, testCase.reachedVersion))
				// Goose keys on the version number alone, so upstream's 069 was skipped.
				assert.False(t, sqliteColumnExistsInternal(t, rawDB, "container_registries", "repository_names"))
			}
			require.Equal(t, testCase.reachedVersion, readGooseSQLiteVersion(t, dsn))

			require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))

			highestVersion, err := getHighestEmbeddedMigrationVersionInternal(dbProviderSQLite)
			require.NoError(t, err)
			assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))

			// The repair must leave the same schema a from-scratch migration produces,
			// including the skipped 069 column and 070's passkey tables.
			freshDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-fresh.db")
			require.NoError(t, migrateDatabase(ctx, freshDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
			for _, table := range []string{"container_registries", "gitops_syncs", "users", "user_sessions", "passkeys"} {
				assert.Equal(t, sqliteTableColumnsInternal(t, freshDB, table), sqliteTableColumnsInternal(t, rawDB, table),
					"repaired schema for %s differs from a database migrated from scratch", table)
			}

			// The opt-in survives a repair that got to run first. In the advanced
			// sub-case the migrations below 094 already ran without it, and
			// upstream's SQLite 092 rebuilt gitops_syncs before the repair could
			// capture anything, so only the schema can be put back.
			var injectCommitEnv bool
			require.NoError(t, rawDB.QueryRow(`SELECT inject_commit_env FROM gitops_syncs WHERE id = 'sync-1'`).Scan(&injectCommitEnv))
			if testCase.reachedVersion == forkCommitEnvPreRenumberVersion {
				assert.True(t, injectCommitEnv)
			}

			// Running again is a no-op rather than a second repair.
			require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
			assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))
		})
	}
}

// TestMigrateDatabase_RepairsMidRenumberForkMigrationState reproduces the databases
// produced by fork builds between 130b45f and the 2026-08-15 rebase, which applied the
// GitOps commit-injection migration as version 71 — the number upstream later gave to
// 071_rename_volume_workspace_legacy_keys.sql. Such a database has inject_commit_env
// already (so the fork migration's current number aborts with "duplicate column name") and never had the
// volume-workspace legacy keys renamed (because Goose treats upstream's 071 as
// applied and skips it).
func TestMigrateDatabase_RepairsMidRenumberForkMigrationState(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-mid-renumber.db")
	seedMidRenumberForkDatabaseInternal(t, ctx, rawDB)

	// Seed the legacy volume-workspace names upstream's 071 renames, so the repair's
	// replay has real work to do.
	_, err := rawDB.ExecContext(ctx, `DELETE FROM settings WHERE key = 'volumeHelperIdleTimeout'`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ('volumeBrowserHelperIdleTimeout', '27')`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO roles (id, name, permissions) VALUES ('role-workspace', 'Workspace role', '["volumes:browse","volumes:upload"]')`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO api_keys (id, name, key_hash, key_prefix) VALUES ('key-workspace', 'Workspace key', 'hash', 'arc_')`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO api_key_permissions (id, api_key_id, permission) VALUES ('grant-browse', 'key-workspace', 'volumes:browse')`)
	require.NoError(t, err)

	require.Equal(t, forkCommitEnvMidRenumberVersion, readGooseSQLiteVersion(t, dsn))
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))

	highestVersion, err := getHighestEmbeddedMigrationVersionInternal(dbProviderSQLite)
	require.NoError(t, err)
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))

	// Upstream's 071, which the stale bookkeeping made Goose skip, must have been
	// replayed by the repair.
	var timeout string
	require.NoError(t, rawDB.QueryRow(`SELECT value FROM settings WHERE key = 'volumeHelperIdleTimeout'`).Scan(&timeout))
	assert.Equal(t, "27", timeout)
	var count int
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = 'volumeBrowserHelperIdleTimeout'`).Scan(&count))
	assert.Zero(t, count)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM json_each((SELECT permissions FROM roles WHERE id = 'role-workspace')) WHERE value = 'volumes:read'`).Scan(&count))
	assert.Equal(t, 1, count)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM json_each((SELECT permissions FROM roles WHERE id = 'role-workspace')) WHERE value = 'volumes:browse'`).Scan(&count))
	assert.Zero(t, count)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM api_key_permissions WHERE api_key_id = 'key-workspace' AND permission = 'volumes:read'`).Scan(&count))
	assert.Equal(t, 1, count)
	require.NoError(t, rawDB.QueryRow(`SELECT COUNT(*) FROM api_key_permissions WHERE permission = 'volumes:browse'`).Scan(&count))
	assert.Zero(t, count)

	// 072 must have been applied through Goose, and the repaired schema must match a
	// from-scratch migration.
	freshDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-fresh-mid.db")
	require.NoError(t, migrateDatabase(ctx, freshDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	for _, table := range []string{"container_registries", "gitops_syncs", "project_tags", "settings"} {
		assert.Equal(t, sqliteTableColumnsInternal(t, freshDB, table), sqliteTableColumnsInternal(t, rawDB, table),
			"repaired schema for %s differs from a database migrated from scratch", table)
	}

	// The opt-in the operator had already set must survive the repair.
	var injectCommitEnv bool
	require.NoError(t, rawDB.QueryRow(`SELECT inject_commit_env FROM gitops_syncs WHERE id = 'sync-1'`).Scan(&injectCommitEnv))
	assert.True(t, injectCommitEnv)

	// Running again is a no-op rather than a second repair.
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))
}

// TestMigrateDatabase_RepairsLateRenumberForkMigrationState reproduces the databases
// produced by fork builds between the 2026-08-15 and 2026-08-22 rebases, which applied
// the GitOps commit-injection migration as version 73 — the number upstream later gave
// to 073_add_backup_support.sql. Such a database has inject_commit_env already (so 090
// aborts with "duplicate column name") and none of the backup-support schema (because
// Goose treats upstream's 073 as applied and skips it).
func TestMigrateDatabase_RepairsLateRenumberForkMigrationState(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-late-renumber.db")
	seedLateRenumberForkDatabaseInternal(t, ctx, rawDB)

	// Seed a pre-existing archive backup row so the replayed column defaults have
	// real work to do.
	_, err := rawDB.ExecContext(ctx, `INSERT INTO volume_backups (id, volume_name, size, created_at) VALUES ('backup-1', 'data', 42, CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	require.Equal(t, forkCommitEnvLateRenumberVersion, readGooseSQLiteVersion(t, dsn))
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))

	highestVersion, err := getHighestEmbeddedMigrationVersionInternal(dbProviderSQLite)
	require.NoError(t, err)
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))

	// Upstream's 073, which the stale bookkeeping made Goose skip, must have been
	// replayed by the repair: the repaired schema must match a from-scratch
	// migration, including all five backup-support tables and the new
	// volume_backups columns.
	freshDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-fresh-late.db")
	require.NoError(t, migrateDatabase(ctx, freshDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	for _, table := range []string{
		"container_registries", "gitops_syncs", "settings", "volume_backups",
		"s3_destinations", "volume_backup_policies", "system_backup_runs",
		"system_backup_policies", "system_backup_recovery_config",
	} {
		assert.Equal(t, sqliteTableColumnsInternal(t, freshDB, table), sqliteTableColumnsInternal(t, rawDB, table),
			"repaired schema for %s differs from a database migrated from scratch", table)
	}

	// The pre-existing backup row must have picked up 073's defaults.
	var format, status string
	require.NoError(t, rawDB.QueryRow(`SELECT format, status FROM volume_backups WHERE id = 'backup-1'`).Scan(&format, &status))
	assert.Equal(t, "archive", format)
	assert.Equal(t, "succeeded", status)

	// The opt-in the operator had already set must survive the repair.
	var injectCommitEnv bool
	require.NoError(t, rawDB.QueryRow(`SELECT inject_commit_env FROM gitops_syncs WHERE id = 'sync-1'`).Scan(&injectCommitEnv))
	assert.True(t, injectCommitEnv)

	// Running again is a no-op rather than a second repair.
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))
}

// TestMigrateDatabase_RepairsLastRenumberForkMigrationState reproduces the databases
// produced by fork builds between the 2026-08-22 and 2026-08-29 rebases, which applied
// the GitOps commit-injection migration as version 74 — the number upstream later gave
// to 074_add_gitops_sync_pull_redeploy.sql. Such a database has inject_commit_env
// already (so 090 aborts with "duplicate column name") and is missing the
// pull/redeploy-after-sync columns (because Goose treats upstream's 074 as applied and
// skips it).
func TestMigrateDatabase_RepairsLastRenumberForkMigrationState(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-last-renumber.db")
	seedLastRenumberForkDatabaseInternal(t, ctx, rawDB)

	// Goose keys on the version number alone, so upstream's 074 was skipped.
	require.Equal(t, forkCommitEnvLastRenumberVersion, readGooseSQLiteVersion(t, dsn))
	assert.False(t, sqliteColumnExistsInternal(t, rawDB, "gitops_syncs", "pull_image_after_sync"))

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))

	highestVersion, err := getHighestEmbeddedMigrationVersionInternal(dbProviderSQLite)
	require.NoError(t, err)
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))

	// Upstream's 074, which the stale bookkeeping made Goose skip, must have been
	// replayed by the repair, and 075/076 applied through Goose: the repaired schema
	// must match a from-scratch migration.
	freshDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-fresh-last.db")
	require.NoError(t, migrateDatabase(ctx, freshDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	for _, table := range []string{
		"container_registries", "gitops_syncs", "settings",
		"image_patches", "vulnerability_reports",
	} {
		assert.Equal(t, sqliteTableColumnsInternal(t, freshDB, table), sqliteTableColumnsInternal(t, rawDB, table),
			"repaired schema for %s differs from a database migrated from scratch", table)
	}

	// The replayed columns must carry 074's defaults on the pre-existing row.
	var pullAfterSync, redeployAfterSync bool
	require.NoError(t, rawDB.QueryRow(`SELECT pull_image_after_sync, redeploy_after_sync FROM gitops_syncs WHERE id = 'sync-1'`).Scan(&pullAfterSync, &redeployAfterSync))
	assert.False(t, pullAfterSync)
	assert.False(t, redeployAfterSync)

	// The opt-in the operator had already set must survive the repair.
	var injectCommitEnv bool
	require.NoError(t, rawDB.QueryRow(`SELECT inject_commit_env FROM gitops_syncs WHERE id = 'sync-1'`).Scan(&injectCommitEnv))
	assert.True(t, injectCommitEnv)

	// Running again is a no-op rather than a second repair.
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))
}

// TestMigrateDatabase_RepairsApnsRenumberForkMigrationState reproduces the databases
// produced by fork builds between the 2026-08-29 and 2026-09-09 rebases, which applied
// the GitOps commit-injection migration as version 77 — the number upstream later gave
// to 077_add_apns.sql. Such a database has inject_commit_env already (so 090 aborts
// with "duplicate column name") and none of the Apple push notification schema
// (because Goose treats upstream's 077 as applied and skips it).
func TestMigrateDatabase_RepairsApnsRenumberForkMigrationState(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-apns-renumber.db")
	seedApnsRenumberForkDatabaseInternal(t, ctx, rawDB)

	// Goose keys on the version number alone, so upstream's 077 was skipped.
	require.Equal(t, forkCommitEnvApnsRenumberVersion, readGooseSQLiteVersion(t, dsn))
	assert.False(t, sqliteTableExistsInternal(t, rawDB, "apns_devices"))

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))

	highestVersion, err := getHighestEmbeddedMigrationVersionInternal(dbProviderSQLite)
	require.NoError(t, err)
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))

	// Upstream's 077, which the stale bookkeeping made Goose skip, must have been
	// replayed by the repair, and 078..084 applied through Goose: the repaired schema
	// must match a from-scratch migration.
	freshDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-fresh-apns.db")
	require.NoError(t, migrateDatabase(ctx, freshDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	for _, table := range []string{
		"container_registries", "gitops_syncs", "settings",
		"apns_devices", "apns_outbox",
	} {
		assert.Equal(t, sqliteTableColumnsInternal(t, freshDB, table), sqliteTableColumnsInternal(t, rawDB, table),
			"repaired schema for %s differs from a database migrated from scratch", table)
	}

	// The opt-in the operator had already set must survive the repair.
	var injectCommitEnv bool
	require.NoError(t, rawDB.QueryRow(`SELECT inject_commit_env FROM gitops_syncs WHERE id = 'sync-1'`).Scan(&injectCommitEnv))
	assert.True(t, injectCommitEnv)

	// Running again is a no-op rather than a second repair.
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))
}

// TestMigrateDatabase_RepairsBackupModeRenumberForkMigrationState reproduces the
// databases produced by fork builds between the 2026-09-09 and 2026-09-19 rebases,
// which applied the GitOps commit-injection migration as version 85 — the number
// upstream later gave to 085_add_gitops_backup_mode.sql. Such a database has
// inject_commit_env already (so 090 aborts with "duplicate column name") and none of
// the GitOps backup-mode schema (because Goose treats upstream's 085 as applied and
// skips it).
func TestMigrateDatabase_RepairsBackupModeRenumberForkMigrationState(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-backup-mode-renumber.db")
	seedBackupModeRenumberForkDatabaseInternal(t, ctx, rawDB)

	// Goose keys on the version number alone, so upstream's 085 was skipped.
	require.Equal(t, forkCommitEnvBackupModeRenumberVersion, readGooseSQLiteVersion(t, dsn))
	assert.False(t, sqliteColumnExistsInternal(t, rawDB, "gitops_syncs", "mode"))

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))

	highestVersion, err := getHighestEmbeddedMigrationVersionInternal(dbProviderSQLite)
	require.NoError(t, err)
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))

	// Upstream's 085, which the stale bookkeeping made Goose skip, must have been
	// replayed by the repair, and 086..087 applied through Goose: the repaired schema
	// must match a from-scratch migration.
	freshDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-fresh-backup-mode.db")
	require.NoError(t, migrateDatabase(ctx, freshDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	for _, table := range []string{
		"container_registries", "gitops_syncs", "git_repositories", "volume_backups", "settings",
	} {
		assert.Equal(t, sqliteTableColumnsInternal(t, freshDB, table), sqliteTableColumnsInternal(t, rawDB, table),
			"repaired schema for %s differs from a database migrated from scratch", table)
	}

	// The replayed columns must carry 085's defaults on the pre-existing row.
	var mode string
	var backupOnSave bool
	require.NoError(t, rawDB.QueryRow(`SELECT mode, backup_on_save FROM gitops_syncs WHERE id = 'sync-1'`).Scan(&mode, &backupOnSave))
	assert.Equal(t, "deploy", mode)
	assert.True(t, backupOnSave)

	// The opt-in the operator had already set must survive the repair.
	var injectCommitEnv bool
	require.NoError(t, rawDB.QueryRow(`SELECT inject_commit_env FROM gitops_syncs WHERE id = 'sync-1'`).Scan(&injectCommitEnv))
	assert.True(t, injectCommitEnv)

	// Running again is a no-op rather than a second repair.
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))
}

// TestMigrateDatabase_RepairsRiskRenumberForkMigrationState reproduces the
// databases produced by fork builds between the 2026-09-19 and 2026-09-26 rebases,
// which applied the GitOps commit-injection migration as version 88 — the number
// upstream later gave to 088_add_vulnerability_risk.sql. Such a database has
// inject_commit_env already (so 090 aborts with "duplicate column name") and none of
// the vulnerability-risk schema (because Goose treats upstream's 088 as applied and
// skips it). This era is also the first where the skipped migration is a
// prerequisite of a later one — upstream's 089 alters vulnerability_risk_snapshots —
// so the repair has to replay it before Goose runs at all.
func TestMigrateDatabase_RepairsRiskRenumberForkMigrationState(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-risk-renumber.db")
	seedRiskRenumberForkDatabaseInternal(t, ctx, rawDB)

	// Goose keys on the version number alone, so upstream's 088 was skipped.
	require.Equal(t, forkCommitEnvRiskRenumberVersion, readGooseSQLiteVersion(t, dsn))
	assert.False(t, sqliteColumnExistsInternal(t, rawDB, "vulnerability_scan_items", "cvss_score"))

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))

	highestVersion, err := getHighestEmbeddedMigrationVersionInternal(dbProviderSQLite)
	require.NoError(t, err)
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))

	// Upstream's 088, which the stale bookkeeping made Goose skip, must have been
	// replayed by the repair, and 089 applied through Goose on top of it: the
	// repaired schema must match a from-scratch migration.
	freshDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-fresh-risk.db")
	require.NoError(t, migrateDatabase(ctx, freshDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	for _, table := range []string{
		"vulnerability_scan_items", "vulnerability_threat_intel", "vulnerability_risk_snapshots", "gitops_syncs", "settings",
	} {
		assert.Equal(t, sqliteTableColumnsInternal(t, freshDB, table), sqliteTableColumnsInternal(t, rawDB, table),
			"repaired schema for %s differs from a database migrated from scratch", table)
	}

	// The opt-in the operator had already set must survive the repair.
	var injectCommitEnv bool
	require.NoError(t, rawDB.QueryRow(`SELECT inject_commit_env FROM gitops_syncs WHERE id = 'sync-1'`).Scan(&injectCommitEnv))
	assert.True(t, injectCommitEnv)

	// Running again is a no-op rather than a second repair.
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))
}

// TestMigrateDatabase_LeavesUnaffectedDatabaseAlone proves the repair never fires on a
// database that reached the version below the fork migration without ever running a
// pre-090 fork build, which must apply 090 through Goose as usual.
func TestMigrateDatabase_LeavesUnaffectedDatabaseAlone(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-unaffected.db")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, forkCommitEnvMigrationVersion-1))
	assert.True(t, sqliteColumnExistsInternal(t, rawDB, "container_registries", "repository_names"))
	assert.False(t, sqliteColumnExistsInternal(t, rawDB, "gitops_syncs", "inject_commit_env"))

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))

	highestVersion, err := getHighestEmbeddedMigrationVersionInternal(dbProviderSQLite)
	require.NoError(t, err)
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))
	assert.True(t, sqliteColumnExistsInternal(t, rawDB, "gitops_syncs", "inject_commit_env"))
}

// seedPreRenumberForkDatabaseInternal migrates to 068 and then replays what the
// pre-renumber fork build did: it applied the commit-injection migration's Up section
// and recorded it as version 69, the number upstream later gave to
// 069_add_container_registry_repository_names.sql.
func seedPreRenumberForkDatabaseInternal(t *testing.T, ctx context.Context, db *stdsql.DB) {
	t.Helper()

	require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, forkCommitEnvPreRenumberVersion-1))
	applyForkCommitEnvMigrationInternal(t, ctx, db, forkCommitEnvPreRenumberVersion)
	seedForkGitOpsSyncRowInternal(t, ctx, db)
}

// seedMidRenumberForkDatabaseInternal migrates to 070 and then replays what a
// 071-era fork build did: it applied the commit-injection migration's Up section
// and recorded it as version 71, the number upstream later gave to
// 071_rename_volume_workspace_legacy_keys.sql.
func seedMidRenumberForkDatabaseInternal(t *testing.T, ctx context.Context, db *stdsql.DB) {
	t.Helper()

	require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, forkCommitEnvMidRenumberVersion-1))
	applyForkCommitEnvMigrationInternal(t, ctx, db, forkCommitEnvMidRenumberVersion)
	seedForkGitOpsSyncRowInternal(t, ctx, db)
}

// seedLateRenumberForkDatabaseInternal migrates to 072 and then replays what a
// 073-era fork build did: it applied the commit-injection migration's Up section
// and recorded it as version 73, the number upstream later gave to
// 073_add_backup_support.sql.
func seedLateRenumberForkDatabaseInternal(t *testing.T, ctx context.Context, db *stdsql.DB) {
	t.Helper()

	require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, forkCommitEnvLateRenumberVersion-1))
	applyForkCommitEnvMigrationInternal(t, ctx, db, forkCommitEnvLateRenumberVersion)
	seedForkGitOpsSyncRowInternal(t, ctx, db)
}

// seedLastRenumberForkDatabaseInternal migrates to 073 and then replays what a
// 074-era fork build did: it applied the commit-injection migration's Up section
// and recorded it as version 74, the number upstream later gave to
// 074_add_gitops_sync_pull_redeploy.sql.
func seedLastRenumberForkDatabaseInternal(t *testing.T, ctx context.Context, db *stdsql.DB) {
	t.Helper()

	require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, forkCommitEnvLastRenumberVersion-1))
	applyForkCommitEnvMigrationInternal(t, ctx, db, forkCommitEnvLastRenumberVersion)
	seedForkGitOpsSyncRowInternal(t, ctx, db)
}

// seedApnsRenumberForkDatabaseInternal migrates to 076 and then replays what a
// 077-era fork build did: it applied the commit-injection migration's Up section
// and recorded it as version 77, the number upstream later gave to
// 077_add_apns.sql.
func seedApnsRenumberForkDatabaseInternal(t *testing.T, ctx context.Context, db *stdsql.DB) {
	t.Helper()

	require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, forkCommitEnvApnsRenumberVersion-1))
	applyForkCommitEnvMigrationInternal(t, ctx, db, forkCommitEnvApnsRenumberVersion)
	seedForkGitOpsSyncRowInternal(t, ctx, db)
}

// seedBackupModeRenumberForkDatabaseInternal migrates to 084 and then replays what
// an 085-era fork build did: it applied the commit-injection migration's Up section
// and recorded it as version 85, the number upstream later gave to
// 085_add_gitops_backup_mode.sql.
func seedBackupModeRenumberForkDatabaseInternal(t *testing.T, ctx context.Context, db *stdsql.DB) {
	t.Helper()

	require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, forkCommitEnvBackupModeRenumberVersion-1))
	applyForkCommitEnvMigrationInternal(t, ctx, db, forkCommitEnvBackupModeRenumberVersion)
	seedForkGitOpsSyncRowInternal(t, ctx, db)
}

// seedRiskRenumberForkDatabaseInternal migrates to 087 and then replays what an
// 088-era fork build did: it applied the commit-injection migration's Up section
// and recorded it as version 88, the number upstream later gave to
// 088_add_vulnerability_risk.sql.
func seedRiskRenumberForkDatabaseInternal(t *testing.T, ctx context.Context, db *stdsql.DB) {
	t.Helper()

	require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, forkCommitEnvRiskRenumberVersion-1))
	applyForkCommitEnvMigrationInternal(t, ctx, db, forkCommitEnvRiskRenumberVersion)
	seedForkGitOpsSyncRowInternal(t, ctx, db)
}

// applyForkCommitEnvMigrationInternal applies the fork commit-injection migration's
// Up section and records it under the version number the fork shipped it as at the
// time being simulated.
func applyForkCommitEnvMigrationInternal(t *testing.T, ctx context.Context, db *stdsql.DB, recordedVersion int64) {
	t.Helper()

	_, err := db.ExecContext(ctx, `ALTER TABLE gitops_syncs ADD COLUMN inject_commit_env BOOLEAN NOT NULL DEFAULT FALSE`)
	require.NoError(t, err)
	require.NoError(t, insertGooseMigrationVersionInternal(ctx, db, dbProviderSQLite, recordedVersion))
}

func seedForkGitOpsSyncRowInternal(t *testing.T, ctx context.Context, db *stdsql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, `INSERT INTO environments (id, api_url, status, enabled) VALUES ('env-1', 'http://localhost', 'online', TRUE)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO git_repositories (id, name, url, auth_type) VALUES ('repo-1', 'lab', 'https://example.test/lab.git', 'none')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
INSERT INTO gitops_syncs (id, name, environment_id, repository_id, branch, compose_path, project_name, created_at, updated_at, inject_commit_env)
VALUES ('sync-1', 'lab', 'env-1', 'repo-1', 'main', 'compose.yaml', 'lab', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, TRUE)`)
	require.NoError(t, err)
}

func sqliteTableExistsInternal(t *testing.T, db *stdsql.DB, table string) bool {
	t.Helper()

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count))
	return count > 0
}

func sqliteColumnExistsInternal(t *testing.T, db *stdsql.DB, table, column string) bool {
	t.Helper()

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count))
	return count > 0
}

func sqliteTableColumnsInternal(t *testing.T, db *stdsql.DB, table string) []string {
	t.Helper()

	rows, err := db.Query(`SELECT name, type, "notnull", COALESCE(dflt_value, '') FROM pragma_table_info(?) ORDER BY name`, table)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, rows.Close())
	}()

	var columns []string
	for rows.Next() {
		var name, columnType, defaultValue string
		var notNull int
		require.NoError(t, rows.Scan(&name, &columnType, &notNull, &defaultValue))
		columns = append(columns, fmt.Sprintf("%s %s notnull=%d default=%s", name, columnType, notNull, defaultValue))
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, columns, "table %s has no columns", table)

	return columns
}

// TestMigrateDatabase_RepairsDeduplicationRenumberForkMigrationState reproduces the
// databases produced by fork builds between the 2026-09-26 and 2026-10-10 rebases,
// which applied the GitOps commit-injection migration as version 90 — the number
// upstream later gave to 090_add_event_deduplication_key.sql. Such a database has
// inject_commit_env already and lacks events.deduplication_key (because Goose
// treats upstream's 090 as applied and skips it). This era is also the first where
// an upstream migration below the fork's new number *removes* the fork column:
// SQLite's 092 rebuilds gitops_syncs from an explicit column list, so the repair
// has to let 094 run for real and restore the operator's per-sync values.
func TestMigrateDatabase_RepairsDeduplicationRenumberForkMigrationState(t *testing.T) {
	ctx := context.Background()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-dedup-renumber.db")
	seedDeduplicationRenumberForkDatabaseInternal(t, ctx, rawDB)

	// Goose keys on the version number alone, so upstream's 090 was skipped.
	require.Equal(t, forkCommitEnvDeduplicationRenumberVersion, readGooseSQLiteVersion(t, dsn))
	assert.False(t, sqliteColumnExistsInternal(t, rawDB, "events", "deduplication_key"))

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))

	highestVersion, err := getHighestEmbeddedMigrationVersionInternal(dbProviderSQLite)
	require.NoError(t, err)
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))

	// Upstream's 090, which the stale bookkeeping made Goose skip, must have been
	// replayed, and the repaired schema must match a from-scratch migration.
	freshDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-fresh-dedup.db")
	require.NoError(t, migrateDatabase(ctx, freshDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	for _, table := range []string{"events", "gitops_syncs", "user_role_assignments", "template_registries", "settings"} {
		assert.Equal(t, sqliteTableColumnsInternal(t, freshDB, table), sqliteTableColumnsInternal(t, rawDB, table),
			"repaired schema for %s differs from a database migrated from scratch", table)
	}

	// Upstream's SQLite 092 rebuilds gitops_syncs without inject_commit_env, so this
	// is the assertion that proves the repair re-applied 094 and put the values back.
	var injectCommitEnv bool
	require.NoError(t, rawDB.QueryRow(`SELECT inject_commit_env FROM gitops_syncs WHERE id = 'sync-1'`).Scan(&injectCommitEnv))
	assert.True(t, injectCommitEnv)

	// Running again is a no-op rather than a second repair.
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	assert.Equal(t, highestVersion, readGooseSQLiteVersion(t, dsn))
}

func seedDeduplicationRenumberForkDatabaseInternal(t *testing.T, ctx context.Context, db *stdsql.DB) {
	t.Helper()

	require.NoError(t, migrateDatabase(ctx, db, dbProviderSQLite, MigrationOptions{}, forkCommitEnvDeduplicationRenumberVersion-1))
	applyForkCommitEnvMigrationInternal(t, ctx, db, forkCommitEnvDeduplicationRenumberVersion)
	seedForkGitOpsSyncRowInternal(t, ctx, db)
}

// getHighestEmbeddedMigrationVersionInternal returns the newest embedded migration
// version, which a repaired database must end up on.
func getHighestEmbeddedMigrationVersionInternal(dbProvider string) (int64, error) {
	versions, err := getEmbeddedMigrationVersions(dbProvider)
	if err != nil {
		return 0, err
	}
	if len(versions) == 0 {
		return 0, fmt.Errorf("no embedded migrations found for %s", dbProvider)
	}
	return versions[len(versions)-1], nil
}
