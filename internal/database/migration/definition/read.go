package definition

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/keegancsmith/sqlf"
	"gopkg.in/yaml.v2"
)

func ReadDefinitions(fs fs.FS) (*Definitions, error) {
	migrationDefinitions, err := readDefinitions(fs)
	if err != nil {
		return nil, err
	}

	if err := reorderDefinitions(migrationDefinitions); err != nil {
		return nil, err
	}

	return newDefinitions(migrationDefinitions), nil
}

func readDefinitions(fs fs.FS) ([]Definition, error) {
	root, err := http.FS(fs).Open("/")
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	migrations, err := root.Readdir(0)
	if err != nil {
		return nil, err
	}

	versions := make([]int, 0, len(migrations))
	for _, file := range migrations {
		if version, err := strconv.Atoi(file.Name()); err == nil {
			versions = append(versions, version)
		}
	}
	sort.Ints(versions)

	definitions := make([]Definition, 0, len(versions))
	for _, version := range versions {
		definition, err := readDefinition(fs, version)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, errors.Wrapf(err, "malformed migration definition %d", version)
			}

			return nil, err
		}

		definitions = append(definitions, definition)
	}

	return definitions, nil
}

func readDefinition(fs fs.FS, version int) (Definition, error) {
	upFilename := fmt.Sprintf("%d/up.sql", version)
	downFilename := fmt.Sprintf("%d/down.sql", version)
	metadataFilename := fmt.Sprintf("%d/metadata.yaml", version)

	upQuery, err := readQueryFromFile(fs, upFilename)
	if err != nil {
		return Definition{}, err
	}

	downQuery, err := readQueryFromFile(fs, downFilename)
	if err != nil {
		return Definition{}, err
	}

	metadata, err := readMetadataFromFile(fs, metadataFilename)
	if err != nil {
		return Definition{}, err
	}

	return Definition{
		ID:           version,
		UpFilename:   upFilename,
		UpQuery:      upQuery,
		DownFilename: downFilename,
		DownQuery:    downQuery,
		Metadata:     metadata,
	}, nil
}

// readQueryFromFile returns the query parsed from the given file.
func readQueryFromFile(fs fs.FS, filepath string) (*sqlf.Query, error) {
	file, err := fs.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	contents, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	// Stringify -> SQL-ify the contents of the file. We first replace any
	// SQL placeholder values with an escaped version so that the sqlf.Sprintf
	// call does not try to interpolate the text with variables we don't have.
	return sqlf.Sprintf(strings.ReplaceAll(string(contents), "%", "%%")), nil
}

// readMetadataFromFile returns the metadata parsed from the given file.
func readMetadataFromFile(fs fs.FS, filepath string) (_ Metadata, _ error) {
	file, err := fs.Open(filepath)
	if err != nil {
		return Metadata{}, err
	}
	defer file.Close()

	contents, err := io.ReadAll(file)
	if err != nil {
		return Metadata{}, err
	}

	var payload struct {
		Parent  int   `yaml:"parent"`
		Parents []int `yaml:"parents"`
	}
	if err := yaml.Unmarshal(contents, &payload); err != nil {
		return Metadata{}, err
	}

	parents := payload.Parents
	if payload.Parent != 0 {
		parents = append(parents, payload.Parent)
	}
	sort.Ints(parents)

	metadata := Metadata{
		Parents: parents,
	}

	return metadata, nil
}

// reorderDefinitions will re-order the given migration definitions in-place so that
// migrations occur before their dependents in the slice. An error is returned if the
// given migration definitions do not form a single-root directed acyclic graph.
func reorderDefinitions(migrationDefinitions []Definition) error {
	if len(migrationDefinitions) == 0 {
		return nil
	}

	// Stash migration definitions by identifier
	migrationDefinitionMap := make(map[int]Definition, len(migrationDefinitions))
	for _, migrationDefinition := range migrationDefinitions {
		migrationDefinitionMap[migrationDefinition.ID] = migrationDefinition
	}

	// Find topological order of migrations
	order, err := findDefinitionOrder(migrationDefinitions)
	if err != nil {
		return err
	}

	for i, id := range order {
		// Re-order migration definitions slice to be in topological order. The order
		// returned by findDefinitionOrder is reversed; we want parents _before_ their
		// dependencies, so we fill this slice in backwards.
		migrationDefinitions[len(migrationDefinitions)-1-i] = migrationDefinitionMap[id]
	}

	return nil
}

// findDefinitionOrder returns an order of migration definition identifiers such that
// migrations occur only after their dependencies (parents). This assumes that the set
// of definitions provided form a single-root directed acyclic graph and fails with an
// error if this is not the case.
func findDefinitionOrder(migrationDefinitions []Definition) ([]int, error) {
	root, err := root(migrationDefinitions)
	if err != nil {
		return nil, err
	}

	// Use depth-first-search to topologically sort the migration definition sets as a
	// graph. At this point we know we have a single root; this means that the given set
	// of definitions either (a) form a connected acyclic graph, or (b) form a disconnected
	// set of graphs containing at least one cycle (by construction). In either case, we'll
	// return an error indicating that a cycle exists and that the set of definitions are
	// not well-formed.
	//
	// See the following Wikipedia article for additional intuition and description of the
	// `marks` array to detect cycles.
	// https://en.wikipedia.org/wiki/Topological_sorting#Depth-first_search

	type MarkType uint
	const (
		MarkTypeUnvisited MarkType = iota
		MarkTypeVisiting
		MarkTypeVisited
	)

	var (
		order    = make([]int, 0, len(migrationDefinitions))
		marks    = make(map[int]MarkType, len(migrationDefinitions))
		children = children(migrationDefinitions)

		dfs func(id int, parents []int) error
	)

	dfs = func(id int, parents []int) error {
		if marks[id] == MarkTypeVisiting {
			// We're currently processing the descendants of this node, so
			// we have a paths in both directions between these two nodes.

			// Peel off the head of the parent list until we reach the target
			// node. This leaves us with a slice starting with the target node,
			// followed by the path back to itself. We'll use this instance of
			// a cycle in the error description.
			for len(parents) > 0 && parents[0] != id {
				parents = parents[1:]
			}
			if len(parents) == 0 || parents[0] != id {
				panic("unreachable")
			}
			cycle := append(parents, id)

			return instructionalError{
				class:       "migration dependency cycle",
				description: fmt.Sprintf("migrations %d and %d declare each other as dependencies", parents[len(parents)-1], id),
				instructions: strings.Join([]string{
					fmt.Sprintf("Break one of the links in the following cycle:\n%s", strings.Join(intsToStrings(cycle), " -> ")),
				}, " "),
			}
		}
		if marks[id] == MarkTypeVisited {
			// already visited
			return nil
		}

		marks[id] = MarkTypeVisiting
		defer func() { marks[id] = MarkTypeVisited }()

		for _, child := range children[id] {
			if err := dfs(child, append(append([]int(nil), parents...), id)); err != nil {
				return err
			}
		}

		// Add self _after_ adding all children recursively
		order = append(order, id)
		return nil
	}

	// Perform a depth-first traversal from the single root we found above
	if err := dfs(root, nil); err != nil {
		return nil, err
	}

	for len(order) < len(migrationDefinitions) {
		// We didn't visit every node, but we also do not have more than one
		// root. There necessarliy exists a cycle that we didn't enter in the
		// traversal from our root. Continue the traversal starting from each
		// unvisited node until we return a cycle.
		for _, migrationDefinition := range migrationDefinitions {
			if _, ok := marks[migrationDefinition.ID]; !ok {
				if err := dfs(migrationDefinition.ID, nil); err != nil {
					return nil, err
				}
			}
		}

		panic("unreachable")
	}

	return order, nil
}

// root returns the unique migration definition with no parent. An error is returned
// if there is not exactly one root.
func root(migrationDefinitions []Definition) (int, error) {
	roots := make([]int, 0, 1)
	for _, migrationDefinition := range migrationDefinitions {
		if len(migrationDefinition.Metadata.Parents) == 0 {
			roots = append(roots, migrationDefinition.ID)
		}
	}
	if len(roots) == 0 {
		return 0, instructionalError{
			class:       "no roots",
			description: "every migration declares a parent",
			instructions: strings.Join([]string{
				`There is no migration defined in this schema that does not declare a parent.`,
				`This indicates either a migration dependency cycle or a reference to a parent migration that no longer exists.`,
			}, " "),
		}
	}

	if len(roots) > 1 {
		strRoots := intsToStrings(roots)
		sort.Strings(strRoots)

		return 0, instructionalError{
			class:       "multiple roots",
			description: fmt.Sprintf("expected exactly one migration to have no parent but found %d", len(roots)),
			instructions: strings.Join([]string{
				`There are multiple migrations defined in this schema that do not declare a parent.`,
				`This indicates a new migration that did not correctly attach itself to an existing migration.`,
				`This may also indicate the presence of a duplicate squashed migration.`,
			}, " "),
		}
	}

	return roots[0], nil
}

// children constructs map from migration identifiers to the set of identifiers of all
// dependent migrations.
func children(migrationDefinitions []Definition) map[int][]int {
	children := make(map[int][]int, len(migrationDefinitions))
	for _, migrationDefinition := range migrationDefinitions {
		for _, parent := range migrationDefinition.Metadata.Parents {
			children[parent] = append(children[parent], migrationDefinition.ID)
		}
	}

	return children
}

func intsToStrings(ints []int) []string {
	strs := make([]string, 0, len(ints))
	for _, value := range ints {
		strs = append(strs, strconv.Itoa(value))
	}

	return strs
}
