package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/hashicorp/go-multierror"
	"github.com/keegancsmith/sqlf"
	"github.com/opentracing/opentracing-go/log"

	"github.com/sourcegraph/sourcegraph/internal/database/basestore"
	"github.com/sourcegraph/sourcegraph/internal/database/dbutil"
	"github.com/sourcegraph/sourcegraph/internal/database/locker"
	"github.com/sourcegraph/sourcegraph/internal/database/migration/definition"
	"github.com/sourcegraph/sourcegraph/internal/observation"
)

type Store struct {
	*basestore.Store
	schemaName string
	operations *Operations
}

func NewWithDB(db dbutil.DB, migrationsTable string, operations *Operations) *Store {
	return &Store{
		Store:      basestore.NewWithDB(db, sql.TxOptions{}),
		schemaName: migrationsTable,
		operations: operations,
	}
}

func (s *Store) With(other basestore.ShareableStore) *Store {
	return &Store{
		Store:      s.Store.With(other),
		schemaName: s.schemaName,
		operations: s.operations,
	}
}

func (s *Store) Transact(ctx context.Context) (*Store, error) {
	txBase, err := s.Store.Transact(ctx)
	if err != nil {
		return nil, err
	}

	return &Store{
		Store:      txBase,
		schemaName: s.schemaName,
		operations: s.operations,
	}, nil
}

const currentMigrationLogSchemaVersion = 1

func (s *Store) EnsureSchemaTable(ctx context.Context) (err error) {
	ctx, endObservation := s.operations.ensureSchemaTable.With(ctx, &err, observation.Args{})
	defer endObservation(1, observation.Args{})

	queries := []*sqlf.Query{
		sqlf.Sprintf(`CREATE TABLE IF NOT EXISTS %s(version bigint NOT NULL PRIMARY KEY)`, quote(s.schemaName)),
		sqlf.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS dirty boolean NOT NULL`, quote(s.schemaName)),

		sqlf.Sprintf(`CREATE TABLE IF NOT EXISTS migration_logs(id SERIAL PRIMARY KEY)`),
		sqlf.Sprintf(`ALTER TABLE migration_logs ADD COLUMN IF NOT EXISTS migration_logs_schema_version integer NOT NULL`),
		sqlf.Sprintf(`ALTER TABLE migration_logs ADD COLUMN IF NOT EXISTS schema text NOT NULL`),
		sqlf.Sprintf(`ALTER TABLE migration_logs ADD COLUMN IF NOT EXISTS version integer NOT NULL`),
		sqlf.Sprintf(`ALTER TABLE migration_logs ADD COLUMN IF NOT EXISTS up bool NOT NULL`),
		sqlf.Sprintf(`ALTER TABLE migration_logs ADD COLUMN IF NOT EXISTS started_at timestamptz NOT NULL`),
		sqlf.Sprintf(`ALTER TABLE migration_logs ADD COLUMN IF NOT EXISTS finished_at timestamptz`),
		sqlf.Sprintf(`ALTER TABLE migration_logs ADD COLUMN IF NOT EXISTS success boolean`),
		sqlf.Sprintf(`ALTER TABLE migration_logs ADD COLUMN IF NOT EXISTS error_message text`),
	}

	minMigrationVersions := map[string]int{
		"schema_migrations":              1528395834,
		"codeintel_schema_migrations":    1000000015,
		"codeinsights_schema_migrations": 1000000000,
	}
	if minMigrationVersion, ok := minMigrationVersions[s.schemaName]; ok {
		queries = append(queries, sqlf.Sprintf(`
			WITH schema_version AS (
				SELECT * FROM %s LIMIT 1
			)
			INSERT INTO migration_logs (
				migration_logs_schema_version,
				schema,
				version,
				up,
				success,
				started_at,
				finished_at
			)
			SELECT %s, %s, version, true, true, NOW(), NOW()
			FROM generate_series(%s, (SELECT version FROM schema_version)) version
			WHERE NOT (SELECT dirty FROM schema_version) AND NOT EXISTS (SELECT 1 FROM migration_logs WHERE schema = %s)
		`,
			quote(s.schemaName),
			currentMigrationLogSchemaVersion,
			s.schemaName,
			minMigrationVersion,
			s.schemaName,
		))
	}

	tx, err := s.Transact(ctx)
	if err != nil {
		return err
	}
	defer func() { err = tx.Done(err) }()

	for _, query := range queries {
		if err := tx.Exec(ctx, query); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) Versions(ctx context.Context) (appliedVersions, pendingVersions, failedVersions []int, err error) {
	ctx, endObservation := s.operations.versions.With(ctx, &err, observation.Args{})
	defer endObservation(1, observation.Args{})

	migrationLogs, err := scanMigrationLogs(s.Query(ctx, sqlf.Sprintf(versionsQuery, s.schemaName)))
	if err != nil {
		return nil, nil, nil, err
	}

	for _, migrationLog := range migrationLogs {
		if migrationLog.Success == nil {
			pendingVersions = append(pendingVersions, migrationLog.Version)
			continue
		}
		if !*migrationLog.Success {
			failedVersions = append(failedVersions, migrationLog.Version)
			continue
		}
		if migrationLog.Up {
			appliedVersions = append(appliedVersions, migrationLog.Version)
		}
	}

	return appliedVersions, pendingVersions, failedVersions, nil
}

const versionsQuery = `
-- source: internal/database/migration/store/store.go:Versions
WITH ranked_migration_logs AS (
	SELECT
		migration_logs.*,
		ROW_NUMBER() OVER (PARTITION BY version ORDER BY finished_at DESC) AS row_number
	FROM migration_logs
	WHERE schema = %s
)
SELECT
	schema,
	version,
	up,
	success
FROM ranked_migration_logs
WHERE row_number = 1
ORDER BY version
`

// Lock creates and holds an advisory lock. This method returns a function that should be called
// once the lock should be released. This method accepts the current function's error output and
// wraps any additional errors that occur on close.
//
// Note that we don't use the internal/database/locker package here as that uses transactionally
// scoped advisory locks. We want to be able to hold locks outside of transactions for migrations.
func (s *Store) Lock(ctx context.Context) (_ bool, _ func(err error) error, err error) {
	key := s.lockKey()

	ctx, endObservation := s.operations.lock.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int32("key", key),
	}})
	defer endObservation(1, observation.Args{})

	if err := s.Exec(ctx, sqlf.Sprintf(`SELECT pg_advisory_lock(%s, %s)`, key, 0)); err != nil {
		return false, nil, err
	}

	close := func(err error) error {
		if unlockErr := s.Exec(ctx, sqlf.Sprintf(`SELECT pg_advisory_unlock(%s, %s)`, key, 0)); unlockErr != nil {
			err = multierror.Append(err, unlockErr)
		}

		return err
	}

	return true, close, nil
}

// TryLock attempts to create hold an advisory lock. This method returns a function that should be
// called once the lock should be released. This method accepts the current function's error output
// and wraps any additional errors that occur on close. Calling this method when the lock was not
// acquired will return the given error without modification (no-op). If this method returns true,
// the lock was acquired and false if the lock is currently held by another process.
//
// Note that we don't use the internal/database/locker package here as that uses transactionally
// scoped advisory locks. We want to be able to hold locks outside of transactions for migrations.
func (s *Store) TryLock(ctx context.Context) (_ bool, _ func(err error) error, err error) {
	key := s.lockKey()

	ctx, endObservation := s.operations.tryLock.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int32("key", key),
	}})
	defer endObservation(1, observation.Args{})

	locked, _, err := basestore.ScanFirstBool(s.Query(ctx, sqlf.Sprintf(`SELECT pg_try_advisory_lock(%s, %s)`, key, 0)))
	if err != nil {
		return false, nil, err
	}

	close := func(err error) error {
		if locked {
			if unlockErr := s.Exec(ctx, sqlf.Sprintf(`SELECT pg_advisory_unlock(%s, %s)`, key, 0)); unlockErr != nil {
				err = multierror.Append(err, unlockErr)
			}
		}

		return err
	}

	return locked, close, nil
}

func (s *Store) lockKey() int32 {
	return locker.StringKey(fmt.Sprintf("%s:migrations", s.schemaName))
}

func (s *Store) Up(ctx context.Context, definition definition.Definition) (err error) {
	ctx, endObservation := s.operations.up.With(ctx, &err, observation.Args{})
	defer endObservation(1, observation.Args{})

	if err := s.runMigrationQuery(ctx, definition.ID, true, definition.UpQuery); err != nil {
		return err
	}

	return nil
}

func (s *Store) Down(ctx context.Context, definition definition.Definition) (err error) {
	ctx, endObservation := s.operations.down.With(ctx, &err, observation.Args{})
	defer endObservation(1, observation.Args{})

	if err := s.runMigrationQuery(ctx, definition.ID, false, definition.DownQuery); err != nil {
		return err
	}

	return nil
}

func (s *Store) runMigrationQuery(ctx context.Context, definitionVersion int, up bool, query *sqlf.Query) (err error) {
	logID, err := s.createMigrationLog(ctx, definitionVersion, up)
	if err != nil {
		return err
	}

	defer func() {
		if execErr := s.Exec(ctx, sqlf.Sprintf(
			`UPDATE migration_logs SET finished_at = NOW(), success = %s, error_message = %s WHERE id = %d`,
			err == nil,
			errMsgPtr(err),
			logID,
		)); execErr != nil {
			err = multierror.Append(err, execErr)
		}
	}()

	if err := s.Exec(ctx, query); err != nil {
		return err
	}

	if err := s.Exec(ctx, sqlf.Sprintf(`UPDATE %s SET dirty=false`, quote(s.schemaName))); err != nil {
		return err
	}

	return nil
}

func (s *Store) createMigrationLog(ctx context.Context, definitionVersion int, up bool) (_ int, err error) {
	tx, err := s.Transact(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { err = tx.Done(err) }()

	if err := tx.validateVersion(ctx, definitionVersion, up); err != nil {
		return 0, err
	}

	if err := tx.Exec(ctx, sqlf.Sprintf(`DELETE FROM %s`, quote(s.schemaName))); err != nil {
		return 0, err
	}
	if err := tx.Exec(ctx, sqlf.Sprintf(`INSERT INTO %s (version, dirty) VALUES (%s, true)`, quote(s.schemaName), definitionVersion)); err != nil {
		return 0, err
	}

	id, _, err := basestore.ScanFirstInt(tx.Query(ctx, sqlf.Sprintf(
		`
			INSERT INTO migration_logs (
				migration_logs_schema_version,
				schema,
				version,
				up,
				started_at
			) VALUES (%s, %s, %s, %s, NOW())
			RETURNING id
		`,
		currentMigrationLogSchemaVersion,
		s.schemaName,
		definitionVersion,
		up,
	)))
	if err != nil {
		return 0, err
	}

	return id, nil
}

func (s *Store) validateVersion(ctx context.Context, definitionVersion int, up bool) (err error) {
	appliedVersions, pendingVersions, failedVersions, err := s.Versions(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			cta := "This condition should not be reachable by normal use of the migration store via the runner and indicates a bug. Please report this issue."
			err = errors.Errorf("%s\n\n%s\n", err, cta)
		}
	}()

	if len(pendingVersions)+len(failedVersions) > 0 {
		return errors.Errorf("dirty database; pending=%v failed=%v", pendingVersions, failedVersions)
	}

	found := false
	for _, version := range appliedVersions {
		if version == definitionVersion {
			found = true
			break
		}
	}

	if up && found {
		return errors.Errorf("migration %d is already applied", definitionVersion)
	} else if !up && !found {
		return errors.Errorf("migration %d has not been applied; nothing to revert", definitionVersion)
	}

	return nil
}

var quote = sqlf.Sprintf

func errMsgPtr(err error) *string {
	if err == nil {
		return nil
	}

	text := err.Error()
	return &text
}

type migrationLog struct {
	Schema  string
	Version int
	Up      bool
	Success *bool
}

// scanMigrationLogs scans a slice of migration logs from the return value of `*Store.query`.
func scanMigrationLogs(rows *sql.Rows, queryErr error) (_ []migrationLog, err error) {
	if queryErr != nil {
		return nil, queryErr
	}
	defer func() { err = basestore.CloseRows(rows, err) }()

	var logs []migrationLog
	for rows.Next() {
		var log migrationLog

		if err := rows.Scan(
			&log.Schema,
			&log.Version,
			&log.Up,
			&log.Success,
		); err != nil {
			return nil, err
		}

		logs = append(logs, log)
	}

	return logs, nil
}
