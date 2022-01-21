package runner

import (
	"context"
	"fmt"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/hashicorp/go-multierror"
	"github.com/inconshreveable/log15"

	"github.com/sourcegraph/sourcegraph/internal/database/migration/definition"
	"github.com/sourcegraph/sourcegraph/internal/database/migration/schemas"
)

type Runner struct {
	storeFactories map[string]StoreFactory
}

type StoreFactory func(ctx context.Context) (Store, error)

func NewRunner(storeFactories map[string]StoreFactory) *Runner {
	return &Runner{
		storeFactories: storeFactories,
	}
}

func (r *Runner) Run(ctx context.Context, options Options) error {
	schemaNames := make([]string, 0, len(options.Operations))
	for _, operation := range options.Operations {
		schemaNames = append(schemaNames, operation.SchemaName)
	}

	operationMap := make(map[string]MigrationOperation, len(options.Operations))
	for _, operation := range options.Operations {
		operationMap[operation.SchemaName] = operation
	}
	if len(operationMap) != len(options.Operations) {
		return fmt.Errorf("multiple operations defined on the same schema")
	}

	numRoutines := 1
	if options.Parallel {
		numRoutines = len(schemaNames)
	}
	semaphore := make(chan struct{}, numRoutines)

	return r.forEachSchema(ctx, schemaNames, func(ctx context.Context, schemaName string, schemaContext schemaContext) error {
		// Block until we can write into this channel. This ensures that we only have at most
		// the same number of active goroutines as we have slots in the channel's buffer.
		semaphore <- struct{}{}
		defer func() { <-semaphore }()

		if err := r.runSchema(ctx, operationMap[schemaName], schemaContext); err != nil {
			return errors.Wrapf(err, "failed to run migration for schema %q", schemaName)
		}

		return nil
	})
}

func (r *Runner) runSchema(ctx context.Context, operation MigrationOperation, schemaContext schemaContext) (err error) {
	schemaVersion := schemaContext.schemaVersion
	definitions := schemaContext.schema.Definitions

	operation, err = desugarOperation(schemaContext, operation)
	if err != nil {
		return err
	}

	gatherMissingDefinitions := definitions.Up
	if operation.Type != MigrationOperationTypeTargetedUp {
		gatherMissingDefinitions = definitions.Down
	}
	missingDefinitions, err := gatherMissingDefinitions(schemaVersion.appliedVersions, operation.TargetVersions)
	if err != nil {
		return err
	}

	if noop, _ := currentMissingDefinitionStatus(schemaVersion, operation, missingDefinitions); noop {
		return nil
	}

	// TODO - poll
	if _, unlock, err := schemaContext.store.Lock(ctx); err != nil {
		return err
	} else {
		defer func() { err = unlock(err) }()
	}

	schemaVersion, err = r.fetchVersion(ctx, schemaContext.schema.Name, schemaContext.store)
	if err != nil {
		return err
	}

	if noop, dirty := currentMissingDefinitionStatus(schemaVersion, operation, missingDefinitions); dirty {
		return errDirtyDatabase
	} else if noop {
		return nil
	}

	missingDefinitions = filterAppliedDefinitions(schemaVersion, operation, missingDefinitions)
	if len(missingDefinitions) == 0 {
		return nil
	}

	log15.Info(
		"Applying migrations",
		"schema", schemaContext.schema.Name,
		"up", operation.Type == MigrationOperationTypeTargetedUp,
		"count", len(missingDefinitions),
	)

	for _, definition := range missingDefinitions {
		log15.Info(
			"Applying migration",
			"migrationID", definition.ID,
			"schema", schemaContext.schema.Name,
			"up", operation.Type == MigrationOperationTypeTargetedUp,
		)

		applyMigration := schemaContext.store.Up
		if operation.Type == MigrationOperationTypeTargetedDown {
			applyMigration = schemaContext.store.Down
		}
		if err := applyMigration(ctx, definition); err != nil {
			return errors.Wrapf(err, "failed to apply migration %d", definition.ID)
		}
	}

	return nil
}

func (r *Runner) Validate(ctx context.Context, schemaNames ...string) error {
	return r.forEachSchema(ctx, schemaNames, func(ctx context.Context, schemaName string, schemaContext schemaContext) error {
		return r.validateSchema(ctx, schemaName, schemaContext)
	})
}

func (r *Runner) validateSchema(ctx context.Context, schemaName string, schemaContext schemaContext) error {
	schemaVersion := schemaContext.schemaVersion
	allDefinitions := schemaContext.schema.Definitions.All()
	counts := countVersions(schemaVersion, allDefinitions)

	if counts.pendingCount+counts.failedCount > 0 {
		// TODO - poll
		if _, unlock, err := schemaContext.store.Lock(ctx); err != nil {
			return err
		} else {
			defer func() { err = unlock(err) }()
		}

		newSchemaVersion, err := r.fetchVersion(ctx, schemaContext.schema.Name, schemaContext.store)
		if err != nil {
			return err
		}

		schemaVersion = newSchemaVersion
		counts = countVersions(schemaVersion, allDefinitions)
	}

	if counts.pendingCount+counts.failedCount > 0 {
		return errDirtyDatabase
	}

	if counts.appliedCount != len(allDefinitions) {
		return newOutOfDateError(schemaContext, schemaVersion)
	}

	return nil
}

func (r *Runner) fetchVersion(ctx context.Context, schemaName string, store Store) (schemaVersion, error) {
	appliedVersions, pendingVersions, failedVersions, err := store.Versions(ctx)
	if err != nil {
		return schemaVersion{}, err
	}

	log15.Info(
		"Checked current version",
		"schema", schemaName,
		"appliedVersions", appliedVersions,
		"pendingVersions", pendingVersions,
		"failedVersions", failedVersions,
	)

	return schemaVersion{
		appliedVersions: appliedVersions,
		pendingVersions: pendingVersions,
		failedVersions:  failedVersions,
	}, nil
}

func currentMissingDefinitionStatus(schemaVersion schemaVersion, operation MigrationOperation, missingDefinitions []definition.Definition) (noop bool, dirty bool) {
	counts := countVersions(schemaVersion, missingDefinitions)
	if counts.failedCount+counts.pendingCount != 0 {
		return false, true
	}

	if operation.Type == MigrationOperationTypeTargetedUp && counts.appliedCount == len(missingDefinitions) {
		return true, false
	}
	if operation.Type == MigrationOperationTypeTargetedDown && counts.appliedCount == 0 {
		return true, false
	}

	return false, false
}

func filterAppliedDefinitions(schemaVersion schemaVersion, operation MigrationOperation, missingDefinitions []definition.Definition) []definition.Definition {
	appliedVersionMap := intSet(schemaVersion.appliedVersions)

	filtered := missingDefinitions[:0]
	for _, definition := range missingDefinitions {
		_, ok := appliedVersionMap[definition.ID]
		if operation.Type == MigrationOperationTypeTargetedUp && ok {
			continue
		}
		if operation.Type == MigrationOperationTypeTargetedDown && !ok {
			continue
		}

		filtered = append(filtered, definition)
	}

	return filtered
}

type versionCounts struct {
	appliedCount int
	pendingCount int
	failedCount  int
}

func countVersions(schemaVersion schemaVersion, definitions []definition.Definition) versionCounts {
	appliedVersionsMap := intSet(schemaVersion.appliedVersions)
	failedVersionsMap := intSet(schemaVersion.failedVersions)
	pendingVersionsMap := intSet(schemaVersion.pendingVersions)

	counts := versionCounts{}
	for _, definition := range definitions {
		if _, ok := appliedVersionsMap[definition.ID]; ok {
			counts.appliedCount++
		}
		if _, ok := pendingVersionsMap[definition.ID]; ok {
			counts.pendingCount++
		}
		if _, ok := failedVersionsMap[definition.ID]; ok {
			counts.failedCount++
		}
	}

	return counts
}

func extractIDs(definitions []definition.Definition) []int {
	ids := make([]int, 0, len(definitions))
	for _, definition := range definitions {
		ids = append(ids, definition.ID)
	}

	return ids
}

func intSet(vs []int) map[int]struct{} {
	m := make(map[int]struct{}, len(vs))
	for _, v := range vs {
		m[v] = struct{}{}
	}

	return m
}

type schemaContext struct {
	schema        *schemas.Schema
	store         Store
	schemaVersion schemaVersion
}

type schemaVersion struct {
	appliedVersions []int
	pendingVersions []int
	failedVersions  []int
}

type visitFunc func(ctx context.Context, schemaName string, schemaContext schemaContext) error

// forEachSchema invokes the given function once for each schema in the given list, with
// store instances initialized for each given schema name. Each function invocation occurs
// concurrently. Errors from each invocation are collected and returned. An error from one
// goroutine will not cancel the progress of another.
func (r *Runner) forEachSchema(ctx context.Context, schemaNames []string, visitor visitFunc) error {
	// Create map of relevant schemas keyed by name
	schemaMap, err := r.prepareSchemas(schemaNames)
	if err != nil {
		return err
	}

	// Create map of migration stores keyed by name
	storeMap, err := r.prepareStores(ctx, schemaNames)
	if err != nil {
		return err
	}

	// Create map of versions keyed by name
	versionMap, err := r.fetchVersions(ctx, storeMap)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	errorCh := make(chan error, len(schemaNames))

	for _, schemaName := range schemaNames {
		wg.Add(1)

		go func(schemaName string) {
			defer wg.Done()

			errorCh <- visitor(ctx, schemaName, schemaContext{
				schema:        schemaMap[schemaName],
				store:         storeMap[schemaName],
				schemaVersion: versionMap[schemaName],
			})
		}(schemaName)
	}

	wg.Wait()
	close(errorCh)

	var errs *multierror.Error
	for err := range errorCh {
		if err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return errs.ErrorOrNil()
}

func (r *Runner) prepareSchemas(schemaNames []string) (map[string]*schemas.Schema, error) {
	schemaMap := make(map[string]*schemas.Schema, len(schemaNames))

	for _, targetSchemaName := range schemaNames {
		for _, schema := range schemas.Schemas {
			if schema.Name == targetSchemaName {
				schemaMap[schema.Name] = schema
				break
			}
		}
	}

	// Ensure that all supplied schema names are valid
	for _, schemaName := range schemaNames {
		if _, ok := schemaMap[schemaName]; !ok {
			return nil, fmt.Errorf("unknown schema %q", schemaName)
		}
	}

	return schemaMap, nil
}

func (r *Runner) prepareStores(ctx context.Context, schemaNames []string) (map[string]Store, error) {
	storeMap := make(map[string]Store, len(schemaNames))

	for _, schemaName := range schemaNames {
		storeFactory, ok := r.storeFactories[schemaName]
		if !ok {
			return nil, fmt.Errorf("unknown schema %q", schemaName)
		}

		store, err := storeFactory(ctx)
		if err != nil {
			return nil, err
		}

		storeMap[schemaName] = store
	}

	return storeMap, nil
}

func (r *Runner) fetchVersions(ctx context.Context, storeMap map[string]Store) (map[string]schemaVersion, error) {
	versions := make(map[string]schemaVersion, len(storeMap))

	for schemaName, store := range storeMap {
		schemaVersion, err := r.fetchVersion(ctx, schemaName, store)
		if err != nil {
			return nil, err
		}

		versions[schemaName] = schemaVersion
	}

	return versions, nil
}
