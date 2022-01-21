package runner

import (
	"fmt"

	"github.com/inconshreveable/log15"
)

type Options struct {
	Operations []MigrationOperation

	// Parallel controls whether we run schema migrations concurrently or not. By default,
	// we run schema migrations sequentially. This is to ensure that in testing, where the
	// same database can be targetted by multiple schemas, we do not hit errors that occur
	// when trying to install Postgres extensions concurrently (which do not seem txn-safe).
	Parallel bool
}

type MigrationOperation struct {
	SchemaName     string
	Type           MigrationOperationType
	TargetVersions []int
}

type MigrationOperationType int

const (
	MigrationOperationTypeTargetedUp MigrationOperationType = iota
	MigrationOperationTypeTargetedDown
	MigrationOperationTypeUpgrade
	MigrationOperationTypeRevert
)

func desugarOperation(schemaContext schemaContext, operation MigrationOperation) (MigrationOperation, error) {
	switch operation.Type {
	case MigrationOperationTypeUpgrade:
		return desugarUpgrade(schemaContext, operation), nil
	case MigrationOperationTypeRevert:
		return desugarRevert(schemaContext, operation)
	}

	return operation, nil
}

func desugarUpgrade(schemaContext schemaContext, operation MigrationOperation) MigrationOperation {
	leafVersions := extractIDs(schemaContext.schema.Definitions.Leaves())

	log15.Info(
		"Desugaring `upgrade` to `targetted up` operation",
		"schema", operation.SchemaName,
		"leafVersions", leafVersions,
	)

	return MigrationOperation{
		SchemaName:     operation.SchemaName,
		Type:           MigrationOperationTypeTargetedUp,
		TargetVersions: leafVersions,
	}
}

func desugarRevert(schemaContext schemaContext, operation MigrationOperation) (MigrationOperation, error) {
	schemaVersion := schemaContext.schemaVersion
	definitions := schemaContext.schema.Definitions

	counts := make(map[int]int, len(schemaVersion.appliedVersions))
	for _, versions := range schemaVersion.appliedVersions {
		counts[versions] = 0
	}

	for _, version := range schemaVersion.appliedVersions {
		definition, ok := definitions.GetByID(version)
		if !ok {
			return MigrationOperation{}, fmt.Errorf("unknown version %d", version)
		}

		for _, parent := range definition.Metadata.Parents {
			counts[parent]++
		}
	}

	leafVersions := make([]int, 0, len(counts))
	for k, v := range counts {
		if v == 0 {
			leafVersions = append(leafVersions, k)
		}
	}

	log15.Info(
		"Desugaring `revert` to `targetted down` operation",
		"schema", operation.SchemaName,
		"appliedLeafVersions", leafVersions,
	)

	if len(leafVersions) != 1 {
		return MigrationOperation{}, fmt.Errorf("ambiguous revert")
	}

	return MigrationOperation{
		SchemaName:     operation.SchemaName,
		Type:           MigrationOperationTypeTargetedDown,
		TargetVersions: leafVersions,
	}, nil
}
