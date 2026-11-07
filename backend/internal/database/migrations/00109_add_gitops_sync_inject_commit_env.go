package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

// This fork migration has shipped under a series of version numbers that
// upstream later claimed for migrations of its own, so a lab database may
// already carry gitops_syncs.inject_commit_env under an older number. Goose
// keys its bookkeeping on the version number alone and would re-run this one,
// so the column is added only when it is missing rather than unconditionally:
// that is what keeps the next renumbering from aborting startup with
// "duplicate column name: inject_commit_env". The matching bookkeeping repair
// for the upstream migrations such a database skipped lives in
// internal/database/forkmigration.go.
const (
	v109SQLiteAdd = `
ALTER TABLE gitops_syncs ADD COLUMN inject_commit_env BOOLEAN NOT NULL DEFAULT false;
`
	v109PostgresAdd = `
ALTER TABLE gitops_syncs ADD COLUMN IF NOT EXISTS inject_commit_env BOOLEAN NOT NULL DEFAULT false;
`
	v109SQLiteDown = `
ALTER TABLE gitops_syncs DROP COLUMN inject_commit_env;
`
	v109PostgresDown = `
ALTER TABLE gitops_syncs DROP COLUMN IF EXISTS inject_commit_env;
`
)

func init() {
	goose.AddMigrationContext(up109, run(v109SQLiteDown, v109PostgresDown))
}

func up109(ctx context.Context, tx *sql.Tx) error {
	present, err := columnExists109(ctx, tx)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	return run(v109SQLiteAdd, v109PostgresAdd)(ctx, tx)
}

func columnExists109(ctx context.Context, tx *sql.Tx) (bool, error) {
	query := `SELECT count(*) FROM pragma_table_info('gitops_syncs') WHERE name = 'inject_commit_env'`
	if Dialect == goose.DialectPostgres {
		query = `SELECT count(*) FROM information_schema.columns WHERE table_name = 'gitops_syncs' AND column_name = 'inject_commit_env'`
	}
	var count int
	if err := tx.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return false, fmt.Errorf("check gitops_syncs.inject_commit_env: %w", err)
	}
	return count > 0, nil
}
