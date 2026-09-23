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
	ParentIdentity string `gorm:"column:parent_identity"`
	StartLine      *int   `gorm:"column:start_line"`
	EndLine        *int   `gorm:"column:end_line"`
	Column         *int   `gorm:"column:column"`
}

func (indexer *Indexer) copyFile(ctx context.Context, source storage.Source) (fileIndex, error) {
	rows, err := indexer.loadCachedNodes(ctx, source)
	if err != nil {
		return fileIndex{}, err
	}
	indexed, err := cachedFileIndex(source)
	if err != nil {
		return fileIndex{}, err
	}
	oldIdentityByID := make(map[uuid.UUID]string, len(rows))
	for _, row := range rows {
		oldIdentityByID[row.ID] = row.IdentityKey
		spec, err := indexer.cachedNodeSpec(ctx, row)
		if err != nil {
			return fileIndex{}, err
		}
		indexed.Nodes = append(indexed.Nodes, spec)
	}
	if err := indexer.copyRelationships(ctx, source, oldIdentityByID, &indexed); err != nil {
		return fileIndex{}, err
	}
	return indexed, nil
}

func (indexer *Indexer) loadCachedNodes(ctx context.Context, source storage.Source) ([]copiedNodeRow, error) {
	var rows []copiedNodeRow
	err := indexer.database.WithContext(ctx).Table("uir_nodes AS node").
		Select("node.*, COALESCE(parent.identity_key, '') AS parent_identity, location.start_line, location.end_line, location.column").
		Joins("JOIN uir_node_locations AS location ON location.node_id = node.id").
		Joins("LEFT JOIN uir_nodes AS parent ON parent.id = node.parent_id").
		Where("location.source_id = ? AND node.node_type <> ?", source.ID, uir.NodeTypePackage).
		Order("node.ordinal, node.identity_key").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load cached nodes for %q: %w", source.PathKey, err)
	}
	return rows, nil
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

func (indexer *Indexer) cachedNodeSpec(ctx context.Context, row copiedNodeRow) (nodeSpec, error) {
	spec := nodeSpec{
		Identifier: storageIdentifier(row.Node), ParentIdentity: row.ParentIdentity,
		ChildSlot: row.ChildSlot, Ordinal: row.Ordinal, Payload: row.Payload,
		SemanticHash: row.SemanticHash, StartLine: row.StartLine, EndLine: row.EndLine, Column: row.Column,
	}
	var field storage.Field
	if err := indexer.database.WithContext(ctx).Where("node_id = ?", row.ID).First(&field).Error; err == nil {
		field.NodeID = uuid.Nil
		spec.Field = &field
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nodeSpec{}, fmt.Errorf("load cached field for node %s: %w", row.ID, err)
	}
	return spec, nil
}

func (indexer *Indexer) copyRelationships(ctx context.Context, source storage.Source, identities map[uuid.UUID]string, indexed *fileIndex) error {
	var relationships []storage.Relationship
	if err := indexer.database.WithContext(ctx).Where("source_id = ?", source.ID).Order("edge_key").Find(&relationships).Error; err != nil {
		return fmt.Errorf("load cached relationships for %q: %w", source.PathKey, err)
	}
	for _, relationship := range relationships {
		call, err := indexer.cachedCallSpec(ctx, relationship, identities)
		if err != nil {
			return err
		}
		indexed.Calls = append(indexed.Calls, call)
	}
	return nil
}

func (indexer *Indexer) cachedCallSpec(ctx context.Context, relationship storage.Relationship, identities map[uuid.UUID]string) (callSpec, error) {
	var target uir.Identifier
	if err := json.Unmarshal(relationship.ToIdentifier, &target); err != nil {
		return callSpec{}, fmt.Errorf("decode target for relationship %s: %w", relationship.ID, err)
	}
	var properties struct {
		Resolvable *bool `json:"resolvable"`
	}
	if err := json.Unmarshal(relationship.Payload, &properties); err != nil {
		return callSpec{}, fmt.Errorf("decode payload for relationship %s: %w", relationship.ID, err)
	}
	if properties.Resolvable == nil {
		return callSpec{}, fmt.Errorf("relationship %s is missing resolvable syntax metadata", relationship.ID)
	}
	fromIdentity, ok := identities[relationship.FromNodeID]
	if !ok {
		var from storage.Node
		if err := indexer.database.WithContext(ctx).Where("id = ?", relationship.FromNodeID).First(&from).Error; err != nil {
			return callSpec{}, fmt.Errorf("load source node for cached relationship %s: %w", relationship.ID, err)
		}
		fromIdentity = from.IdentityKey
	}
	return callSpec{
		FromIdentity: fromIdentity, ToIdentifier: target, ToRootKey: relationship.ToRootKey,
		Resolvable:    *properties.Resolvable,
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
