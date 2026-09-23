package query

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	defaultLimit = 100
	maximumLimit = 1000
)

type resolution struct {
	query      Query
	project    storage.Project
	snapshot   storage.Snapshot
	root       *storage.Root
	predicates []Predicate
	limit      int
	stages     []ResolutionStage
}

// Run parses, resolves, and executes one query without broadening its requested scope.
func (pipeline *Pipeline) Run(ctx context.Context, input string, options ScopeOptions) (Result, error) {
	if pipeline == nil || pipeline.database == nil {
		return Result{}, errors.New("UIR query database is required")
	}
	parsed, err := Parse(input)
	if err != nil {
		return Result{}, err
	}
	resolved := resolution{query: parsed, limit: options.Limit}
	resolved.addStage("parse", string(parsed.Operation))
	if err := resolved.validateLimit(); err != nil {
		return Result{}, err
	}
	if err := pipeline.resolveScope(ctx, &resolved, options); err != nil {
		return Result{}, err
	}
	return pipeline.execute(ctx, &resolved)
}

func (resolved *resolution) validateLimit() error {
	if resolved.limit == 0 {
		resolved.limit = defaultLimit
	}
	if resolved.limit < 1 || resolved.limit > maximumLimit {
		return fmt.Errorf("UIR query limit must be between 1 and %d, got %d", maximumLimit, resolved.limit)
	}
	return nil
}

func (resolved *resolution) addStage(name, value string) {
	resolved.stages = append(resolved.stages, ResolutionStage{Name: name, Value: value})
}

func (pipeline *Pipeline) resolveScope(ctx context.Context, resolved *resolution, options ScopeOptions) error {
	project, snapshot, err := pipeline.resolveSnapshot(ctx, options)
	if err != nil {
		return fmt.Errorf("resolve UIR query snapshot: %w", err)
	}
	resolved.project, resolved.snapshot = project, snapshot
	resolved.addStage("project", project.ProjectKey)
	resolved.addStage("snapshot", snapshot.ID.String())

	rootKey, predicates, err := rootSelector(resolved.query.Predicates, options.RootKey)
	if err != nil {
		return fmt.Errorf("resolve UIR query root: %w", err)
	}
	resolved.predicates = predicates
	root, err := pipeline.resolveRoot(ctx, snapshot.ID, rootKey)
	if err != nil {
		return fmt.Errorf("resolve UIR query root: %w", err)
	}
	resolved.root = root
	resolved.addStage("root", displayRoot(root))
	return nil
}

func (pipeline *Pipeline) resolveSnapshot(ctx context.Context, options ScopeOptions) (storage.Project, storage.Snapshot, error) {
	if options.SnapshotID != "" {
		return pipeline.resolveExplicitSnapshot(ctx, options)
	}
	if options.ProjectKey == "" {
		return storage.Project{}, storage.Snapshot{}, errors.New("project key or snapshot ID is required")
	}
	var project storage.Project
	if err := pipeline.database.WithContext(ctx).Where("project_key = ?", options.ProjectKey).First(&project).Error; err != nil {
		return storage.Project{}, storage.Snapshot{}, lookupError(err, fmt.Sprintf("project %q", options.ProjectKey))
	}
	var head storage.ProjectHead
	if err := pipeline.database.WithContext(ctx).Where("project_id = ?", project.ID).First(&head).Error; err != nil {
		return storage.Project{}, storage.Snapshot{}, lookupError(err, fmt.Sprintf("published head for project %q", options.ProjectKey))
	}
	var snapshot storage.Snapshot
	if err := pipeline.database.WithContext(ctx).Where("id = ? AND project_id = ?", head.SnapshotID, project.ID).First(&snapshot).Error; err != nil {
		return storage.Project{}, storage.Snapshot{}, lookupError(err, fmt.Sprintf("head snapshot %s", head.SnapshotID))
	}
	return project, snapshot, nil
}

func (pipeline *Pipeline) resolveExplicitSnapshot(ctx context.Context, options ScopeOptions) (storage.Project, storage.Snapshot, error) {
	snapshotID, err := uuid.Parse(options.SnapshotID)
	if err != nil {
		return storage.Project{}, storage.Snapshot{}, fmt.Errorf("invalid snapshot ID %q: %w", options.SnapshotID, err)
	}
	var snapshot storage.Snapshot
	if err := pipeline.database.WithContext(ctx).Where("id = ?", snapshotID).First(&snapshot).Error; err != nil {
		return storage.Project{}, storage.Snapshot{}, lookupError(err, fmt.Sprintf("snapshot %q", options.SnapshotID))
	}
	var project storage.Project
	query := pipeline.database.WithContext(ctx).Where("id = ?", snapshot.ProjectID)
	if options.ProjectKey != "" {
		query = query.Where("project_key = ?", options.ProjectKey)
	}
	if err := query.First(&project).Error; err != nil {
		return storage.Project{}, storage.Snapshot{}, lookupError(err, fmt.Sprintf("project for snapshot %q", options.SnapshotID))
	}
	return project, snapshot, nil
}

func rootSelector(predicates []Predicate, optionRoot string) (string, []Predicate, error) {
	rootKey := optionRoot
	nodePredicates := make([]Predicate, 0, len(predicates))
	for _, predicate := range predicates {
		if predicate.Field != "root" {
			nodePredicates = append(nodePredicates, predicate)
			continue
		}
		if predicate.Value == "" {
			return "", nil, errors.New("root predicate cannot be empty")
		}
		if rootKey != "" && rootKey != predicate.Value {
			return "", nil, fmt.Errorf("root %q conflicts with root %q from the query", rootKey, predicate.Value)
		}
		rootKey = predicate.Value
	}
	return rootKey, nodePredicates, nil
}

func (pipeline *Pipeline) resolveRoot(ctx context.Context, snapshotID uuid.UUID, rootKey string) (*storage.Root, error) {
	if rootKey == "" {
		return nil, nil
	}
	var root storage.Root
	if err := pipeline.database.WithContext(ctx).Where("snapshot_id = ? AND root_key = ?", snapshotID, rootKey).First(&root).Error; err != nil {
		return nil, lookupError(err, fmt.Sprintf("root %q", rootKey))
	}
	return &root, nil
}

func lookupError(err error, subject string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%s was not found", subject)
	}
	return fmt.Errorf("load %s: %w", subject, err)
}

func displayRoot(root *storage.Root) string {
	if root == nil {
		return "*"
	}
	return root.RootKey
}
