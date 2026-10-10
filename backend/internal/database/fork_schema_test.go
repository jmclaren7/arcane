package database

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureForkSchema_AddsInjectCommitEnvIdempotently(t *testing.T) {
	ctx := t.Context()
	rawDB, _ := newSQLiteSQLDB(t, t.TempDir(), "fork-schema.db")
	require.NoError(t, migrateDatabase(ctx, rawDB, dbProviderSQLite, MigrationOptions{}, latestMigrationVersion))

	exists, err := sqliteColumnExists(ctx, rawDB, forkGitOpsSyncsTable, forkInjectCommitEnvColumn)
	require.NoError(t, err)
	require.False(t, exists, "upstream's schema must not carry the fork column on its own")

	require.NoError(t, ensureForkSchema(ctx, rawDB, dbProviderSQLite))
	require.NoError(t, ensureForkSchema(ctx, rawDB, dbProviderSQLite))

	exists, err = sqliteColumnExists(ctx, rawDB, forkGitOpsSyncsTable, forkInjectCommitEnvColumn)
	require.NoError(t, err)
	require.True(t, exists)
}
