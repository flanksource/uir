package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var registerOnce sync.Once

type projectListOptions struct{}

type queryOptions struct {
	Expression string `flag:"expression" help:"PEG query expression" required:"true"`
	Snapshot   string `flag:"snapshot" help:"Explicit snapshot UUID instead of the published head"`
	Root       string `flag:"root" help:"Default root key when the expression has no root predicate"`
	Limit      int    `flag:"limit" help:"Maximum rows from 1 through 1000" default:"100"`
}

func (queryOptions) ClickyActionFlags() {}

type reindexOptions struct {
	Path         string `flag:"path" help:"Workspace or repository path" default:"."`
	Root         string `flag:"root" help:"Stable key for the top-level root"`
	Name         string `flag:"name" help:"Project display name"`
	IncludeTests bool   `flag:"include-tests" help:"Include Go test files" default:"false"`
	Force        bool   `flag:"force" help:"Reparse every file and publish a new snapshot" default:"false"`
}

func (reindexOptions) ClickyActionFlags() {}

type projectRow struct {
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	SnapshotID  string    `json:"snapshot_id,omitempty"`
	HeadVersion int64     `json:"head_version"`
	Roots       int64     `json:"roots"`
	Sources     int64     `json:"sources"`
	Nodes       int64     `json:"nodes"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (row projectRow) GetID() string   { return row.Key }
func (row projectRow) GetName() string { return row.Name }

func (projectRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("key").Label("Project").Build(),
		api.Column("name").Label("Name").Build(),
		api.Column("head_version").Label("Version").Type("int").Build(),
		api.Column("snapshot_id").Label("Snapshot").Build(),
		api.Column("roots").Label("Roots").Type("int").Build(),
		api.Column("sources").Label("Sources").Type("int").Build(),
		api.Column("nodes").Label("Nodes").Type("int").Build(),
		api.Column("updated_at").Label("Updated").Kind("timestamp").Build(),
	}
}

func (row projectRow) Row() map[string]any {
	return map[string]any{
		"key": row.Key, "name": row.Name, "head_version": row.HeadVersion, "snapshot_id": row.SnapshotID,
		"roots": row.Roots, "sources": row.Sources, "nodes": row.Nodes, "updated_at": row.UpdatedAt,
	}
}

func registerEntities() {
	registerOnce.Do(func() {
		clicky.NewEntity[projectRow, projectListOptions, projectRow]("project").
			Aliases("projects").
			ToolGroup("uir").
			ListWithContext(listProjects).
			GetWithContext(getProject).
			WithAction(clicky.TypedActionWithContext("query", queryOptions{}, runQuery).
				WithShort("Run a PEG symbol or call query against a project snapshot")).
			WithAction(clicky.TypedActionWithContext("reindex", reindexOptions{}, runReindex).
				WithShort("Incrementally reindex Go syntax into a new immutable snapshot")).
			Register()
	})
}

func listProjects(ctx context.Context, _ projectListOptions) ([]projectRow, error) {
	database, err := databaseFor(ctx)
	if err != nil {
		return nil, err
	}
	var projects []storage.Project
	if err := database.WithContext(ctx).Order("project_key").Find(&projects).Error; err != nil {
		return nil, fmt.Errorf("list UIR projects: %w", err)
	}
	rows := make([]projectRow, 0, len(projects))
	for _, project := range projects {
		row, err := loadProjectRow(ctx, database, project)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func getProject(ctx context.Context, projectKey string) (projectRow, error) {
	database, err := databaseFor(ctx)
	if err != nil {
		return projectRow{}, err
	}
	var project storage.Project
	if err := database.WithContext(ctx).Where("project_key = ?", projectKey).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return projectRow{}, fmt.Errorf("project %q was not found", projectKey)
		}
		return projectRow{}, fmt.Errorf("load project %q: %w", projectKey, err)
	}
	return loadProjectRow(ctx, database, project)
}

func loadProjectRow(ctx context.Context, database *gorm.DB, project storage.Project) (projectRow, error) {
	row := projectRow{Key: project.ProjectKey, Name: project.Name, UpdatedAt: project.UpdatedAt}
	var head storage.ProjectHead
	if err := database.WithContext(ctx).Where("project_id = ?", project.ID).First(&head).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return row, nil
		}
		return projectRow{}, fmt.Errorf("load head for project %q: %w", project.ProjectKey, err)
	}
	row.SnapshotID, row.HeadVersion = head.SnapshotID.String(), head.Version
	counts := []struct {
		model any
		value *int64
		where string
		arg   uuid.UUID
	}{
		{model: &storage.Root{}, value: &row.Roots, where: "snapshot_id = ?", arg: head.SnapshotID},
		{model: &storage.Source{}, value: &row.Sources, where: "root_id IN (?)", arg: head.SnapshotID},
		{model: &storage.Node{}, value: &row.Nodes, where: "snapshot_id = ?", arg: head.SnapshotID},
	}
	for index, count := range counts {
		query := database.WithContext(ctx).Model(count.model)
		if index == 1 {
			query = query.Where("root_id IN (SELECT id FROM uir_roots WHERE snapshot_id = ?)", count.arg)
		} else {
			query = query.Where(count.where, count.arg)
		}
		if err := query.Count(count.value).Error; err != nil {
			return projectRow{}, fmt.Errorf("count project %q resources: %w", project.ProjectKey, err)
		}
	}
	return row, nil
}

func runQuery(ctx context.Context, projectKey string, options queryOptions) ([]queryRow, error) {
	database, err := databaseFor(ctx)
	if err != nil {
		return nil, err
	}
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return nil, err
	}
	result, err := pipeline.Run(ctx, options.Expression, query.ScopeOptions{
		ProjectKey: projectKey, SnapshotID: options.Snapshot, RootKey: options.Root, Limit: options.Limit,
	})
	if err != nil {
		return nil, err
	}
	return queryResultRows(ctx, database, result)
}

func runReindex(ctx context.Context, projectKey string, options reindexOptions) (indexer.Result, error) {
	database, err := databaseFor(ctx)
	if err != nil {
		return indexer.Result{}, err
	}
	engine, err := indexer.New(database)
	if err != nil {
		return indexer.Result{}, err
	}
	return indexer.RunTask(ctx, engine, indexer.Options{
		ProjectKey: projectKey, ProjectName: options.Name, Path: options.Path,
		RootKey: options.Root, IncludeTests: options.IncludeTests, Force: options.Force,
	})
}
