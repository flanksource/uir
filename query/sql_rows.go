package query

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/query/sqlsyntax"
)

var sqlFields = map[string]string{
	"symbol": "symbol", "symbol.id": "string", "symbol.kind": "string", "symbol.name": "string", "symbol.query_name": "string",
	"symbol.module": "string", "symbol.package": "string", "symbol.owner": "string", "symbol.visibility": "string",
	"path": "string", "line": "number", "column": "number", "package": "string", "module": "string", "role": "string",
	"dispatch": "boolean", "depth": "number", "snapshot": "string", "relationship.kind": "string", "relationship.resolution": "string",
}

var sqlWildcardFields = []string{"symbol", "symbol.id", "symbol.kind", "symbol.name", "symbol.query_name", "symbol.module", "symbol.package", "symbol.owner", "symbol.visibility", "symbol.props", "path", "line", "column", "package", "module", "role", "dispatch", "depth", "snapshot", "relationship.kind", "relationship.resolution", "relationship.props"}

type sqlRecord struct {
	match  ModuleMatch
	values map[string]any
}

type sqlRows struct {
	fields    map[string]string
	alias     string
	columns   []QueryColumn
	order     []*sqlsyntax.Ordering
	predicate sqlPredicate
}

func (rows *sqlRows) field(name string) (string, string, error) {
	field := name
	if rows.alias != "" && strings.HasPrefix(field, rows.alias+".") {
		field = "symbol.props." + strings.TrimPrefix(field, rows.alias+".")
	}
	kind, ok := rows.fields[field]
	if !ok {
		return "", "", fmt.Errorf("unknown SQL field %q", name)
	}
	if kind == "" || kind == "mixed" {
		return "", "", fmt.Errorf("field %q has no unambiguous schema type", name)
	}
	return field, kind, nil
}

func (rows *sqlRows) bind(query *sqlsyntax.Query) error {
	rows.alias = query.Alias
	columns := query.Projection.Columns
	if query.Projection.Wildcard {
		for _, field := range sqlWildcardFields {
			columns = append(columns, &sqlsyntax.Column{Field: field})
		}
	}
	names := map[string]bool{}
	for _, column := range columns {
		field, kind, err := rows.field(column.Field)
		if err != nil {
			return sqlError(column.Pos, "%s", err)
		}
		name := column.Alias
		if name == "" {
			name = column.Field
		}
		if names[name] {
			return sqlError(column.Pos, "projection name %q is repeated", name)
		}
		names[name] = true
		rows.columns = append(rows.columns, QueryColumn{Name: name, Field: field, Type: kind})
	}
	for _, order := range query.Order {
		field := order.Field
		for _, column := range rows.columns {
			if column.Name == field {
				field = column.Field
				break
			}
		}
		field, kind, err := rows.field(field)
		if err != nil {
			return sqlError(order.Pos, "%s", err)
		}
		if kind == "symbol" || kind == "object" || kind == "array" {
			return sqlError(order.Pos, "%s cannot be ordered", field)
		}
		rows.order = append(rows.order, &sqlsyntax.Ordering{Field: field, Direction: order.Direction})
	}
	return nil
}

func (index *compactIndex) sqlRecords(ctx context.Context, value compactValue, projectEnclosing bool) ([]sqlRecord, error) {
	ids := map[string]bool{}
	for _, match := range value.matches {
		id := match.SymbolID
		if projectEnclosing {
			id = match.EnclosingID
		}
		if id != "" {
			ids[id] = true
		}
	}
	symbols, err := index.symbolsByID(ctx, sortedKeys(ids))
	if err != nil {
		return nil, err
	}
	byID := map[string]ModuleSymbol{}
	for _, symbol := range symbols {
		byID[symbol.ID] = symbol
	}
	records := make([]sqlRecord, 0, len(value.matches))
	for _, match := range value.matches {
		id := match.SymbolID
		if projectEnclosing {
			id = match.EnclosingID
		}
		symbol := byID[id]
		if match.Relationship != nil && id != "" {
			match.Identifier = index.identifier(symbol)
			match.NodeKind = symbol.Kind
		}
		values := map[string]any{
			"symbol": symbol, "symbol.id": id, "symbol.kind": symbol.Kind, "symbol.name": symbol.Name, "symbol.query_name": symbol.QueryName,
			"symbol.module": symbol.ModuleKey, "symbol.package": symbol.PackagePath, "symbol.owner": symbol.Owner, "symbol.visibility": symbol.Visibility,
			"path": match.Path, "line": nil, "column": nil, "package": match.PackagePath, "module": match.RootKey, "role": match.Role,
			"dispatch": match.Dispatch, "depth": match.Depth, "snapshot": match.SnapshotID, "relationship.kind": nil, "relationship.resolution": nil,
		}
		if match.Line != nil {
			values["line"] = *match.Line
		}
		if match.Column != nil {
			values["column"] = *match.Column
		}
		if id == "" {
			for key := range values {
				if strings.HasPrefix(key, "symbol") {
					values[key] = nil
				}
			}
		}
		records = append(records, sqlRecord{match: match, values: values})
	}
	return records, nil
}

func (rows *sqlRows) project(records []sqlRecord, limit int, result *ModuleQueryResult) {
	if rows.predicate != nil {
		records = slices.DeleteFunc(records, func(record sqlRecord) bool { return rows.predicate(record) != sqlTrue })
	}
	slices.SortStableFunc(records, func(left, right sqlRecord) int {
		for _, order := range rows.order {
			compared := sqlCompareValues(left.values[order.Field], right.values[order.Field])
			if strings.EqualFold(order.Direction, "DESC") {
				compared = -compared
			}
			if compared != 0 {
				return compared
			}
		}
		return 0
	})
	result.Total = len(records)
	result.Columns = rows.columns
	result.Rows = make([]map[string]any, 0, min(limit, len(records)))
	result.Matches = make([]ModuleMatch, 0, min(limit, len(records)))
	for _, record := range records[:min(limit, len(records))] {
		projected := map[string]any{}
		for _, column := range rows.columns {
			projected[column.Name] = record.values[column.Field]
		}
		result.Rows = append(result.Rows, projected)
		result.Matches = append(result.Matches, record.match)
	}
}

func sqlCompareValues(left, right any) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}
	switch value := left.(type) {
	case string:
		return strings.Compare(value, right.(string))
	case int:
		if number, ok := right.(float64); ok {
			return cmp.Compare(float64(value), number)
		}
		return cmp.Compare(value, right.(int))
	case float64:
		if number, ok := right.(int); ok {
			return cmp.Compare(value, float64(number))
		}
		return cmp.Compare(value, right.(float64))
	case bool:
		if value == right.(bool) {
			return 0
		}
		if value {
			return 1
		}
		return -1
	default:
		panic(fmt.Sprintf("unorderable SQL value %T", left))
	}
}
