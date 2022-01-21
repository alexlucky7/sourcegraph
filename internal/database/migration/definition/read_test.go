package definition

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/keegancsmith/sqlf"

	"github.com/sourcegraph/sourcegraph/internal/database/migration/definition/testdata"
)

func TestReadDefinitions(t *testing.T) {
	t.Run("well-formed", func(t *testing.T) {
		fs, err := fs.Sub(testdata.Content, "well-formed")
		if err != nil {
			t.Fatalf("unexpected error fetching schema %q: %s", "well-formed", err)
		}

		definitions, err := ReadDefinitions(fs)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		type comparableDefinition struct {
			ID           int
			UpFilename   string
			UpQuery      string
			DownFilename string
			DownQuery    string
		}

		comparableDefinitions := make([]comparableDefinition, 0, len(definitions.definitions))
		for _, definition := range definitions.definitions {
			comparableDefinitions = append(comparableDefinitions, comparableDefinition{
				ID:           definition.ID,
				UpFilename:   definition.UpFilename,
				UpQuery:      strings.TrimSpace(definition.UpQuery.Query(sqlf.PostgresBindVar)),
				DownFilename: definition.DownFilename,
				DownQuery:    strings.TrimSpace(definition.DownQuery.Query(sqlf.PostgresBindVar)),
			})
		}

		expectedDefinitions := []comparableDefinition{
			{ID: 10001, UpFilename: "10001/up.sql", DownFilename: "10001/down.sql", UpQuery: "10001 UP", DownQuery: "10001 DOWN"}, // first
			{ID: 10002, UpFilename: "10002/up.sql", DownFilename: "10002/down.sql", UpQuery: "10002 UP", DownQuery: "10002 DOWN"}, // second
			{ID: 10004, UpFilename: "10004/up.sql", DownFilename: "10004/down.sql", UpQuery: "10004 UP", DownQuery: "10004 DOWN"}, // third or fourth (2)
			{ID: 10003, UpFilename: "10003/up.sql", DownFilename: "10003/down.sql", UpQuery: "10003 UP", DownQuery: "10003 DOWN"}, // third or fourth (1)
			{ID: 10005, UpFilename: "10005/up.sql", DownFilename: "10005/down.sql", UpQuery: "10005 UP", DownQuery: "10005 DOWN"}, // fifth
		}
		if diff := cmp.Diff(expectedDefinitions, comparableDefinitions); diff != "" {
			t.Fatalf("unexpected definitions (-want +got):\n%s", diff)
		}
	})

	t.Run("missing upgrade query", func(t *testing.T) { testReadDefinitionsError(t, "missing-upgrade-query", "malformed") })
	t.Run("missing downgrade query", func(t *testing.T) { testReadDefinitionsError(t, "missing-downgrade-query", "malformed") })
	t.Run("missing metadata", func(t *testing.T) { testReadDefinitionsError(t, "missing-metadata", "malformed") })
	t.Run("no roots", func(t *testing.T) { testReadDefinitionsError(t, "no-roots", "no roots") })
	t.Run("multiple roots", func(t *testing.T) { testReadDefinitionsError(t, "multiple-roots", "multiple roots") })
	t.Run("cycle (connected to root)", func(t *testing.T) { testReadDefinitionsError(t, "cycle-traversal", "cycle") })
	t.Run("cycle (disconnected from root)", func(t *testing.T) { testReadDefinitionsError(t, "cycle-size", "cycle") })
}

func testReadDefinitionsError(t *testing.T, name, expectedError string) {
	t.Helper()

	fs, err := fs.Sub(testdata.Content, name)
	if err != nil {
		t.Fatalf("unexpected error fetching schema %q: %s", name, err)
	}

	if _, err := ReadDefinitions(fs); err == nil || !strings.Contains(err.Error(), expectedError) {
		t.Fatalf("unexpected error. want=%q got=%q", expectedError, err)
	}
}
