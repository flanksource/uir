package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/entity"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type browseOptions struct {
	Project  string `flag:"project" help:"Project key"`
	Snapshot string `flag:"snapshot" help:"Snapshot UUID"`
	Root     string `flag:"root" help:"Root UUID"`
	Source   string `flag:"source" help:"Source UUID"`
	Search   string `flag:"search" help:"Symbol or path search"`
	Limit    int    `flag:"limit" help:"Maximum rows" default:"100"`
	Offset   int    `flag:"offset" help:"Rows to skip" default:"0"`
}

type browseRow struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	ProjectKey  string     `json:"project_key,omitempty"`
	SnapshotID  string     `json:"snapshot_id,omitempty"`
	RootID      string     `json:"root_id,omitempty"`
	SourceID    string     `json:"source_id,omitempty"`
	Path        string     `json:"path,omitempty"`
	NodeType    string     `json:"node_type,omitempty"`
	Symbol      string     `json:"symbol,omitempty"`
	Language    string     `json:"language,omitempty"`
	State       string     `json:"state,omitempty"`
	Head        bool       `json:"head,omitempty"`
	Version     int64      `json:"version,omitempty"`
	Kind        string     `json:"kind,omitempty"`
	Repository  string     `json:"repository,omitempty"`
	Revision    string     `json:"revision,omitempty"`
	LocalPath   string     `json:"local_path,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	StartLine   *int       `json:"start_line,omitempty"`
	EndLine     *int       `json:"end_line,omitempty"`
}

func (row browseRow) GetID() string   { return row.ID }
func (row browseRow) GetName() string { return row.Name }

func registerBrowseEntities() {
	clicky.NewEntity[browseRow, browseOptions, browseRow]("snapshot").
		ToolGroup("uir").ListPagedWithContext(listSnapshots).GetWithContext(getSnapshot).Register()
	clicky.NewEntity[browseRow, browseOptions, browseRow]("root").
		ToolGroup("uir").ListPagedWithContext(listRoots).GetWithContext(getRoot).Register()
	clicky.NewEntity[browseRow, browseOptions, browseRow]("source").
		ToolGroup("uir").ListPagedWithContext(listSources).GetWithContext(getSource).
		WithAction(clicky.TypedActionWithContext("content", sourceContentOptions{}, readSourceContent).
			WithShort("Read source referenced by a saved snapshot").WithMethod(http.MethodGet)).Register()
	clicky.NewEntity[browseRow, browseOptions, nodeDetails]("node").
		ToolGroup("uir").ListPagedWithContext(listNodes).GetWithContext(getNode).Register()
}

func page(options browseOptions) (int, int, error) {
	limit := options.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 1000 || options.Offset < 0 {
		return 0, 0, fmt.Errorf("invalid pagination: limit must be 1..1000 and offset >= 0, got %d/%d", limit, options.Offset)
	}
	return limit, options.Offset, nil
}

func snapshotFor(ctx context.Context, database *gorm.DB, id string) (storage.Snapshot, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return storage.Snapshot{}, entity.NewStatusErrorf(http.StatusBadRequest, "invalid_snapshot", "invalid snapshot UUID %q", id)
	}
	var snapshot storage.Snapshot
	if err := database.WithContext(ctx).Where("id = ?", parsed).First(&snapshot).Error; err != nil {
		return storage.Snapshot{}, browseError(err, "snapshot", id)
	}
	return snapshot, nil
}

func browseError(err error, kind, id string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return entity.NewStatusErrorf(http.StatusNotFound, "not_found", "%s %q was not found", kind, id)
	}
	return fmt.Errorf("load %s %q: %w", kind, id, err)
}

func listSnapshots(ctx context.Context, options browseOptions) (clicky.PagedResult[browseRow], error) {
	if options.Project == "" {
		return clicky.PagedResult[browseRow]{}, entity.NewStatusErrorf(http.StatusBadRequest, "project_required", "project is required")
	}
	limit, offset, err := page(options)
	if err != nil {
		return clicky.PagedResult[browseRow]{}, err
	}
	database, err := databaseFor(ctx)
	if err != nil {
		return clicky.PagedResult[browseRow]{}, err
	}
	var project storage.Project
	if err := database.WithContext(ctx).Where("project_key = ?", options.Project).First(&project).Error; err != nil {
		return clicky.PagedResult[browseRow]{}, browseError(err, "project", options.Project)
	}
	query := database.WithContext(ctx).Model(&storage.Snapshot{}).Where("project_id = ?", project.ID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return clicky.PagedResult[browseRow]{}, fmt.Errorf("count snapshots: %w", err)
	}
	var snapshots []storage.Snapshot
	if err := query.Order("started_at DESC, id DESC").Limit(limit).Offset(offset).Find(&snapshots).Error; err != nil {
		return clicky.PagedResult[browseRow]{}, fmt.Errorf("list snapshots: %w", err)
	}
	var head storage.ProjectHead
	err = database.WithContext(ctx).Where("project_id = ?", project.ID).First(&head).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return clicky.PagedResult[browseRow]{}, fmt.Errorf("load project head: %w", err)
	}
	rows := make([]browseRow, 0, len(snapshots))
	for _, snapshot := range snapshots {
		row := browseRow{ID: snapshot.ID.String(), Name: snapshot.ID.String(), ProjectKey: project.ProjectKey,
			State: string(snapshot.State), StartedAt: &snapshot.StartedAt, CompletedAt: snapshot.CompletedAt,
			Head: snapshot.ID == head.SnapshotID}
		if row.Head {
			row.Version = head.Version
		}
		rows = append(rows, row)
	}
	return clicky.NewPagedResult(rows, limit, offset, total), nil
}

func listRoots(ctx context.Context, options browseOptions) (clicky.PagedResult[browseRow], error) {
	limit, offset, err := page(options)
	if err != nil {
		return clicky.PagedResult[browseRow]{}, err
	}
	database, err := databaseFor(ctx)
	if err != nil {
		return clicky.PagedResult[browseRow]{}, err
	}
	if _, err := snapshotFor(ctx, database, options.Snapshot); err != nil {
		return clicky.PagedResult[browseRow]{}, err
	}
	query := database.WithContext(ctx).Model(&storage.Root{}).Where("snapshot_id = ?", options.Snapshot)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return clicky.PagedResult[browseRow]{}, fmt.Errorf("count roots: %w", err)
	}
	var roots []storage.Root
	if err := query.Order("root_key, id").Limit(limit).Offset(offset).Find(&roots).Error; err != nil {
		return clicky.PagedResult[browseRow]{}, fmt.Errorf("list roots: %w", err)
	}
	rows := make([]browseRow, 0, len(roots))
	for _, root := range roots {
		rows = append(rows, rootRow(root))
	}
	return clicky.NewPagedResult(rows, limit, offset, total), nil
}

func listSources(ctx context.Context, options browseOptions) (clicky.PagedResult[browseRow], error) {
	limit, offset, err := page(options)
	if err != nil {
		return clicky.PagedResult[browseRow]{}, err
	}
	database, err := databaseFor(ctx)
	if err != nil {
		return clicky.PagedResult[browseRow]{}, err
	}
	if _, err := snapshotFor(ctx, database, options.Snapshot); err != nil {
		return clicky.PagedResult[browseRow]{}, err
	}
	query := database.WithContext(ctx).Table("uir_sources AS source").
		Joins("JOIN uir_roots AS root ON root.id = source.root_id").Where("root.snapshot_id = ?", options.Snapshot)
	if options.Root != "" {
		query = query.Where("source.root_id = ?", options.Root)
	}
	if options.Search != "" {
		query = query.Where("source.path_key LIKE ?", "%"+options.Search+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return clicky.PagedResult[browseRow]{}, fmt.Errorf("count sources: %w", err)
	}
	var sources []storage.Source
	if err := query.Select("source.*").Order("source.path_key, source.id").Limit(limit).Offset(offset).Scan(&sources).Error; err != nil {
		return clicky.PagedResult[browseRow]{}, fmt.Errorf("list sources: %w", err)
	}
	rows := make([]browseRow, 0, len(sources))
	for _, source := range sources {
		rows = append(rows, sourceRow(source, options.Snapshot))
	}
	return clicky.NewPagedResult(rows, limit, offset, total), nil
}

func listNodes(ctx context.Context, options browseOptions) (clicky.PagedResult[browseRow], error) {
	limit, offset, err := page(options)
	if err != nil {
		return clicky.PagedResult[browseRow]{}, err
	}
	database, err := databaseFor(ctx)
	if err != nil {
		return clicky.PagedResult[browseRow]{}, err
	}
	if _, err := snapshotFor(ctx, database, options.Snapshot); err != nil {
		return clicky.PagedResult[browseRow]{}, err
	}
	query := database.WithContext(ctx).Model(&storage.Node{}).Where("snapshot_id = ?", options.Snapshot)
	if options.Root != "" {
		query = query.Where("root_id = ?", options.Root)
	}
	if options.Source != "" {
		query = query.Where("id IN (SELECT node_id FROM uir_node_locations WHERE source_id = ?)", options.Source)
	}
	if options.Search != "" {
		query = query.Where("symbol_key LIKE ?", "%"+options.Search+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return clicky.PagedResult[browseRow]{}, fmt.Errorf("count nodes: %w", err)
	}
	var nodes []storage.Node
	if err := query.Order("root_id, identity_key, id").Limit(limit).Offset(offset).Find(&nodes).Error; err != nil {
		return clicky.PagedResult[browseRow]{}, fmt.Errorf("list nodes: %w", err)
	}
	rows := make([]browseRow, 0, len(nodes))
	for _, node := range nodes {
		rows = append(rows, nodeRow(node))
	}
	return clicky.NewPagedResult(rows, limit, offset, total), nil
}

func rootRow(root storage.Root) browseRow {
	row := browseRow{ID: root.ID.String(), Name: root.RootKey, SnapshotID: root.SnapshotID.String(),
		Kind: root.Kind}
	if root.RepositoryURI != nil {
		row.Repository = *root.RepositoryURI
	}
	if root.Revision != nil {
		row.Revision = *root.Revision
	}
	if root.LocalPath != nil {
		row.LocalPath = *root.LocalPath
	}
	return row
}

func sourceRow(source storage.Source, snapshotID string) browseRow {
	return browseRow{ID: source.ID.String(), Name: source.DisplayPath, SnapshotID: snapshotID,
		RootID: source.RootID.String(), Path: source.PathKey, Kind: source.Kind, Language: source.Language}
}

func nodeRow(node storage.Node) browseRow {
	name := node.SymbolKey
	if name == "" {
		name = node.IdentityKey
	}
	return browseRow{ID: node.ID.String(), Name: name, SnapshotID: node.SnapshotID.String(),
		RootID: node.RootID.String(), NodeType: node.NodeType, Symbol: node.SymbolKey, Language: node.Language}
}

func getSnapshot(ctx context.Context, id string) (browseRow, error) {
	database, err := databaseFor(ctx)
	if err != nil {
		return browseRow{}, err
	}
	snapshot, err := snapshotFor(ctx, database, id)
	if err != nil {
		return browseRow{}, err
	}
	var project storage.Project
	if err := database.WithContext(ctx).Where("id = ?", snapshot.ProjectID).First(&project).Error; err != nil {
		return browseRow{}, browseError(err, "project", snapshot.ProjectID.String())
	}
	return browseRow{ID: snapshot.ID.String(), Name: snapshot.ID.String(), ProjectKey: project.ProjectKey,
		State: string(snapshot.State), StartedAt: &snapshot.StartedAt, CompletedAt: snapshot.CompletedAt}, nil
}

func getRoot(ctx context.Context, id string) (browseRow, error) {
	database, err := databaseFor(ctx)
	if err != nil {
		return browseRow{}, err
	}
	var root storage.Root
	if err := database.WithContext(ctx).Where("id = ?", id).First(&root).Error; err != nil {
		return browseRow{}, browseError(err, "root", id)
	}
	return rootRow(root), nil
}

func getSource(ctx context.Context, id string) (browseRow, error) {
	database, err := databaseFor(ctx)
	if err != nil {
		return browseRow{}, err
	}
	var source storage.Source
	if err := database.WithContext(ctx).Where("id = ?", id).First(&source).Error; err != nil {
		return browseRow{}, browseError(err, "source", id)
	}
	var root storage.Root
	if err := database.WithContext(ctx).Where("id = ?", source.RootID).First(&root).Error; err != nil {
		return browseRow{}, browseError(err, "root", source.RootID.String())
	}
	return sourceRow(source, root.SnapshotID.String()), nil
}

type nodeDetails struct {
	Node          storage.Node           `json:"node"`
	Fields        []storage.Field        `json:"fields"`
	Locations     []storage.NodeLocation `json:"locations"`
	Relationships []storage.Relationship `json:"relationships"`
}

func getNode(ctx context.Context, id string) (nodeDetails, error) {
	database, err := databaseFor(ctx)
	if err != nil {
		return nodeDetails{}, err
	}
	var details nodeDetails
	if err := database.WithContext(ctx).Where("id = ?", id).First(&details.Node).Error; err != nil {
		return nodeDetails{}, browseError(err, "node", id)
	}
	if err := database.WithContext(ctx).Where("node_id = ?", details.Node.ID).Order("ordinal").Find(&details.Fields).Error; err != nil {
		return nodeDetails{}, fmt.Errorf("load node fields: %w", err)
	}
	if err := database.WithContext(ctx).Where("node_id = ?", details.Node.ID).Order("ordinal").Find(&details.Locations).Error; err != nil {
		return nodeDetails{}, fmt.Errorf("load node locations: %w", err)
	}
	if err := database.WithContext(ctx).Where("from_node_id = ?", details.Node.ID).Order("edge_key").Find(&details.Relationships).Error; err != nil {
		return nodeDetails{}, fmt.Errorf("load node relationships: %w", err)
	}
	return details, nil
}
