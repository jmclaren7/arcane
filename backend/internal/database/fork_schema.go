package database

import (
	"context"
	"database/sql"
	"fmt"
)

// Fork-only schema (jmclaren7/arcane change #7). It is applied idempotently after
// the Goose run instead of as a numbered migration, so the fork never claims a
// version number upstream will later reuse.
const (
	forkGitOpsSyncsTable          = "gitops_syncs"
	forkInjectCommitEnvColumn     = "inject_commit_env"
	forkInjectCommitEnvSQLiteDDL  = "ALTER TABLE gitops_syncs ADD COLUMN inject_commit_env BOOLEAN NOT NULL DEFAULT false"
	forkInjectCommitEnvPostgreDDL = "ALTER TABLE gitops_syncs ADD COLUMN IF NOT EXISTS inject_commit_env BOOLEAN NOT NULL DEFAULT false"
)

// ensureForkSchema adds the fork's columns when they are missing.
func ensureForkSchema(ctx context.Context, db *sql.DB, dbProvider string) error {
	if dbProvider == dbProviderPostgres {
		if _, err := db.ExecContext(ctx, forkInjectCommitEnvPostgreDDL); err != nil {
			return fmt.Errorf("failed to add %s.%s: %w", forkGitOpsSyncsTable, forkInjectCommitEnvColumn, err)
		}
		return nil
	}

	exists, err := injectCommitEnvColumnExists(ctx, db, dbProvider)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err = db.ExecContext(ctx, forkInjectCommitEnvSQLiteDDL); err != nil {
		return fmt.Errorf("failed to add %s.%s: %w", forkGitOpsSyncsTable, forkInjectCommitEnvColumn, err)
	}
	return nil
}

func injectCommitEnvColumnExists(ctx context.Context, db *sql.DB, dbProvider string) (bool, error) {
	query := "SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?"
	if dbProvider == dbProviderPostgres {
		query = "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2"
	}
	var count int
	if err := db.QueryRowContext(ctx, query, forkGitOpsSyncsTable, forkInjectCommitEnvColumn).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to inspect %s.%s: %w", forkGitOpsSyncsTable, forkInjectCommitEnvColumn, err)
	}
	return count > 0, nil
}
