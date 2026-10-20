package query

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

func cloneProperties(properties map[string]any) map[string]any {
	if properties == nil {
		return nil
	}
	encoded, err := json.Marshal(properties)
	if err != nil {
		panic(fmt.Sprintf("invalid decoded properties: %s", err))
	}
	var cloned map[string]any
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

func sqlSourceKinds(source *sqlSource, kinds symbolhandle.Kinds) ([]string, error) {
	if source.selector != nil {
		selected, err := selectorKinds(*source.selector, kinds)
		if err != nil || len(selected) > 0 {
			return selected, err
		}
		var names []string
		for _, kind := range kinds.All() {
			names = append(names, kind.Name)
		}
		return names, nil
	}
	switch source.function.Name {
	case "definitions":
		return sqlSourceKinds(source.arguments[0], kinds)
	case "methods":
		return []string{"method"}, nil
	case "implementers", "inheritors":
		return []string{"type"}, nil
	case "callers", "callees":
		return kinds.InCategory(symbolhandle.CategoryCallable), nil
	default:
		names := []string{}
		for _, kind := range kinds.All() {
			names = append(names, kind.Name)
		}
		return names, nil
	}
}

func sqlPropertyFields(source *sqlSource, kinds symbolhandle.Kinds, catalog storage.DomainCatalog) (map[string]string, error) {
	if source.function.Name == "files" {
		return maps.Clone(sqlFileFields), nil
	}
	fields := maps.Clone(sqlFields)
	fields["symbol.props"] = "object"
	fields["relationship.props"] = "object"
	selected, err := sqlSourceKinds(source, kinds)
	if err != nil {
		return nil, err
	}
	for name, schema := range catalog.Symbols {
		if !slices.Contains(selected, name) {
			continue
		}
		properties, err := storage.PropertyFields(schema)
		if err != nil {
			return nil, err
		}
		for property, kind := range properties {
			field := "symbol.props." + property
			if existing, ok := fields[field]; ok && existing != kind {
				kind = "mixed"
			}
			fields[field] = kind
		}
	}
	for _, kind := range catalog.Relationships {
		if len(source.options.via) > 0 && !slices.Contains(source.options.via, kind.Name) {
			continue
		}
		properties, err := storage.PropertyFields(kind.Schema)
		if err != nil {
			return nil, err
		}
		for property, kind := range properties {
			field := "relationship.props." + property
			if existing, ok := fields[field]; ok && existing != kind {
				kind = "mixed"
			}
			fields[field] = kind
		}
	}
	return fields, nil
}

func (index *compactIndex) sqlRecordProperties(ctx context.Context, records []sqlRecord) error {
	ids := map[string]bool{}
	for _, record := range records {
		if id, ok := record.values["symbol.id"].(string); ok && id != "" {
			ids[id] = true
		}
	}
	declarations, err := index.declarations(ctx, sortedKeys(ids), "definition")
	if err != nil {
		return err
	}
	byID := map[string][]ModuleMatch{}
	for _, declaration := range declarations {
		byID[declaration.SymbolID] = append(byID[declaration.SymbolID], declaration)
	}
	for i := range records {
		record := &records[i]
		id, _ := record.values["symbol.id"].(string)
		var selected *ModuleMatch
		for _, declaration := range byID[id] {
			if declaration.SnapshotID == record.match.SnapshotID {
				selected = &declaration
				break
			}
		}
		if selected == nil && len(byID[id]) == 1 {
			selected = &byID[id][0]
		}
		if selected == nil && len(byID[id]) > 1 {
			return fmt.Errorf("symbol %s has several declarations in the selected snapshots; select a location or snapshot", id)
		}
		if selected != nil {
			record.values["symbol.props"] = selected.Props
			record.match.Props = cloneProperties(selected.Props)
			flattenSQLProperties(record.values, "symbol.props", selected.Props)
		}
		if relationship := record.match.Relationship; relationship != nil {
			record.values["relationship.kind"] = relationship.Kind
			record.values["relationship.resolution"] = relationship.Resolution
			record.values["relationship.props"] = relationship.Props
			flattenSQLProperties(record.values, "relationship.props", relationship.Props)
		}
	}
	return nil
}

func flattenSQLProperties(fields map[string]any, prefix string, properties map[string]any) {
	for name, value := range properties {
		field := prefix + "." + name
		fields[field] = value
		if object, ok := value.(map[string]any); ok {
			flattenSQLProperties(fields, field, object)
		}
	}
}
