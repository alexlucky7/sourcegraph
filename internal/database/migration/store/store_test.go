package store

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/keegancsmith/sqlf"

	"github.com/sourcegraph/sourcegraph/internal/database/basestore"
	"github.com/sourcegraph/sourcegraph/internal/database/dbtest"
	"github.com/sourcegraph/sourcegraph/internal/database/dbutil"
	"github.com/sourcegraph/sourcegraph/internal/database/migration/definition"
	"github.com/sourcegraph/sourcegraph/internal/observation"
)

func TestEnsureSchemaTable(t *testing.T) {
	db := dbtest.NewDB(t)
	store := testStore(db)
	ctx := context.Background()

	if err := store.Exec(ctx, sqlf.Sprintf("SELECT * FROM test_migrations_table")); err == nil {
		t.Fatalf("expected query to fail due to missing schema table")
	}

	if err := store.Exec(ctx, sqlf.Sprintf("SELECT * FROM migration_logs")); err == nil {
		t.Fatalf("expected query to fail due to missing logs table")
	}

	if err := store.EnsureSchemaTable(ctx); err != nil {
		t.Fatalf("unexpected error ensuring schema table exists: %s", err)
	}

	if err := store.Exec(ctx, sqlf.Sprintf("SELECT * FROM test_migrations_table")); err != nil {
		t.Fatalf("unexpected error querying version table: %s", err)
	}

	if err := store.Exec(ctx, sqlf.Sprintf("SELECT * FROM migration_logs")); err != nil {
		t.Fatalf("unexpected error querying logs table: %s", err)
	}

	if err := store.EnsureSchemaTable(ctx); err != nil {
		t.Fatalf("expected method to be idempotent, got error: %s", err)
	}
}

func TestVersions(t *testing.T) {
	db := dbtest.NewDB(t)
	store := testStore(db)
	ctx := context.Background()

	if err := store.EnsureSchemaTable(ctx); err != nil {
		t.Fatalf("unexpected error ensuring schema table exists: %s", err)
	}

	t.Run("empty", func(*testing.T) {
		if appliedVersions, pendingVersions, failedVersions, err := store.Versions(ctx); err != nil {
			t.Fatalf("unexpected error querying versions: %s", err)
		} else if len(appliedVersions)+len(pendingVersions)+len(failedVersions) > 0 {
			t.Fatalf("unexpected no versions, got applied=%v pending=%v failed=%v", appliedVersions, pendingVersions, failedVersions)
		}
	})

	type testCase struct {
		version      int
		up           bool
		success      *bool
		errorMessage *string
	}
	makeCase := func(version int, up bool, failed *bool) testCase {
		if failed == nil {
			return testCase{version, up, nil, nil}
		}
		if *failed {
			return testCase{version, up, boolPtr(false), strPtr("uh-oh")}
		}
		return testCase{version, up, boolPtr(true), nil}
	}

	for _, migrationLog := range []testCase{
		// Historic attempts
		makeCase(1003, true, boolPtr(true)), makeCase(1003, false, boolPtr(true)), // 1003: successful up, successful down
		makeCase(1004, true, boolPtr(true)),                                       // 1004: successful up
		makeCase(1006, true, boolPtr(false)), makeCase(1006, true, boolPtr(true)), // 1006: failed up, successful up

		// Last attempts
		makeCase(1001, true, boolPtr(false)),  // successful up
		makeCase(1002, false, boolPtr(false)), // successful down
		makeCase(1003, true, nil),             // pending up
		makeCase(1004, false, nil),            // pending down
		makeCase(1005, true, boolPtr(true)),   // failed up
		makeCase(1006, false, boolPtr(true)),  // failed down
	} {
		if err := store.Exec(ctx, sqlf.Sprintf(`INSERT INTO migration_logs (
				migration_logs_schema_version,
				schema,
				version,
				up,
				started_at,
				success,
				finished_at,
				error_message
			) VALUES (%s, %s, %s, %s, NOW(), %s, NOW(), %s)`,
			currentMigrationLogSchemaVersion,
			"test_migrations_table",
			migrationLog.version,
			migrationLog.up,
			migrationLog.success,
			migrationLog.errorMessage,
		)); err != nil {
			t.Fatalf("unexpected error inserting data: %s", err)
		}
	}

	assertVersions(
		t,
		ctx,
		store,
		[]int{1001},       // expectedAppliedVersions
		[]int{1003, 1004}, // expectedPendingVersions
		[]int{1005, 1006}, // expectedFailedVersions
	)
}

func TestLock(t *testing.T) {
	db := dbtest.NewDB(t)
	store := testStore(db)
	ctx := context.Background()

	t.Run("sanity test", func(t *testing.T) {
		acquired, close, err := store.Lock(ctx)
		if err != nil {
			t.Fatalf("unexpected error acquiring lock: %s", err)
		}
		if !acquired {
			t.Fatalf("expected lock to be acquired")
		}

		if err := close(nil); err != nil {
			t.Fatalf("unexpected error releasing lock: %s", err)
		}
	})
}

func TestTryLock(t *testing.T) {
	db := dbtest.NewDB(t)
	store := testStore(db)
	ctx := context.Background()

	t.Run("sanity test", func(t *testing.T) {
		acquired, close, err := store.TryLock(ctx)
		if err != nil {
			t.Fatalf("unexpected error acquiring lock: %s", err)
		}
		if !acquired {
			t.Fatalf("expected lock to be acquired")
		}

		if err := close(nil); err != nil {
			t.Fatalf("unexpected error releasing lock: %s", err)
		}
	})
}

func TestUp(t *testing.T) {
	db := dbtest.NewDB(t)
	store := testStore(db)
	ctx := context.Background()

	if err := store.EnsureSchemaTable(ctx); err != nil {
		t.Fatalf("unexpected error ensuring schema table exists: %s", err)
	}
	if err := store.Exec(ctx, sqlf.Sprintf(`INSERT INTO test_migrations_table VALUES (15, false)`)); err != nil {
		t.Fatalf("unexpected error setting initial version: %s", err)
	}

	// Seed a few migrations
	for _, id := range []int{13, 14, 15} {
		if err := store.Up(ctx, definition.Definition{
			ID:      id,
			UpQuery: sqlf.Sprintf(`-- No-op`),
		}); err != nil {
			t.Fatalf("unexpected error running migration: %s", err)
		}
	}

	logs := []migrationLog{
		{
			Schema:  "test_migrations_table",
			Version: 13,
			Up:      true,
			Success: boolPtr(true),
		},
		{
			Schema:  "test_migrations_table",
			Version: 14,
			Up:      true,
			Success: boolPtr(true),
		}, {
			Schema:  "test_migrations_table",
			Version: 15,
			Up:      true,
			Success: boolPtr(true),
		},
	}

	t.Run("success", func(t *testing.T) {
		if err := store.Up(ctx, definition.Definition{
			ID: 16,
			UpQuery: sqlf.Sprintf(`
				CREATE TABLE test_trees (
					name text,
					leaf_type text,
					seed_type text,
					bark_type text
				);

				INSERT INTO test_trees VALUES
					('oak', 'broad', 'regular', 'strong'),
					('birch', 'narrow', 'regular', 'flaky'),
					('pine', 'needle', 'pine cone', 'soft');
			`),
		}); err != nil {
			t.Fatalf("unexpected error running migration: %s", err)
		}

		if barkType, _, err := basestore.ScanFirstString(store.Query(ctx, sqlf.Sprintf(`SELECT bark_type FROM test_trees WHERE name = 'birch'`))); err != nil {
			t.Fatalf("migration query did not succeed; unexpected error querying test table: %s", err)
		} else if barkType != "flaky" {
			t.Fatalf("migration query did not succeed; unexpected bark type. want=%s have=%s", "flaky", barkType)
		}

		logs = append(logs, migrationLog{
			Schema:  "test_migrations_table",
			Version: 16,
			Up:      true,
			Success: boolPtr(true),
		})
		assertLogs(t, ctx, store, logs)
		assertVersions(t, ctx, store, []int{13, 14, 15, 16}, nil, nil)
	})

	t.Run("unexpected state", func(t *testing.T) {
		expectedErrorMessage := "already applied"

		if err := store.Up(ctx, definition.Definition{
			ID: 15,
			UpQuery: sqlf.Sprintf(`
				-- Does not actually run
			`),
		}); err == nil || !strings.Contains(err.Error(), expectedErrorMessage) {
			t.Fatalf("unexpected error want=%q have=%q", expectedErrorMessage, err)
		}

		// no change
		assertLogs(t, ctx, store, logs)
		assertVersions(t, ctx, store, []int{13, 14, 15, 16}, nil, nil)
	})

	t.Run("query failure", func(t *testing.T) {
		expectedErrorMessage := "SQL Error"

		if err := store.Up(ctx, definition.Definition{
			ID: 17,
			UpQuery: sqlf.Sprintf(`
				-- Note: table already exists
				CREATE TABLE test_trees (
					name text,
					leaf_type text,
					seed_type text,
					bark_type text
				);
			`),
		}); err == nil || !strings.Contains(err.Error(), expectedErrorMessage) {
			t.Fatalf("unexpected error want=%q have=%q", expectedErrorMessage, err)
		}

		logs = append(logs, migrationLog{
			Schema:  "test_migrations_table",
			Version: 17,
			Up:      true,
			Success: boolPtr(false),
		})
		assertLogs(t, ctx, store, logs)
		assertVersions(t, ctx, store, []int{13, 14, 15, 16}, nil, []int{17})
	})

	t.Run("dirty", func(t *testing.T) {
		expectedErrorMessage := "dirty database"

		if err := store.Up(ctx, definition.Definition{
			ID: 17,
			UpQuery: sqlf.Sprintf(`
				-- Does not actually run
			`),
		}); err == nil || !strings.Contains(err.Error(), expectedErrorMessage) {
			t.Fatalf("unexpected error want=%q have=%q", expectedErrorMessage, err)
		}

		// no change
		assertLogs(t, ctx, store, logs)
		assertVersions(t, ctx, store, []int{13, 14, 15, 16}, nil, []int{17})
	})
}

func TestDown(t *testing.T) {
	db := dbtest.NewDB(t)
	store := testStore(db)
	ctx := context.Background()

	if err := store.EnsureSchemaTable(ctx); err != nil {
		t.Fatalf("unexpected error ensuring schema table exists: %s", err)
	}
	if err := store.Exec(ctx, sqlf.Sprintf(`INSERT INTO test_migrations_table VALUES (14, false)`)); err != nil {
		t.Fatalf("unexpected error setting initial version: %s", err)
	}
	if err := store.Exec(ctx, sqlf.Sprintf(`
		CREATE TABLE test_trees (
			name text,
			leaf_type text,
			seed_type text,
			bark_type text
		);
	`)); err != nil {
		t.Fatalf("unexpected error creating test table: %s", err)
	}

	testQuery := sqlf.Sprintf(`
		INSERT INTO test_trees VALUES
			('oak', 'broad', 'regular', 'strong'),
			('birch', 'narrow', 'regular', 'flaky'),
			('pine', 'needle', 'pine cone', 'soft');
	`)

	// run twice to ensure the error post-migration is not due to an index constraint
	if err := store.Exec(ctx, testQuery); err != nil {
		t.Fatalf("unexpected error inserting into test table: %s", err)
	}
	if err := store.Exec(ctx, testQuery); err != nil {
		t.Fatalf("unexpected error inserting into test table: %s", err)
	}

	// Seed a few migrations
	for _, id := range []int{12, 13, 14} {
		if err := store.Up(ctx, definition.Definition{
			ID:      id,
			UpQuery: sqlf.Sprintf(`-- No-op`),
		}); err != nil {
			t.Fatalf("unexpected error running migration: %s", err)
		}
	}

	logs := []migrationLog{
		{
			Schema:  "test_migrations_table",
			Version: 12,
			Up:      true,
			Success: boolPtr(true),
		},
		{
			Schema:  "test_migrations_table",
			Version: 13,
			Up:      true,
			Success: boolPtr(true),
		},
		{
			Schema:  "test_migrations_table",
			Version: 14,
			Up:      true,
			Success: boolPtr(true),
		},
	}

	t.Run("success", func(t *testing.T) {
		if err := store.Down(ctx, definition.Definition{
			ID: 14,
			DownQuery: sqlf.Sprintf(`
				DROP TABLE test_trees;
			`),
		}); err != nil {
			t.Fatalf("unexpected error running migration: %s", err)
		}

		// note: this query succeeded twice earlier
		if err := store.Exec(ctx, testQuery); err == nil || !strings.Contains(err.Error(), "SQL Error") {
			t.Fatalf("migration query did not succeed; expected missing table. want=%q have=%q", "SQL Error", err)
		}

		logs = append(logs, migrationLog{
			Schema:  "test_migrations_table",
			Version: 14,
			Up:      false,
			Success: boolPtr(true),
		})
		assertLogs(t, ctx, store, logs)
		assertVersions(t, ctx, store, []int{12, 13}, nil, nil)
	})

	t.Run("unexpected state", func(t *testing.T) {
		expectedErrorMessage := "has not been applied; nothing to revert"

		if err := store.Down(ctx, definition.Definition{
			ID: 15,
			DownQuery: sqlf.Sprintf(`
				-- Does not actually run
			`),
		}); err == nil || !strings.Contains(err.Error(), expectedErrorMessage) {
			t.Fatalf("unexpected error want=%q have=%q", expectedErrorMessage, err)
		}

		// no change
		assertLogs(t, ctx, store, logs)
		assertVersions(t, ctx, store, []int{12, 13}, nil, nil)
	})

	t.Run("query failure", func(t *testing.T) {
		expectedErrorMessage := "SQL Error"

		if err := store.Down(ctx, definition.Definition{
			ID: 13,
			DownQuery: sqlf.Sprintf(`
				-- Note: table does not exist
				DROP TABLE TABLE test_trees;
			`),
		}); err == nil || !strings.Contains(err.Error(), expectedErrorMessage) {
			t.Fatalf("unexpected error want=%q have=%q", expectedErrorMessage, err)
		}

		logs = append(logs, migrationLog{
			Schema:  "test_migrations_table",
			Version: 13,
			Up:      false,
			Success: boolPtr(false),
		})
		assertLogs(t, ctx, store, logs)
		assertVersions(t, ctx, store, []int{12}, nil, []int{13})
	})

	t.Run("dirty", func(t *testing.T) {
		expectedErrorMessage := "dirty database"

		if err := store.Down(ctx, definition.Definition{
			ID: 12,
			DownQuery: sqlf.Sprintf(`
				-- Does not actually run
			`),
		}); err == nil || !strings.Contains(err.Error(), expectedErrorMessage) {
			t.Fatalf("unexpected error want=%q have=%q", expectedErrorMessage, err)
		}

		// no change
		assertLogs(t, ctx, store, logs)
		assertVersions(t, ctx, store, []int{12}, nil, []int{13})
	})
}

func testStore(db dbutil.DB) *Store {
	return NewWithDB(db, "test_migrations_table", NewOperations(&observation.TestContext))
}

func strPtr(v string) *string {
	return &v
}

func boolPtr(value bool) *bool {
	return &value
}

func assertLogs(t *testing.T, ctx context.Context, store *Store, expectedLogs []migrationLog) {
	t.Helper()

	logs, err := scanMigrationLogs(store.Query(ctx, sqlf.Sprintf(`SELECT schema, version, up, success FROM migration_logs ORDER BY started_at`)))
	if err != nil {
		t.Fatalf("unexpected error scanning logs: %s", err)
	}

	if diff := cmp.Diff(expectedLogs, logs); diff != "" {
		t.Errorf("unexpected migration logs (-want +got):\n%s", diff)
	}
}

func assertVersions(t *testing.T, ctx context.Context, store *Store, expectedAppliedVersions, expectedPendingVersions, expectedFailedVersions []int) {
	t.Helper()

	appliedVersions, pendingVersions, failedVersions, err := store.Versions(ctx)
	if err != nil {
		t.Fatalf("unexpected error querying version: %s", err)
	}

	if diff := cmp.Diff(expectedAppliedVersions, appliedVersions); diff != "" {
		t.Errorf("unexpected applied migration logs (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(expectedPendingVersions, pendingVersions); diff != "" {
		t.Errorf("unexpected pending migration logs (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(expectedFailedVersions, failedVersions); diff != "" {
		t.Errorf("unexpected failed migration logs (-want +got):\n%s", diff)
	}
}
