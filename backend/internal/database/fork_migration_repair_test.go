package database

import (
	stdsql "database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/database/migrations"
)

// newForkEraDatabase builds the shape a v2 fork build left behind: upstream's
// v2.17.1 schema plus change #7's column, recorded as Goose version 95.
func newForkEraDatabase(t *testing.T, recordVersion95 bool) *stdsql.DB {
	t.Helper()
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "fork-era.db")
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, migrations.BaselineVersion))
	_, err := rawDB.ExecContext(ctx, forkInjectCommitEnvSQLiteDDL)
	require.NoError(t, err)
	if recordVersion95 {
		_, err = rawDB.ExecContext(ctx, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (95, TRUE)")
		require.NoError(t, err)
	}
	return rawDB
}

func TestRepairForkMigrationState_ReplaysUpstream95OnForkEraDatabase(t *testing.T) {
	ctx := t.Context()
	rawDB := newForkEraDatabase(t, true)
	_, err := rawDB.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ('autoHealInterval', '*/30 * * * * *')
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`)
	require.NoError(t, err)
	_, err = rawDB.ExecContext(ctx, `INSERT INTO gitops_syncs (id, name, environment_id, repository_id, branch, compose_path, project_name, inject_commit_env)
		VALUES ('sync-1', 'demo', '0', 'repo-1', 'main', 'compose.yaml', 'demo', TRUE)`)
	require.NoError(t, err)

	require.NoError(t, repairForkMigrationState(ctx, rawDB, dbProviderSQLite))
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))
	require.NoError(t, ensureForkSchema(ctx, rawDB, dbProviderSQLite))

	var retired int
	require.NoError(t, rawDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM settings WHERE key = 'autoHealInterval' AND value = '*/30 * * * * *'").Scan(&retired))
	assert.Zero(t, retired, "upstream's skipped migration 95 must have been replayed")

	var injectCommitEnv bool
	require.NoError(t, rawDB.QueryRowContext(ctx, "SELECT inject_commit_env FROM gitops_syncs WHERE id = 'sync-1'").Scan(&injectCommitEnv))
	assert.True(t, injectCommitEnv, "the operator's opt-in must survive the upgrade")

	// A second startup is a no-op.
	require.NoError(t, repairForkMigrationState(ctx, rawDB, dbProviderSQLite))
}

func TestRepairForkMigrationState_RejectsPreRenumberForkDatabase(t *testing.T) {
	rawDB := newForkEraDatabase(t, false)

	err := repairForkMigrationState(t.Context(), rawDB, dbProviderSQLite)
	require.ErrorIs(t, err, errForkPreRenumberDatabase)
}

func TestRepairForkMigrationState_LeavesUpstreamDatabasesAlone(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "upstream.db")
	require.NoError(t, repairForkMigrationState(ctx, rawDB, dbProviderSQLite), "a fresh database has nothing to repair")

	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, migrations.BaselineVersion))
	require.NoError(t, repairForkMigrationState(ctx, rawDB, dbProviderSQLite), "an upstream v2.17.1 database has no fork column")
}
