package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

// Fork-only debt (jmclaren7/arcane change #8). Fork builds up to the upstream
// 2.17.x line recorded change #7's column as Goose version 95. Upstream's v3
// Goose baseline now owns 95 (drop_retired_schedule_defaults), so on such a
// database Goose treats upstream's 95 as applied and skips it.
const (
	forkLastRecordedMigrationVersion int64 = 95
	// Body of upstream's 00095_drop_retired_schedule_defaults.go; it is valid on both dialects.
	forkReplayUpstream95SQL = `DELETE FROM settings WHERE key IN ('dockerClientRefreshInterval', 'autoHealInterval') AND value = '*/30 * * * * *'`
)

var errForkPreRenumberDatabase = errors.New(
	"this database was migrated by a fork build older than the 2.17.x line and still carries change #7's migration under a renumbered version; " +
		"start the fork's last v2 build (commit 0950664) once so it repairs the database, then start this build",
)

// repairForkMigrationState runs before the Goose run. It replays upstream's
// version 95 on a database where the fork's migration claimed that number, and
// refuses to migrate a database whose older fork bookkeeping only the v2 fork
// build can repair.
func repairForkMigrationState(ctx context.Context, db *sql.DB, dbProvider string) error {
	tableExists, err := forkGooseVersionTableExists(ctx, db, dbProvider)
	if err != nil || !tableExists {
		return err
	}
	forkColumn, err := injectCommitEnvColumnExists(ctx, db, dbProvider)
	if err != nil || !forkColumn {
		return err
	}

	var current sql.NullInt64
	if err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT MAX(version_id) FROM %s WHERE is_applied = TRUE", gooseVersionTable)).Scan(&current); err != nil {
		return fmt.Errorf("failed to read the current migration version: %w", err)
	}

	switch {
	case current.Int64 < forkLastRecordedMigrationVersion:
		return errForkPreRenumberDatabase
	case current.Int64 == forkLastRecordedMigrationVersion:
		if _, err = db.ExecContext(ctx, forkReplayUpstream95SQL); err != nil {
			return fmt.Errorf("failed to replay upstream migration %d: %w", forkLastRecordedMigrationVersion, err)
		}
		slog.InfoContext(ctx, "Replayed the upstream migration a fork build had recorded as its own", "version", forkLastRecordedMigrationVersion)
	}
	return nil
}

func forkGooseVersionTableExists(ctx context.Context, db *sql.DB, dbProvider string) (bool, error) {
	query := "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?"
	if dbProvider == dbProviderPostgres {
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = $1"
	}
	var count int
	if err := db.QueryRowContext(ctx, query, gooseVersionTable).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to inspect %s: %w", gooseVersionTable, err)
	}
	return count > 0, nil
}
