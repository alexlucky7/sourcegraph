package migration

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sourcegraph/sourcegraph/dev/sg/internal/db"
	"github.com/sourcegraph/sourcegraph/dev/sg/root"
	"github.com/sourcegraph/sourcegraph/internal/database/migration/definition"
)

const upMigrationFileTemplate = `BEGIN;

-- Perform migration here.
--
-- See /migrations/README.md. Highlights:
--  * Make migrations idempotent (use IF EXISTS)
--  * Make migrations backwards-compatible (old readers/writers must continue to work)
--  * Wrap your changes in a transaction. Note that CREATE INDEX CONCURRENTLY is an exception
--    and cannot be performed in a transaction. For such migrations, ensure that only one
--    statement is defined per migration to prevent query transactions from starting implicitly.

COMMIT;
`

const downMigrationFileTemplate = `BEGIN;

-- Undo the changes made in the up migration

COMMIT;
`

const metadataTemplate = `
name: %s
parents: [%s]
`

// RunAdd creates a new up/down migration file pair for the given database and
// returns the names of the new files. If there was an error, the filesystem should remain
// unmodified.
func RunAdd(database db.Database, migrationName string) (up, down, metadata string, _ error) {
	fs, err := database.FS()
	if err != nil {
		return "", "", "", err
	}

	definitions, err := definition.ReadDefinitions(fs)
	if err != nil {
		return "", "", "", err
	}

	leaves := definitions.Leaves()
	parents := make([]int, 0, len(leaves))
	for _, leaf := range leaves {
		parents = append(parents, leaf.ID)
	}

	id := int(time.Now().UTC().Unix())

	upPath, downPath, metadataPath, err := MakeMigrationFilenames(database, id)
	if err != nil {
		return "", "", "", err
	}

	contents := map[string]string{
		upPath:       upMigrationFileTemplate,
		downPath:     downMigrationFileTemplate,
		metadataPath: fmt.Sprintf(metadataTemplate, migrationName, strings.Join(intsToStrings(parents), ", ")),
	}

	if err := WriteMigrationFiles(contents); err != nil {
		return "", "", "", err
	}

	return upPath, downPath, metadataPath, nil
}

// MigrationDirectoryForDatabase returns the directory where migration files are stored for the
// given database.
func MigrationDirectoryForDatabase(database db.Database) (string, error) {
	repoRoot, err := root.RepositoryRoot()
	if err != nil {
		return "", err
	}

	return filepath.Join(repoRoot, "migrations", database.Name), nil
}

// MakeMigrationFilenames makes a pair of (absolute) paths to migration files with the
// given migration index.
func MakeMigrationFilenames(database db.Database, migrationIndex int) (up, down, metadata string, _ error) {
	baseDir, err := MigrationDirectoryForDatabase(database)
	if err != nil {
		return "", "", "", err
	}

	upPath := filepath.Join(baseDir, fmt.Sprintf("%d/up.sql", migrationIndex))
	downPath := filepath.Join(baseDir, fmt.Sprintf("%d/down.sql", migrationIndex))
	metadataPath := filepath.Join(baseDir, fmt.Sprintf("%d/metadata.yaml", migrationIndex))
	return upPath, downPath, metadataPath, nil
}

// WriteMigrationFiles writes the contents of migrationFileTemplate to the given filepaths.
func WriteMigrationFiles(contents map[string]string) (err error) {
	defer func() {
		if err != nil {
			for path := range contents {
				// undo any changes to the fs on error
				_ = os.Remove(path)
			}
		}
	}()

	for path, contents := range contents {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}

		if err := os.WriteFile(path, []byte(contents), os.FileMode(0644)); err != nil {
			return err
		}
	}

	return nil
}

func intsToStrings(ints []int) []string {
	strs := make([]string, 0, len(ints))
	for _, value := range ints {
		strs = append(strs, strconv.Itoa(value))
	}

	return strs
}
