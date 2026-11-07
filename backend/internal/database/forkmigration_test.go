package database

import (
	stdsql "database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/database/migrations"
)

// forkEraSQLiteDB brings a database up to the v2.17.1 baseline, then rewrites
// its bookkeeping to look like a fork build that shipped the commit-injection
// migration under era, which is what makes Goose skip the upstream migration
// that now owns that number.
func forkEraSQLiteDB(t *testing.T, era int64) (*stdsql.DB, string) {
	t.Helper()
	ctx := t.Context()
	rawDB, dsn := newSQLiteSQLDB(t, t.TempDir(), "arcane-fork-era.db")
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, migrations.BaselineVersion))
	for version := int64(1); version < migrations.BaselineVersion; version++ {
		_, err := rawDB.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, true)`, version)
		require.NoError(t, err)
	}

	// The fork migration ran under `era`, so its column is present while the
	// upstream migration that owns the number never ran.
	_, err := rawDB.ExecContext(ctx, `ALTER TABLE gitops_syncs ADD COLUMN inject_commit_env BOOLEAN NOT NULL DEFAULT false`)
	require.NoError(t, err)
	if era > migrations.BaselineVersion {
		_, err = rawDB.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, true)`, era)
		require.NoError(t, err)
	}
	return rawDB, dsn
}

func columnPresent(t *testing.T, db *stdsql.DB, table, column string) bool {
	t.Helper()
	present, err := columnExists(t.Context(), db, dbProviderSQLite, table, column)
	require.NoError(t, err)
	return present
}

func settingValue(t *testing.T, db *stdsql.DB, key string) string {
	t.Helper()
	var value string
	err := db.QueryRowContext(t.Context(), `SELECT COALESCE(MAX(value), '') FROM settings WHERE key = ?`, key).Scan(&value)
	require.NoError(t, err)
	return value
}

func TestRepairRenumberedForkMigration_ImageUpdateSeenEra(t *testing.T) {
	ctx := t.Context()
	rawDB, dsn := forkEraSQLiteDB(t, forkCommitEnvImageUpdateSeenEra)
	// Upstream's own 094 is what creates this column, and the era's bookkeeping
	// is exactly what makes Goose skip it.
	_, err := rawDB.ExecContext(ctx, `ALTER TABLE image_updates DROP COLUMN latest_seen_at`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx,
		`INSERT INTO image_updates (id, repository, tag, has_update, current_version, check_time)
		 VALUES ('iu1', 'app', 'latest', 1, 'v1', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))

	assert.True(t, columnPresent(t, rawDB, "image_updates", "latest_seen_at"),
		"the repair must replay the upstream migration the era's bookkeeping skipped")
	var stamped int
	require.NoError(t, rawDB.QueryRowContext(ctx,
		`SELECT count(*) FROM image_updates WHERE has_update = 1 AND latest_seen_at IS NOT NULL`).Scan(&stamped))
	assert.Equal(t, 1, stamped, "rows already flagged has_update must be stamped by the backfill")

	versions := sqliteMigrationVersions(t)
	assert.Equal(t, versions[len(versions)-1], readGooseSQLiteVersion(t, dsn))
	assert.True(t, columnPresent(t, rawDB, "gitops_syncs", "inject_commit_env"))
}

func TestRepairRenumberedForkMigration_RetiredScheduleEra(t *testing.T) {
	ctx := t.Context()
	rawDB, dsn := forkEraSQLiteDB(t, forkCommitEnvRetiredScheduleEra)
	// Upstream's 00095 deletes these two retired defaults; the era's bookkeeping
	// skips it, so without the repair 00101 rewrites them to five-field form and
	// keeps them instead of letting them fall back to the current default.
	for _, key := range []string{"dockerClientRefreshInterval", "autoHealInterval"} {
		_, err := rawDB.ExecContext(ctx,
			`INSERT INTO settings (key, value) VALUES (?, '*/30 * * * * *')`, key)
		require.NoError(t, err)
	}

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))

	assert.Empty(t, settingValue(t, rawDB, "dockerClientRefreshInterval"))
	assert.Empty(t, settingValue(t, rawDB, "autoHealInterval"))
	versions := sqliteMigrationVersions(t)
	assert.Equal(t, versions[len(versions)-1], readGooseSQLiteVersion(t, dsn))
	assert.True(t, columnPresent(t, rawDB, "gitops_syncs", "inject_commit_env"))
}

func TestRepairRenumberedForkMigration_LeavesUpstreamDatabaseAlone(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "arcane-upstream.db")
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, migrations.BaselineVersion))
	for _, key := range []string{"dockerClientRefreshInterval", "autoHealInterval"} {
		_, err := rawDB.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, '*/30 * * * * *')`, key)
		require.NoError(t, err)
	}

	// No inject_commit_env column, so this is a pristine v2.17.1 database at the
	// same version: the repair must not fire and upstream's own 00095 does the
	// delete itself.
	require.NoError(t, repairRenumberedForkMigration(ctx, rawDB, dbProviderSQLite, migrations.BaselineVersion, forkCommitEnvMigrationVersion))
	assert.Equal(t, "*/30 * * * * *", settingValue(t, rawDB, "autoHealInterval"))

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	assert.Empty(t, settingValue(t, rawDB, "autoHealInterval"))
	assert.True(t, columnPresent(t, rawDB, "gitops_syncs", "inject_commit_env"))
}
