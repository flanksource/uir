package main

import (
	"context"
	"fmt"

	"github.com/flanksource/clicky/api"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type queryRow struct {
	ID        string `json:"id"`
	Operation string `json:"operation"`
	Root      string `json:"root,omitempty"`
	NodeType  string `json:"node_type"`
	Symbol    string `json:"symbol"`
	Package   string `json:"package,omitempty"`
	Type      string `json:"type,omitempty"`
	Method    string `json:"method,omitempty"`
	Field     string `json:"field,omitempty"`
	Signature string `json:"signature,omitempty"`
	Language  string `json:"language,omitempty"`
	Location  string `json:"location,omitempty"`
}

func (queryRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("id").Label("ID").Build(),
		api.Column("operation").Label("Operation").Build(),
		api.Column("root").Label("Root").Build(),
		api.Column("node_type").Label("Kind").Build(),
		api.Column("symbol").Label("Symbol").Build(),
		api.Column("package").Label("Package").Build(),
		api.Column("type").Label("Type").Build(),
		api.Column("method").Label("Method").Build(),
		api.Column("field").Label("Field").Build(),
		api.Column("signature").Label("Signature").Build(),
		api.Column("language").Label("Language").Build(),
		api.Column("location").Label("Location").Build(),
	}
}

func (row queryRow) Row() map[string]any {
	return map[string]any{
		"id":        row.ID,
		"operation": row.Operation, "root": row.Root, "node_type": row.NodeType, "symbol": row.Symbol,
		"package": row.Package, "type": row.Type, "method": row.Method, "field": row.Field,
		"signature": row.Signature, "language": row.Language, "location": row.Location,
	}
}

type nodeLocationRow struct {
	NodeID      uuid.UUID `gorm:"column:node_id"`
	DisplayPath string    `gorm:"column:display_path"`
	StartLine   *int      `gorm:"column:start_line"`
	EndLine     *int      `gorm:"column:end_line"`
	Column      *int      `gorm:"column:column"`
}

func queryResultRows(ctx context.Context, database *gorm.DB, result query.Result) ([]queryRow, error) {
	roots, err := queryRoots(ctx, database, result.SnapshotID)
	if err != nil {
		return nil, err
	}
	locations, err := queryLocations(ctx, database, result.Nodes)
	if err != nil {
		return nil, err
	}
	relationshipSources := make(map[uuid.UUID]string, len(result.Relationships))
	if len(result.Relationships) > 0 {
		sourceIDs := make([]uuid.UUID, 0, len(result.Relationships))
		for _, relationship := range result.Relationships {
			if relationship.SourceID != nil {
				sourceIDs = append(sourceIDs, *relationship.SourceID)
			}
		}
		if len(sourceIDs) > 0 {
			var sources []storage.Source
			if err := database.WithContext(ctx).Where("id IN ?", sourceIDs).Find(&sources).Error; err != nil {
				return nil, fmt.Errorf("load query relationship sources: %w", err)
			}
			for _, source := range sources {
				relationshipSources[source.ID] = source.DisplayPath
			}
		}
	}
	rows := make([]queryRow, 0, len(result.Nodes)+len(result.Relationships))
	for _, node := range result.Nodes {
		rows = append(rows, queryRow{
			ID:        node.ID.String(),
			Operation: string(result.Operation), Root: roots[node.RootID], NodeType: node.NodeType,
			Symbol: node.SymbolKey, Package: node.Package, Type: node.TypeName, Method: node.Method,
			Field: node.Field, Signature: node.Signature, Language: node.Language, Location: locations[node.ID],
		})
	}
	for _, relationship := range result.Relationships {
		path := ""
		if relationship.SourceID != nil {
			path = relationshipSources[*relationship.SourceID]
		}
		rows = append(rows, queryRow{
			ID:        relationship.ID.String(),
			Operation: string(result.Operation), Root: roots[relationship.FromRootID], NodeType: relationship.RelationshipType,
			Symbol: relationship.ToSymbolKey, Location: formatLocation(path, relationship.StartLine, relationship.EndLine, relationship.Column),
		})
	}
	return rows, nil
}

func queryRoots(ctx context.Context, database *gorm.DB, snapshotID string) (map[uuid.UUID]string, error) {
	var roots []storage.Root
	if err := database.WithContext(ctx).Where("snapshot_id = ?", snapshotID).Find(&roots).Error; err != nil {
		return nil, fmt.Errorf("load query roots: %w", err)
	}
	result := make(map[uuid.UUID]string, len(roots))
	for _, root := range roots {
		result[root.ID] = root.RootKey
	}
	return result, nil
}

func queryLocations(ctx context.Context, database *gorm.DB, nodes []storage.Node) (map[uuid.UUID]string, error) {
	result := make(map[uuid.UUID]string, len(nodes))
	if len(nodes) == 0 {
		return result, nil
	}
	ids := make([]uuid.UUID, len(nodes))
	for i := range nodes {
		ids[i] = nodes[i].ID
	}
	var rows []nodeLocationRow
	err := database.WithContext(ctx).Table("uir_node_locations AS location").
		Select("location.node_id, source.display_path, location.start_line, location.end_line, location.column").
		Joins("JOIN uir_sources AS source ON source.id = location.source_id").
		Where("location.node_id IN ? AND location.is_primary = ?", ids, true).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load query node locations: %w", err)
	}
	for _, row := range rows {
		result[row.NodeID] = formatLocation(row.DisplayPath, row.StartLine, row.EndLine, row.Column)
	}
	return result, nil
}

func formatLocation(path string, start, end, column *int) string {
	location := path
	if start == nil {
		return location
	}
	location += fmt.Sprintf(":%d", *start)
	if end != nil && *end != *start {
		location += fmt.Sprintf("-%d", *end)
	}
	if column != nil {
		location += fmt.Sprintf(":%d", *column)
	}
	return location
}
