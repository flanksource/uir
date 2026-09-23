package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type previousSnapshot struct {
	Snapshot storage.Snapshot
	Head     storage.ProjectHead
	Roots    map[string]previousRoot
}

type previousRoot struct {
	Root    storage.Root
	Sources map[string]storage.Source
}

func (indexer *Indexer) ensureProject(ctx context.Context, options Options) (storage.Project, error) {
	var project storage.Project
	err := indexer.database.WithContext(ctx).Where("project_key = ?", options.ProjectKey).First(&project).Error
	if err == nil {
		return indexer.updateProjectName(ctx, project, options.ProjectName)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return storage.Project{}, fmt.Errorf("load UIR project %q: %w", options.ProjectKey, err)
	}
	now := time.Now().UTC()
	name := options.ProjectName
	if name == "" {
		name = options.ProjectKey
	}
	project = storage.Project{
		ID: uuid.New(), ProjectKey: options.ProjectKey, Name: name,
		Properties: storage.JSON(`{}`), CreatedAt: now, UpdatedAt: now,
	}
	if err := indexer.database.WithContext(ctx).Create(&project).Error; err != nil {
		return storage.Project{}, fmt.Errorf("create UIR project %q: %w", options.ProjectKey, err)
	}
	return project, nil
}

func (indexer *Indexer) updateProjectName(ctx context.Context, project storage.Project, name string) (storage.Project, error) {
	if name == "" || name == project.Name {
		return project, nil
	}
	if err := indexer.database.WithContext(ctx).Model(&project).Updates(map[string]any{
		"name": name, "updated_at": time.Now().UTC(),
	}).Error; err != nil {
		return storage.Project{}, fmt.Errorf("update UIR project %q: %w", project.ProjectKey, err)
	}
	project.Name = name
	return project, nil
}

func (indexer *Indexer) loadPrevious(ctx context.Context, project storage.Project) (*previousSnapshot, error) {
	var head storage.ProjectHead
	if err := indexer.database.WithContext(ctx).Where("project_id = ?", project.ID).First(&head).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("load head for project %q: %w", project.ProjectKey, err)
	}
	var snapshot storage.Snapshot
	if err := indexer.database.WithContext(ctx).Where("id = ?", head.SnapshotID).First(&snapshot).Error; err != nil {
		return nil, fmt.Errorf("load head snapshot %s: %w", head.SnapshotID, err)
	}
	var roots []storage.Root
	if err := indexer.database.WithContext(ctx).Where("snapshot_id = ?", snapshot.ID).Find(&roots).Error; err != nil {
		return nil, fmt.Errorf("load roots for snapshot %s: %w", snapshot.ID, err)
	}
	previous := &previousSnapshot{Snapshot: snapshot, Head: head, Roots: make(map[string]previousRoot, len(roots))}
	for _, root := range roots {
		sources, err := indexer.loadSources(ctx, root)
		if err != nil {
			return nil, err
		}
		previous.Roots[root.RootKey] = previousRoot{Root: root, Sources: sources}
	}
	return previous, nil
}

func (indexer *Indexer) loadSources(ctx context.Context, root storage.Root) (map[string]storage.Source, error) {
	var sources []storage.Source
	if err := indexer.database.WithContext(ctx).Where("root_id = ?", root.ID).Find(&sources).Error; err != nil {
		return nil, fmt.Errorf("load sources for root %q: %w", root.RootKey, err)
	}
	byPath := make(map[string]storage.Source, len(sources))
	for _, source := range sources {
		byPath[source.PathKey] = source
	}
	return byPath, nil
}

type copiedNodeRow struct {
	storage.Node
	SourceID       uuid.UUID `gorm:"column:source_id"`
	ParentIdentity string    `gorm:"column:parent_identity"`
	StartLine      *int      `gorm:"column:start_line"`
	EndLine        *int      `gorm:"column:end_line"`
	Column         *int      `gorm:"column:column"`
}

func (indexer *Indexer) loadCachedFiles(ctx context.Context, sources []storage.Source) (map[uuid.UUID]fileIndex, error) {
	files := make(map[uuid.UUID]fileIndex, len(sources))
	ids := make([]uuid.UUID, 0, len(sources))
	for _, source := range sources {
		file, err := cachedFileIndex(source)
		if err != nil {
			return nil, err
		}
		files[source.ID] = file
		ids = append(ids, source.ID)
	}
	if len(ids) == 0 {
		return files, nil
	}
	nodes, err := indexer.loadCachedNodes(ctx, ids)
	if err != nil {
		return nil, err
	}
	fields, err := indexer.loadCachedFields(ctx, ids)
	if err != nil {
		return nil, err
	}
	identities := make(map[uuid.UUID]string, len(nodes))
	for _, row := range nodes {
		identities[row.ID] = row.IdentityKey
		file := files[row.SourceID]
		file.Nodes = append(file.Nodes, cachedNodeSpec(row, fields))
		files[row.SourceID] = file
	}
	if err := indexer.loadCachedCalls(ctx, ids, identities, files); err != nil {
		return nil, err
	}
	return files, nil
}

func (indexer *Indexer) loadCachedNodes(ctx context.Context, ids []uuid.UUID) ([]copiedNodeRow, error) {
	var rows []copiedNodeRow
	for start := 0; start < len(ids); start += 256 {
		end := min(start+256, len(ids))
		var batch []copiedNodeRow
		err := indexer.database.WithContext(ctx).Table("uir_nodes AS node").
			Select("node.*, COALESCE(parent.identity_key, '') AS parent_identity, location.source_id, location.start_line, location.end_line, location.column").
			Joins("JOIN uir_node_locations AS location ON location.node_id = node.id").
			Joins("LEFT JOIN uir_nodes AS parent ON parent.id = node.parent_id").
			Where("location.source_id IN ? AND node.node_type <> ?", ids[start:end], uir.NodeTypePackage).
			Order("location.source_id, node.ordinal, node.identity_key").Scan(&batch).Error
		if err != nil {
			return nil, fmt.Errorf("load cached nodes: %w", err)
		}
		rows = append(rows, batch...)
	}
	return rows, nil
}

func (indexer *Indexer) loadCachedFields(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]storage.Field, error) {
	fields := make(map[uuid.UUID]storage.Field)
	for start := 0; start < len(ids); start += 256 {
		end := min(start+256, len(ids))
		var batch []storage.Field
		err := indexer.database.WithContext(ctx).Table("uir_fields AS field").Select("field.*").
			Joins("JOIN uir_node_locations AS location ON location.node_id = field.node_id").
			Where("location.source_id IN ?", ids[start:end]).Scan(&batch).Error
		if err != nil {
			return nil, fmt.Errorf("load cached field projections: %w", err)
		}
		for _, field := range batch {
			fields[field.NodeID] = field
		}
	}
	return fields, nil
}

func cachedFileIndex(source storage.Source) (fileIndex, error) {
	var properties struct {
		PackagePath string `json:"package_path"`
		PackageName string `json:"package_name"`
	}
	if err := json.Unmarshal(source.Properties, &properties); err != nil {
		return fileIndex{}, fmt.Errorf("decode cached source properties for %q: %w", source.PathKey, err)
	}
	return fileIndex{PackagePath: properties.PackagePath, PackageName: properties.PackageName}, nil
}

func cachedNodeSpec(row copiedNodeRow, fields map[uuid.UUID]storage.Field) nodeSpec {
	spec := nodeSpec{
		Identifier: storageIdentifier(row.Node), ParentIdentity: row.ParentIdentity,
		ChildSlot: row.ChildSlot, Ordinal: row.Ordinal, Payload: row.Payload,
		SemanticHash: row.SemanticHash, StartLine: row.StartLine, EndLine: row.EndLine, Column: row.Column,
	}
	if field, found := fields[row.ID]; found {
		field.NodeID = uuid.Nil
		spec.Field = &field
	}
	return spec
}

func (indexer *Indexer) loadCachedCalls(ctx context.Context, ids []uuid.UUID, identities map[uuid.UUID]string, files map[uuid.UUID]fileIndex) error {
	for start := 0; start < len(ids); start += 256 {
		end := min(start+256, len(ids))
		var relationships []storage.Relationship
		if err := indexer.database.WithContext(ctx).Where("source_id IN ?", ids[start:end]).Order("source_id, edge_key").Find(&relationships).Error; err != nil {
			return fmt.Errorf("load cached relationships: %w", err)
		}
		if err := indexer.loadMissingCallSources(ctx, relationships, identities); err != nil {
			return err
		}
		for _, relationship := range relationships {
			call, err := cachedCallSpec(relationship, identities)
			if err != nil {
				return err
			}
			file := files[*relationship.SourceID]
			file.Calls = append(file.Calls, call)
			files[*relationship.SourceID] = file
		}
	}
	return nil
}

func (indexer *Indexer) loadMissingCallSources(ctx context.Context, relationships []storage.Relationship, identities map[uuid.UUID]string) error {
	missing := make(map[uuid.UUID]struct{})
	for _, relationship := range relationships {
		if _, found := identities[relationship.FromNodeID]; !found {
			missing[relationship.FromNodeID] = struct{}{}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(missing))
	for id := range missing {
		ids = append(ids, id)
	}
	for start := 0; start < len(ids); start += 256 {
		end := min(start+256, len(ids))
		var nodes []storage.Node
		if err := indexer.database.WithContext(ctx).Where("id IN ?", ids[start:end]).Find(&nodes).Error; err != nil {
			return fmt.Errorf("load missing cached call sources: %w", err)
		}
		for _, node := range nodes {
			identities[node.ID] = node.IdentityKey
		}
	}
	return nil
}

func cachedCallSpec(relationship storage.Relationship, identities map[uuid.UUID]string) (callSpec, error) {
	var properties struct {
		Resolvable       *bool           `json:"resolvable"`
		SourceIdentifier *uir.Identifier `json:"source_identifier"`
		SourceRootKey    *string         `json:"source_root_key"`
		LocalRoot        bool            `json:"local_root"`
	}
	if err := json.Unmarshal(relationship.Payload, &properties); err != nil {
		return callSpec{}, fmt.Errorf("decode payload for relationship %s: %w", relationship.ID, err)
	}
	if properties.Resolvable == nil || properties.SourceIdentifier == nil {
		return callSpec{}, fmt.Errorf("relationship %s is missing source syntax metadata", relationship.ID)
	}
	fromIdentity, ok := identities[relationship.FromNodeID]
	if !ok {
		return callSpec{}, fmt.Errorf("cached relationship %s has unknown source node %s", relationship.ID, relationship.FromNodeID)
	}
	return callSpec{
		FromIdentity: fromIdentity, ToIdentifier: *properties.SourceIdentifier, ToRootKey: properties.SourceRootKey,
		LocalRoot: properties.LocalRoot, Resolvable: *properties.Resolvable,
		StatementPath: cachedStatementPath(relationship), StartLine: relationship.StartLine, EndLine: relationship.EndLine,
		Column: relationship.Column, Text: relationship.Text,
	}, nil
}

func cachedStatementPath(relationship storage.Relationship) string {
	if relationship.StatementPath != "" {
		return relationship.StatementPath
	}
	return relationship.EdgeKey
}

func storageIdentifier(node storage.Node) uir.Identifier {
	return uir.Identifier{
		Module: node.Module, Package: node.Package, Type: node.TypeName, Method: node.Method,
		Field: node.Field, Signature: node.Signature, NodeType: uir.NodeType(node.NodeType),
	}
}
