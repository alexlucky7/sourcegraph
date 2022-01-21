package runner

import (
	"fmt"
	"strconv"
	"strings"
)

type SchemaOutOfDateError struct {
	schemaName      string
	missingVersions []int
}

func (e *SchemaOutOfDateError) Error() string {
	ids := make([]string, 0, len(e.missingVersions))
	for _, id := range e.missingVersions {
		ids = append(ids, strconv.Itoa(id))
	}

	return (instructionalError{
		class:       "schema out of date",
		description: fmt.Sprintf("schema %q requires the following migrations to be applied: %s\n", e.schemaName, strings.Join(ids, ", ")),
		instructions: strings.Join([]string{
			`This software expects a migrator instance to have run on this schema prior to the deployment of this process.`,
			`If this error is occurring directly after an upgrade, roll back your instance to the previous versiona nd ensure the migrator instance runs successfully prior attempting to re-upgrade.`,
		}, " "),
	}).Error()
}

func newOutOfDateError(schemaContext schemaContext, schemaVersion schemaVersion) error {
	definitions, err := schemaContext.schema.Definitions.Up(
		schemaVersion.appliedVersions,
		extractIDs(schemaContext.schema.Definitions.Leaves()),
	)
	if err != nil {
		return err
	}

	return &SchemaOutOfDateError{
		schemaName:      schemaContext.schema.Name,
		missingVersions: extractIDs(definitions),
	}
}

type instructionalError struct {
	class        string
	description  string
	instructions string
}

func (e instructionalError) Error() string {
	return fmt.Sprintf("%s: %s\n\n%s\n", e.class, e.description, e.instructions)
}

// errDirtyDatabase occurs when a database schema is marked as dirty but there does not
// appear to be any running instance currently migrating that schema. This occurs when
// a previous attempt to migrate the schema had not successfully completed and requires
// intervention of a site administrator.
var errDirtyDatabase = instructionalError{
	class:       "dirty database",
	description: "schema is marked as dirty but no migrator instance appears to be running",
	instructions: strings.Join([]string{
		`The target schema is marked as dirty and no other migration operation is seen running on this schema.`,
		`The last migration operation over this schema has failed (or, at least, the migrator instance issuing that migration has died).`,
		`Please contact support@sourcegraph.com for further assistance.`,
	}, " "),
}
