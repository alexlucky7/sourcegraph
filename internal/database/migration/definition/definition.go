package definition

import (
	"fmt"

	"github.com/keegancsmith/sqlf"
)

type Definition struct {
	ID           int
	UpFilename   string
	UpQuery      *sqlf.Query
	DownFilename string
	DownQuery    *sqlf.Query
	Metadata     Metadata
}

type Metadata struct {
	Parents []int
}

type Definitions struct {
	definitions    []Definition
	definitionsMap map[int]Definition
}

func newDefinitions(migrationDefinitions []Definition) *Definitions {
	definitionsMap := make(map[int]Definition, len(migrationDefinitions))
	for _, migrationDefinition := range migrationDefinitions {
		definitionsMap[migrationDefinition.ID] = migrationDefinition
	}

	return &Definitions{
		definitions:    migrationDefinitions,
		definitionsMap: definitionsMap,
	}
}

func (ds *Definitions) All() []Definition {
	return ds.definitions
}

func (ds *Definitions) GetByID(id int) (Definition, bool) {
	definition, ok := ds.definitionsMap[id]
	return definition, ok
}

func (ds *Definitions) Root() Definition {
	return ds.definitions[0]
}

func (ds *Definitions) Leaves() []Definition {
	childrenMap := children(ds.definitions)

	leaves := make([]Definition, 0, 4)
	for _, definition := range ds.definitions {
		if len(childrenMap[definition.ID]) == 0 {
			leaves = append(leaves, definition)
		}
	}

	return leaves
}

// TODO - test this
func (ds *Definitions) Filter(ids []int) (*Definitions, error) {
	idMap := map[int]struct{}{}
	for _, id := range ids {
		idMap[id] = struct{}{}
	}

	filtered := make([]Definition, 0, len(ds.definitions)-len(ids))
	for _, definition := range ds.definitions {
		if _, ok := idMap[definition.ID]; ok {
			filtered = append(filtered, definition)
		}
	}

	for _, definition := range filtered {
		for _, parent := range definition.Metadata.Parents {
			if _, ok := idMap[parent]; ok {
				return nil, fmt.Errorf("outstanding (in a bad way) refrences")
			}
		}
	}

	return newDefinitions(filtered), nil
}

// TODO - test this
func (ds *Definitions) LeafDominator() (Definition, bool) {
	// Dom(n_0) = { n_0 }
	// Dom(n) = { n } union (intersect dom(p) over { p | preds(n) })

	dominators := map[int][]int{}
	for _, definition := range ds.definitions {
		ds := []int{definition.ID}

		if len(definition.Metadata.Parents) != 0 {
			a := dominators[definition.Metadata.Parents[0]]
			bs := make([][]int, 0, len(definition.Metadata.Parents))
			for _, parent := range definition.Metadata.Parents[1:] {
				bs = append(bs, dominators[parent])
			}

			ds = append(ds, intersect(a, bs...)...)
		}

		dominators[definition.ID] = ds
	}

	leaves := ds.Leaves()
	if len(leaves) == 0 {
		return Definition{}, false
	}

	ids := make([][]int, 0, len(leaves))
	for _, leaf := range leaves {
		ids = append(ids, dominators[leaf.ID])
	}

	same := intersect(ids[0], ids[1:]...)
	if len(same) == 0 {
		return Definition{}, false
	}
	return ds.GetByID(same[len(same)-1])
}

func (ds *Definitions) Up(appliedIDs, targetIDs []int) ([]Definition, error) {
	// Gather the set of ancestors of the migrations with the target identifiers
	definitions, err := ds.traverse(targetIDs, func(definition Definition) []int {
		return definition.Metadata.Parents
	})
	if err != nil {
		return nil, err
	}

	appliedMap := make(map[int]struct{}, len(appliedIDs))
	for _, id := range appliedIDs {
		appliedMap[id] = struct{}{}
	}

	filtered := definitions[:0]
	for _, definition := range definitions {
		if _, ok := appliedMap[definition.ID]; ok {
			continue
		}

		// Exclude any already-applied definition, which are included in the
		// set returned by definitions. We maintain the topological order implicit
		// in the slice as we're returning migrations to be applied in sequence.
		filtered = append(filtered, definition)
	}

	return filtered, nil
}

func (ds *Definitions) Down(appliedIDs, targetIDs []int) ([]Definition, error) {
	// Gather the set of descendants of the migrations with the target identifiers
	childrenMap := children(ds.definitions)
	definitions, err := ds.traverse(targetIDs, func(definition Definition) []int {
		return childrenMap[definition.ID]
	})
	if err != nil {
		return nil, err
	}

	targetMap := make(map[int]struct{}, len(targetIDs))
	for _, id := range targetIDs {
		targetMap[id] = struct{}{}
	}
	appliedMap := make(map[int]struct{}, len(appliedIDs))
	for _, id := range appliedIDs {
		appliedMap[id] = struct{}{}
	}

	filtered := definitions[:0]
	for _, definition := range definitions {
		if _, ok := targetMap[definition.ID]; ok {
			continue
		}
		if _, ok := appliedMap[definition.ID]; !ok {
			continue
		}

		// Exclude the targets themselves as well as any non-applied definition. We
		// are returning the set of migrations to _undo_, which should not include
		// the target schema version.
		filtered = append(filtered, definition)
	}

	// Reverse the slice in-place. We want to undo them in the opposite order from
	// which they were applied.
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}

	return filtered, nil
}

// traverse returns an ordered slice of definitions that are reachable from the given
// target identifiers through the edges defined by the given next function. Any definition
// that is reachable in this traversal will be included in the resulting slice, which has
// the same topological ordering guarantees as the underlying `ds.definitions` slice.
func (ds *Definitions) traverse(targetIDs []int, next func(definition Definition) []int) ([]Definition, error) {
	type node struct {
		id     int
		parent *int
	}

	frontier := make([]node, 0, len(targetIDs))
	for _, id := range targetIDs {
		frontier = append(frontier, node{id: id})
	}

	visited := map[int]struct{}{}

	for len(frontier) > 0 {
		newFrontier := make([]node, 0, 4)
		for _, n := range frontier {
			if _, ok := visited[n.id]; ok {
				continue
			}
			visited[n.id] = struct{}{}

			definition, ok := ds.GetByID(n.id)
			if !ok {
				return nil, unknownMigrationError(n.id, n.parent)
			}

			for _, id := range next(definition) {
				newFrontier = append(newFrontier, node{id, &n.id})
			}
		}

		frontier = newFrontier
	}

	filtered := make([]Definition, 0, len(visited))
	for _, definition := range ds.definitions {
		if _, ok := visited[definition.ID]; !ok {
			continue
		}

		filtered = append(filtered, definition)
	}

	return filtered, nil
}

func unknownMigrationError(id int, parent *int) error {
	if parent == nil {
		return fmt.Errorf("unknown migration %d", id)
	}

	return fmt.Errorf("unknown migration %dreferenced from migration %d", id, *parent)
}

func intersect(a []int, bs ...[]int) []int {
	aCopy := make([]int, len(a))
	copy(aCopy, a)

	for _, b := range bs {
		bMap := make(map[int]struct{}, len(b))
		for _, v := range b {
			bMap[v] = struct{}{}
		}

		filtered := aCopy[:0]
		for _, v := range aCopy {
			if _, ok := bMap[v]; ok {
				filtered = append(filtered, v)
			}
		}

		aCopy = filtered
	}

	return aCopy
}
