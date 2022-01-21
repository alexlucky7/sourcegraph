package definition

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDefinitionGetByID(t *testing.T) {
	definitions := []Definition{
		{ID: 1, UpFilename: "1.up.sql"},
		{ID: 2, UpFilename: "2.up.sql", Metadata: Metadata{Parents: []int{1}}},
		{ID: 3, UpFilename: "3.up.sql", Metadata: Metadata{Parents: []int{2}}},
		{ID: 4, UpFilename: "4.up.sql", Metadata: Metadata{Parents: []int{3}}},
		{ID: 5, UpFilename: "5.up.sql", Metadata: Metadata{Parents: []int{4}}},
	}

	definition, ok := newDefinitions(definitions).GetByID(3)
	if !ok {
		t.Fatalf("expected definition")
	}

	if diff := cmp.Diff(definitions[2], definition); diff != "" {
		t.Errorf("unexpected definition (-want, +got):\n%s", diff)
	}
}

func TestLeaves(t *testing.T) {
	definitions := []Definition{
		{ID: 1, UpFilename: "1.up.sql"},
		{ID: 2, UpFilename: "2.up.sql", Metadata: Metadata{Parents: []int{1}}},
		{ID: 3, UpFilename: "3.up.sql", Metadata: Metadata{Parents: []int{2}}},
		{ID: 4, UpFilename: "4.up.sql", Metadata: Metadata{Parents: []int{2}}},
		{ID: 5, UpFilename: "5.up.sql", Metadata: Metadata{Parents: []int{3, 4}}},
		{ID: 6, UpFilename: "6.up.sql", Metadata: Metadata{Parents: []int{5}}},
		{ID: 7, UpFilename: "7.up.sql", Metadata: Metadata{Parents: []int{5}}},
		{ID: 8, UpFilename: "8.up.sql", Metadata: Metadata{Parents: []int{5, 6}}},
		{ID: 9, UpFilename: "9.up.sql", Metadata: Metadata{Parents: []int{5, 8}}},
	}

	expectedLeaves := []Definition{
		definitions[6],
		definitions[8],
	}
	if diff := cmp.Diff(expectedLeaves, newDefinitions(definitions).Leaves()); diff != "" {
		t.Errorf("unexpected leaves (-want, +got):\n%s", diff)
	}
}

func TestUp(t *testing.T) {
	definitions := []Definition{
		{ID: 1, UpFilename: "1.up.sql"},
		{ID: 2, UpFilename: "2.up.sql", Metadata: Metadata{Parents: []int{1}}},
		{ID: 3, UpFilename: "3.up.sql", Metadata: Metadata{Parents: []int{2}}},
		{ID: 4, UpFilename: "4.up.sql", Metadata: Metadata{Parents: []int{2}}},
		{ID: 5, UpFilename: "5.up.sql", Metadata: Metadata{Parents: []int{3, 4}}},
		{ID: 6, UpFilename: "6.up.sql", Metadata: Metadata{Parents: []int{5}}},
		{ID: 7, UpFilename: "7.up.sql", Metadata: Metadata{Parents: []int{5}}},
		{ID: 8, UpFilename: "8.up.sql", Metadata: Metadata{Parents: []int{5, 6}}},
		{ID: 9, UpFilename: "9.up.sql", Metadata: Metadata{Parents: []int{5, 8}}},
		{ID: 10, UpFilename: "10.up.sql", Metadata: Metadata{Parents: []int{7, 9}}},
	}

	for _, testCase := range []struct {
		name                string
		appliedIDs          []int
		targetIDs           []int
		expectedDefinitions []Definition
	}{
		{"empty", nil, nil, []Definition{}},
		{"empty to leaf", nil, []int{10}, definitions},
		{"empty to internal node", nil, []int{7}, append(append([]Definition(nil), definitions[0:5]...), definitions[6])},
		{"already applied", []int{1, 2, 3, 4, 5, 6, 8}, []int{8}, []Definition{}},
		{"partially applied", []int{1, 4, 5, 8}, []int{8}, append(append([]Definition(nil), definitions[1:3]...), definitions[5])},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			definitions, err := newDefinitions(definitions).Up(testCase.appliedIDs, testCase.targetIDs)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			if diff := cmp.Diff(testCase.expectedDefinitions, definitions); diff != "" {
				t.Errorf("unexpected definitions (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDown(t *testing.T) {
	definitions := []Definition{
		{ID: 1, UpFilename: "1.up.sql"},
		{ID: 2, UpFilename: "2.up.sql", Metadata: Metadata{Parents: []int{1}}},
		{ID: 3, UpFilename: "3.up.sql", Metadata: Metadata{Parents: []int{2}}},
		{ID: 4, UpFilename: "4.up.sql", Metadata: Metadata{Parents: []int{2}}},
		{ID: 5, UpFilename: "5.up.sql", Metadata: Metadata{Parents: []int{3, 4}}},
		{ID: 6, UpFilename: "6.up.sql", Metadata: Metadata{Parents: []int{5}}},
		{ID: 7, UpFilename: "7.up.sql", Metadata: Metadata{Parents: []int{5}}},
		{ID: 8, UpFilename: "8.up.sql", Metadata: Metadata{Parents: []int{5, 6}}},
		{ID: 9, UpFilename: "9.up.sql", Metadata: Metadata{Parents: []int{5, 8}}},
		{ID: 10, UpFilename: "10.up.sql", Metadata: Metadata{Parents: []int{7, 9}}},
	}

	reverse := func(definitions []Definition) []Definition {
		reversed := make([]Definition, 0, len(definitions))
		for i := len(definitions) - 1; i >= 0; i-- {
			reversed = append(reversed, definitions[i])
		}

		return reversed
	}

	for _, testCase := range []struct {
		name                string
		appliedIDs          []int
		targetIDs           []int
		expectedDefinitions []Definition
	}{
		{"empty", nil, nil, []Definition{}},
		{"unapply dominator", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, []int{5}, reverse(definitions[5:])},
		{"unapply non-dominator (1)", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, []int{6}, reverse(definitions[7:])},
		{"unapply non-dominator (2)", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, []int{7}, reverse(definitions[9:])},
		{"partial unapplied", []int{1, 2, 3, 4, 5, 6, 7, 10}, []int{5}, reverse(append(append([]Definition(nil), definitions[5:7]...), definitions[9]))},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			definitions, err := newDefinitions(definitions).Down(testCase.appliedIDs, testCase.targetIDs)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			if diff := cmp.Diff(testCase.expectedDefinitions, definitions); diff != "" {
				t.Errorf("unexpected definitions (-want, +got):\n%s", diff)
			}
		})
	}
}
