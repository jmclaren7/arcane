package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// Fork-only debt. This fork's GitOps commit-injection migration has shipped
// under a series of version numbers that upstream later claimed for migrations
// of its own. Goose keys its bookkeeping on the version number alone, so a
// database migrated by one of those builds has the upstream migration that now
// owns the recorded number treated as applied and silently skipped.
//
// Under v3 only two of those eras can still reach this code. migrateDatabase
// refuses any database below the v2.17.1 schema baseline (94) and tells the
// operator to upgrade through v2.17.1 first, which for a fork install means a
// fork build whose own repair has already lifted anything older to 95. What is
// left:
//
//   - A database whose newest applied version is 94 ran the fork migration as
//     094, so upstream's own 094 (image_updates.latest_seen_at, plus the
//     backfill stamping rows already flagged has_update) was skipped. The
//     baseline at 00094 is skipped too, and it is what would otherwise have
//     created that column, so the column is simply absent.
//   - A database whose newest applied version is 95 ran the fork migration as
//     095, so upstream's 00095 (deleting the two retired */30-second schedule
//     defaults) is skipped. Left in place, 00101 would rewrite those values to
//     five-field form and keep them instead of falling back to the new default.
//
// repairRenumberedForkMigration replays exactly those statements before the
// Goose run, without touching the operator's data. It runs outside Goose's
// Postgres session lock, which is harmless: a second instance re-runs the same
// checks and finds nothing to do, and every statement here is idempotent. The
// fork migration itself (00109) adds its column only when it is missing, so no
// bookkeeping has to be forged. All of this can be deleted once no database
// migrated by a pre-095 fork build is left in the wild.
const (
	// forkCommitEnvMigrationVersion is the fork migration's current number.
	forkCommitEnvMigrationVersion int64 = 109
	// forkCommitEnvImageUpdateSeenEra is the era whose fork migration took the
	// number of upstream's image_updates.latest_seen_at migration.
	forkCommitEnvImageUpdateSeenEra int64 = 94
	// forkCommitEnvRetiredScheduleEra is the era whose fork migration took the
	// number of upstream's retired-schedule-defaults cleanup.
	forkCommitEnvRetiredScheduleEra int64 = 95

	forkRepairSQLiteSeenAt = `
ALTER TABLE image_updates ADD COLUMN latest_seen_at DATETIME;
UPDATE image_updates SET latest_seen_at = CURRENT_TIMESTAMP WHERE has_update = 1;
`
	forkRepairPostgresSeenAt = `
ALTER TABLE image_updates ADD COLUMN IF NOT EXISTS latest_seen_at TIMESTAMPTZ;
UPDATE image_updates SET latest_seen_at = CURRENT_TIMESTAMP WHERE has_update = true;
`
	forkRepairRetiredSchedules = `
DELETE FROM settings WHERE key IN ('dockerClientRefreshInterval', 'autoHealInterval') AND value = '*/30 * * * * *';
`
)

func repairRenumberedForkMigration(ctx context.Context, db *sql.DB, dbProvider string, currentVersion, requiredVersion int64) error {
	if requiredVersion < forkCommitEnvMigrationVersion {
		return nil
	}
	if currentVersion != forkCommitEnvImageUpdateSeenEra && currentVersion != forkCommitEnvRetiredScheduleEra {
		return nil
	}

	// gitops_syncs.inject_commit_env is the fork migration's fingerprint: a
	// pristine upstream database at the same version does not carry it, and must
	// not be touched.
	forkMigrated, err := columnExists(ctx, db, dbProvider, "gitops_syncs", "inject_commit_env")
	if err != nil || !forkMigrated {
		return err
	}

	statements := forkRepairRetiredSchedules
	if currentVersion == forkCommitEnvImageUpdateSeenEra {
		seenAtPresent, seenAtErr := columnExists(ctx, db, dbProvider, "image_updates", "latest_seen_at")
		if seenAtErr != nil {
			return seenAtErr
		}
		if seenAtPresent {
			return nil
		}
		statements = sqliteOr(dbProvider, forkRepairSQLiteSeenAt, forkRepairPostgresSeenAt)
	}

	slog.WarnContext(ctx,
		"Repairing migration bookkeeping left by a pre-renumber fork build",
		"provider", dbProvider,
		"recordedVersion", currentVersion,
		"forkMigrationVersion", forkCommitEnvMigrationVersion)

	if _, err = db.ExecContext(ctx, statements); err != nil {
		return fmt.Errorf("failed to repair migration bookkeeping left by a pre-renumber fork build for %s: %w", dbProvider, err)
	}
	return nil
}

func sqliteOr(dbProvider, sqliteStatements, postgresStatements string) string {
	if dbProvider == dbProviderPostgres {
		return postgresStatements
	}
	return sqliteStatements
}

func columnExists(ctx context.Context, db *sql.DB, dbProvider, table, column string) (bool, error) {
	query := `SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`
	if dbProvider == dbProviderPostgres {
		query = `SELECT count(*) FROM information_schema.columns WHERE table_name = $1 AND column_name = $2`
	}
	var count int
	if err := db.QueryRowContext(ctx, query, table, column).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to inspect %s.%s for %s: %w", table, column, dbProvider, err)
	}
	return count > 0, nil
}
