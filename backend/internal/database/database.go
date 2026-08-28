package database

import (
	"context"
	"database/sql"
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

	"emperror.dev/errors"

	"github.com/libtnb/sqlite"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
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
)

// Prepared-statement cache bounds. GORM's PrepareStmt cache is a global LRU keyed
// by SQL text. When PrepareStmtMaxSize/PrepareStmtTTL are left at zero, GORM falls
// back to math.MaxInt entries with a 24h TTL, i.e. effectively unbounded. Because
// this codebase emits highly variable SQL (dynamic filter/sort/pagination and
// GORM's IN (?,?,...) slice expansion, whose placeholder count changes the query
// text), the cache — and the modernc.org/sqlite compiled statements it retains on
// the Go heap — grows steadily under normal use. Bounding size and TTL keeps hot
// queries prepared while evicting the long tail (evicted statements are closed).
const (
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
		return nil, errors.WrapIf(err, "failed to connect to database")
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

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Get underlying sql.DB for migrations
	sqlDB, err := db.DB.DB()
	if err != nil {
		return nil, errors.WrapIf(err, "failed to get sql.DB")
	}

	var dbProvider string
	switch {
	case strings.HasPrefix(databaseURL, "file:"):
		dbProvider = dbProviderSQLite
	case strings.HasPrefix(databaseURL, "postgres"):
		dbProvider = dbProviderPostgres
	default:
		return nil, errors.Errorf("unsupported database type in URL: %s", databaseURL)
	}

	if err := migrateDatabaseInternal(ctx, sqlDB, dbProvider, options); err != nil {
		slog.Error("Failed to run migrations", "error", err)
		return nil, errors.WrapIf(err, "failed to run migrations")
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
		connString, err := ParseSQLiteConnectionString(databaseURL)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to parse SQLite connection string")
		}
		if err := ensureSQLiteDirectoryInternal(connString); err != nil {
			return nil, errors.WrapIf(err, "failed to prepare SQLite directory")
		}
		dialector = sqlite.Open(connString)
	case strings.HasPrefix(databaseURL, "postgres"):
		dialector = postgres.Open(databaseURL)
	default:
		return nil, errors.Errorf("unsupported database type in URL: %s", databaseURL)
	}

	// Retry connection up to 3 times
	var db *gorm.DB
	var err error
	for i := 1; i <= 3; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
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
		return errors.WrapIff(err, "failed to determine target migration version for %s", dbProvider)
	}

	return migrateDatabaseToVersionInternal(ctx, db, dbProvider, options, requiredVersion)
}

func migrateDatabaseToVersionInternal(ctx context.Context, db *sql.DB, dbProvider string, options MigrationOptions, requiredVersion int64) error {
	if err := adoptLegacyMigrationStateInternal(ctx, db, dbProvider, options); err != nil {
		return err
	}

	provider, err := newGooseProviderInternal(db, dbProvider)
	if err != nil {
		return errors.WrapIff(err, "failed to create goose provider for %s", dbProvider)
	}

	currentVersion, err := provider.GetDBVersion(ctx)
	if err != nil {
		return errors.WrapIff(err, "failed to determine current migration version for %s", dbProvider)
	}

	slog.Info("Resolved database migration state", "provider", dbProvider, "currentVersion", currentVersion, "requiredVersion", requiredVersion)

	if currentVersion > requiredVersion {
		if !options.AllowDowngrade {
			return errors.Errorf("database schema version %d is newer than this Arcane binary supports (target %d for %s); downgrade requires ALLOW_DOWNGRADE=true and a database backup before startup", currentVersion, requiredVersion, dbProvider)
		}

		missingVersions, err := missingEmbeddedDowngradeMigrationsInternal(ctx, db, dbProvider, requiredVersion)
		if err != nil {
			return err
		}
		if len(missingVersions) > 0 {
			return errors.Errorf("cannot downgrade database from version %d to %d for %s: embedded Goose migrations are missing for applied version(s) %v, so the rollback SQL is unavailable in this Arcane binary; ALLOW_DOWNGRADE=true is not sufficient, restore the database from a backup taken before the newer schema was applied", currentVersion, requiredVersion, dbProvider, missingVersions)
		}

		if _, err := provider.DownTo(ctx, requiredVersion); err != nil {
			return errors.WrapIff(err, "failed to downgrade database from version %d to %d for %s using embedded Goose migrations", currentVersion, requiredVersion, dbProvider)
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

	if _, err := provider.UpTo(ctx, requiredVersion); err != nil {
		return errors.WrapIff(err, "failed to apply embedded Goose migrations for %s", dbProvider)
	}

	slog.Info("Database migrations completed successfully", "provider", dbProvider, "targetVersion", requiredVersion)
	return nil
}

// This fork's GitOps commit-injection migration has shipped under three version
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
//
// Goose keys its bookkeeping on the version number alone, so a database migrated
// by a build from any of those windows is wrong in two ways: the upstream
// migration that now owns the recorded number is treated as applied and silently
// skipped, and gitops_syncs.inject_commit_env already exists, so re-running the
// fork migration under its new number aborts with "duplicate column name:
// inject_commit_env" and Arcane refuses to start.
//
// repairPreRenumberForkMigrationInternal detects all three states and repairs them
// in place, without touching the operator's data: it applies everything below the
// fork migration's current number through Goose, replays the upstream migration(s)
// the stale bookkeeping made Goose skip, and records the fork migration as applied
// instead of re-running its DDL. It runs outside Goose's Postgres session lock,
// which is harmless: a second instance re-runs the same checks and finds nothing to
// do, and the worst a genuine race can leave behind is a duplicate version row,
// which Goose collapses when it reads its state. It can be deleted once no database
// migrated by a pre-074 fork build is left in the wild.
const (
	forkCommitEnvMigrationVersion    int64 = 74
	forkCommitEnvLateRenumberVersion int64 = 73
	forkCommitEnvMidRenumberVersion  int64 = 71
	forkCommitEnvPreRenumberVersion  int64 = 69
)

func repairPreRenumberForkMigrationInternal(ctx context.Context, db *sql.DB, dbProvider string, provider *goose.Provider, currentVersion, requiredVersion int64) error {
	if requiredVersion < forkCommitEnvMigrationVersion || currentVersion >= forkCommitEnvMigrationVersion {
		return nil
	}

	needsRepair, err := hasPreRenumberForkMigrationStateInternal(ctx, db, dbProvider, currentVersion)
	if err != nil || !needsRepair {
		return err
	}

	// Decide *before* the UpTo below whether versions 71 and 73 are already
	// recorded: on a 069-era database neither is, and the UpTo applies upstream's
	// real 071 and 073 itself (recording them); on a 071-era database 71 is
	// recorded for the fork's migration, so Goose will skip upstream's 071 and its
	// rename statements have to be replayed by hand; on a 073-era database 73 is
	// recorded for the fork's migration, so Goose will skip upstream's 073 and its
	// backup-support DDL has to be replayed by hand.
	midRenumberVersionRecorded, err := gooseMigrationVersionAppliedInternal(ctx, db, dbProvider, forkCommitEnvMidRenumberVersion)
	if err != nil {
		return err
	}
	lateRenumberVersionRecorded, err := gooseMigrationVersionAppliedInternal(ctx, db, dbProvider, forkCommitEnvLateRenumberVersion)
	if err != nil {
		return err
	}

	slog.Warn("Detected a database migrated by a pre-renumber fork build; repairing migration state",
		"provider", dbProvider, "currentVersion", currentVersion, "forkMigrationVersion", forkCommitEnvMigrationVersion)

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

	// Decide outside the transaction which volume_backups columns the backup-support
	// replay still has to add: SQLite's ALTER TABLE has no IF NOT EXISTS, and on a
	// database where a crashed earlier repair already let Goose apply the real 073,
	// re-adding an existing column would abort the whole repair.
	var missingBackupColumns []string
	if lateRenumberVersionRecorded {
		missingBackupColumns, err = missingBackupSupportColumnsInternal(ctx, db, dbProvider)
		if err != nil {
			return err
		}
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

	// On a 071-era database the recorded 71 was the fork's migration, so upstream's
	// 071 (volume-workspace legacy key renames) never ran. Its statements are
	// idempotent by construction, so replaying them on a database where the renames
	// already happened (a 069-era database repaired after Goose applied the real
	// 071 above, but crashed before recording 73) is a no-op.
	if midRenumberVersionRecorded {
		if err := replaySkippedVolumeWorkspaceRenameInternal(ctx, tx, dbProvider); err != nil {
			return err
		}
	}

	// On a 073-era database the recorded 73 was the fork's migration, so upstream's
	// 073 (S3/system backup support) never ran. Every statement is guarded (IF NOT
	// EXISTS, plus the missing-column list above), so replaying on a database where
	// the schema already exists is a no-op.
	if lateRenumberVersionRecorded {
		if err := replaySkippedBackupSupportInternal(ctx, tx, dbProvider, missingBackupColumns); err != nil {
			return err
		}
	}

	// The column 074 adds is already present, so record it as applied rather than
	// re-running its DDL, which would fail on the duplicate column.
	if err := insertGooseMigrationVersionInternal(ctx, tx, dbProvider, forkCommitEnvMigrationVersion); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errors.WrapIff(err, "failed to commit pre-renumber fork migration repair for %s", dbProvider)
	}

	slog.Info("Repaired pre-renumber fork migration state",
		"provider", dbProvider, "forkMigrationVersion", forkCommitEnvMigrationVersion,
		"restoredSkippedMigration", !repositoryNamesPresent, "replayedVolumeWorkspaceRename", midRenumberVersionRecorded,
		"replayedBackupSupport", lateRenumberVersionRecorded)
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
// exists without version 74 being recorded — the signature of a fork build that applied
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
		sessionLocker, err := lock.NewPostgresSessionLocker()
		if err != nil {
			return nil, errors.WrapIf(err, "failed to create Postgres migration session locker")
		}
		options = append(options, goose.WithSessionLocker(sessionLocker))
	}

	return goose.NewProvider(dialect, db, migrationsFS, options...)
}

func embeddedMigrationFSInternal(dbProvider string) (fs.FS, error) {
	migrationsFS, err := fs.Sub(resources.FS, "migrations/"+dbProvider)
	if err != nil {
		return nil, errors.WrapIff(err, "failed to load embedded migrations for %s", dbProvider)
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
		return "", errors.Errorf("unsupported database provider: %s", dbProvider)
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
			return errors.Errorf("database schema version %d is dirty in legacy %s table; resolve it manually or set ALLOW_DOWNGRADE=true after verifying the database state", legacyState.version, legacyVersionTable)
		}

		if err := clearLegacyMigrationDirtyInternal(ctx, db, dbProvider, legacyState.version); err != nil {
			return err
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
		return errors.WrapIff(err, "failed to start legacy migration adoption transaction for %s", dbProvider)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := createGooseVersionTableInternal(ctx, tx, dbProvider); err != nil {
		return err
	}

	if err := clearGooseVersionTableInternal(ctx, tx, dbProvider); err != nil {
		return err
	}

	if err := insertGooseMigrationVersionInternal(ctx, tx, dbProvider, 0); err != nil {
		return err
	}
	versionApplied := legacyState.version == 0
	for _, version := range versions {
		if version > legacyState.version {
			break
		}
		if err := insertGooseMigrationVersionInternal(ctx, tx, dbProvider, version); err != nil {
			return err
		}
		if version == legacyState.version {
			versionApplied = true
		}
	}
	if !versionApplied {
		if err := insertGooseMigrationVersionInternal(ctx, tx, dbProvider, legacyState.version); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return errors.WrapIff(err, "failed to commit legacy migration adoption for %s", dbProvider)
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
		return legacyMigrationState{}, false, errors.WrapIff(err, "failed to read legacy migration state for %s", dbProvider)
	}

	return state, true, nil
}

func legacyVersionTableExistsInternal(ctx context.Context, db *sql.DB, dbProvider string) (bool, error) {
	switch dbProvider {
	case dbProviderSQLite:
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, legacyVersionTable).Scan(&count); err != nil {
			return false, errors.WrapIf(err, "failed to check legacy migration table for sqlite")
		}
		return count > 0, nil
	case dbProviderPostgres:
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, legacyVersionTable).Scan(&exists); err != nil {
			return false, errors.WrapIf(err, "failed to check legacy migration table for postgres")
		}
		return exists, nil
	default:
		return false, errors.Errorf("unsupported database provider: %s", dbProvider)
	}
}

func clearLegacyMigrationDirtyInternal(ctx context.Context, db *sql.DB, dbProvider string, version int64) error {
	queryFormat := "UPDATE " + legacyVersionTable + " SET dirty = false WHERE version = %s"
	query, args, err := sqlWithProviderPlaceholderInternal(dbProvider, queryFormat, version)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return errors.WrapIff(err, "failed to clear legacy dirty migration state for %s version %d", dbProvider, version)
	}
	return nil
}

func gooseVersionTableHasAppliedMigrationsInternal(ctx context.Context, db *sql.DB, dbProvider string) (bool, error) {
	exists, err := gooseVersionTableExistsInternal(ctx, db, dbProvider)
	if err != nil || !exists {
		return false, err
	}

	var version int64
	if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COALESCE(MAX(version_id), 0) FROM %s WHERE is_applied = %s", gooseVersionTable, appliedLiteralInternal(dbProvider))).Scan(&version); err != nil {
		return false, errors.WrapIff(err, "failed to read Goose migration state for %s", dbProvider)
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
			return false, errors.WrapIf(err, "failed to check Goose version table for sqlite")
		}
		return count > 0, nil
	case dbProviderPostgres:
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, gooseVersionTable).Scan(&exists); err != nil {
			return false, errors.WrapIf(err, "failed to check Goose version table for postgres")
		}
		return exists, nil
	default:
		return false, errors.Errorf("unsupported database provider: %s", dbProvider)
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
		return errors.Errorf("unsupported database provider: %s", dbProvider)
	}

	if _, err := execer.ExecContext(ctx, query); err != nil {
		return errors.WrapIff(err, "failed to create Goose version table for %s", dbProvider)
	}
	return nil
}

func clearGooseVersionTableInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string) error {
	if _, err := execer.ExecContext(ctx, "DELETE FROM "+gooseVersionTable); err != nil {
		return errors.WrapIff(err, "failed to clear Goose version table for %s", dbProvider)
	}
	return nil
}

func insertGooseMigrationVersionInternal(ctx context.Context, execer sqlExecerInternal, dbProvider string, version int64) error {
	switch dbProvider {
	case dbProviderSQLite:
		if _, err := execer.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (version_id, is_applied) VALUES (?, ?)", gooseVersionTable), version, true); err != nil {
			return errors.WrapIff(err, "failed to insert Goose migration version %d for sqlite", version)
		}
	case dbProviderPostgres:
		if _, err := execer.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (version_id, is_applied) VALUES ($1, $2)", gooseVersionTable), version, true); err != nil {
			return errors.WrapIff(err, "failed to insert Goose migration version %d for postgres", version)
		}
	default:
		return errors.Errorf("unsupported database provider: %s", dbProvider)
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
		return "", nil, errors.Errorf("unsupported database provider: %s", dbProvider)
	}
}

func appliedLiteralInternal(dbProvider string) string {
	if dbProvider == dbProviderPostgres {
		return "true"
	}
	return "1"
}

func getHighestEmbeddedMigrationVersionInternal(dbProvider string) (int64, error) {
	versions, err := getEmbeddedMigrationVersionsInternal(dbProvider)
	if err != nil {
		return 0, err
	}
	if len(versions) == 0 {
		return 0, errors.Errorf("no embedded migrations found for %s", dbProvider)
	}

	return versions[len(versions)-1], nil
}

func getEmbeddedMigrationVersionsInternal(dbProvider string) ([]int64, error) {
	entries, err := resources.FS.ReadDir("migrations/" + dbProvider)
	if err != nil {
		return nil, errors.WrapIff(err, "failed to read embedded migrations for %s", dbProvider)
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
		" WHERE is_applied = " + appliedLiteralInternal(dbProvider) +
		" AND version_id > %s ORDER BY version_id"
	query, args, err := sqlWithProviderPlaceholderInternal(dbProvider, queryFormat, requiredVersion)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.WrapIff(err, "failed to read applied migration versions for %s", dbProvider)
	}
	defer func() {
		_ = rows.Close()
	}()

	var missing []int64
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return nil, errors.WrapIff(err, "failed to scan applied migration version for %s", dbProvider)
		}
		if _, ok := embeddedSet[version]; !ok {
			missing = append(missing, version)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errors.WrapIff(err, "failed to iterate applied migration versions for %s", dbProvider)
	}

	return missing, nil
}

func ParseSQLiteConnectionString(connString string) (string, error) {
	if !strings.HasPrefix(connString, "file:") {
		connString = "file:" + connString
	}

	connStringUrl, err := url.Parse(connString)
	if err != nil {
		return "", errors.WrapIf(err, "failed to parse SQLite connection string")
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
		default:
			qs[k] = v
		}
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
	pathPart, err := utils.SQLitePathFromDSN(connString)
	if err != nil {
		return errors.WrapIf(err, "failed to parse SQLite DSN")
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
