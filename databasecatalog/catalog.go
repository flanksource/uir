// Package databasecatalog lowers read-only SQL catalogue metadata into UIR snapshots.
package databasecatalog

import (
	"crypto/sha256"
	"fmt"
	"strings"

	sqlinspect "github.com/flanksource/commons-db/inspect/sql"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer"
)

// Documents returns one stable document per schema, including an empty schema.
// A truncated catalogue is refused because publishing it would turn omitted
// objects into false deletions in the next diff.
func Documents(catalog sqlinspect.Catalog) ([]indexer.NodeDocument, error) {
	if catalog.Truncated {
		return nil, fmt.Errorf("catalogue is truncated: %s", catalog.TruncateReason)
	}
	if len(catalog.Schemas) == 0 {
		return nil, fmt.Errorf("catalogue for %q has no schemas", catalog.Database)
	}
	documents := make([]indexer.NodeDocument, 0, len(catalog.Schemas))
	for _, schema := range catalog.Schemas {
		if strings.TrimSpace(schema.Name) == "" {
			return nil, fmt.Errorf("catalogue has an unnamed schema")
		}
		key := fmt.Sprintf("schemas/%x.sql", sha256.Sum256([]byte(schema.Name)))
		document := indexer.NodeDocument{PathKey: key, PackagePath: schema.Name, Nodes: []uir.Node{}}
		for _, relation := range schema.Relations {
			document.Nodes = append(document.Nodes, relationNode(schema.Name, relation))
		}
		for _, routine := range schema.Routines {
			document.Nodes = append(document.Nodes, routineNode(schema.Name, routine))
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func relationNode(schema string, relation sqlinspect.Relation) uir.RecordTable {
	builder := uir.NewTable(relation.Name, uir.Identifier{Package: schema})
	columns := make([]uir.RecordColumn, 0, len(relation.Columns))
	for _, item := range relation.Columns {
		column := uir.NewColumn(item.Name, item.DataType, fieldType(item.DataType))
		column.Ordinal, column.PrimaryKey, column.AutoIncrement, column.Unique = item.Ordinal, item.PrimaryKey, item.Identity, item.Unique
		column.MaxLength, column.Precision, column.Scale, column.Enum = item.MaxLength, item.NumericPrecision, item.NumericScale, item.EnumValues
		if item.Nullable != nil {
			column.Nullable = *item.Nullable
		}
		if item.Default != nil {
			column.DefaultValue = &uir.TypedValue{Str: item.Default}
		}
		if item.Comment != nil {
			column.Label = *item.Comment
		}
		columns = append(columns, column)
	}
	builder.WithColumns(columns...)
	for _, item := range relation.Indexes {
		index := uir.NewIndex(item.Name, item.Columns...)
		index.Unique, index.Primary, index.IndexType, index.Condition = item.Unique, item.Primary, item.Type, item.Filter
		builder.WithIndexes(index)
	}
	for _, item := range relation.ForeignKeys {
		key := uir.NewForeignKey(item.Name, item.Columns, item.ReferencedTable, item.ReferencedColumns)
		key.ReferencedSchema = item.ReferencedSchema
		builder.WithForeignKeys(key)
	}
	table := builder.Build()
	if relation.Type == "view" {
		table.RecordType = uir.RecordTypeView
		if relation.ViewDef != "" {
			body := uir.NewBlock().WithStatement(uir.RawStmt{Source: relation.ViewDef, Language: "sql"}).Build()
			table.Definition = &body
		}
	}
	for _, item := range relation.Triggers {
		trigger := uir.NewMethod(item.Name, uir.Identifier{Package: schema, Type: relation.Name}).Build()
		trigger.Properties = map[string]any{uir.TriggerEventProperty: item.Event, uir.TriggerTimingProperty: item.Timing, "disabled": item.Disabled}
		if item.SQL != "" {
			body := uir.NewBlock().WithStatement(uir.RawStmt{Source: item.SQL, Language: "sql"}).Build()
			trigger.Body = &body
		}
		table.Triggers = append(table.Triggers, trigger)
	}
	return table
}

func routineNode(schema string, routine sqlinspect.Routine) uir.MethodNode {
	builder := uir.NewMethod(routine.Name, uir.Identifier{Package: schema, NodeType: uir.NodeTypeFunction})
	for _, item := range routine.Parameters {
		field := uir.Field(item.Name, fieldType(item.DataType))
		field.SQLType = item.DataType
		builder.WithParam(field)
	}
	method := builder.Build()
	method.Properties = map[string]any{"routine_type": routine.Type, "return_type": routine.ReturnType}
	if routine.SQL != "" {
		body := uir.NewBlock().WithStatement(uir.RawStmt{Source: routine.SQL, Language: "sql"}).Build()
		method.Body = &body
	}
	return method
}

func fieldType(sqlType string) uir.RecordFieldType {
	typ := strings.ToLower(sqlType)
	switch {
	case strings.Contains(typ, "bool"), typ == "bit":
		return uir.RecordFieldTypeBoolean
	case strings.Contains(typ, "int"), typ == "serial":
		return uir.RecordFieldTypeInt
	case strings.Contains(typ, "decimal"), strings.Contains(typ, "numeric"), strings.Contains(typ, "float"), strings.Contains(typ, "double"), typ == "real":
		return uir.RecordFieldTypeFloat
	case strings.Contains(typ, "date"), strings.Contains(typ, "time"):
		return uir.RecordFieldTypeDate
	case strings.Contains(typ, "json"):
		return uir.RecordFieldTypeJSON
	case strings.Contains(typ, "uuid"), strings.Contains(typ, "uniqueidentifier"):
		return uir.RecordFieldTypeUUID
	default:
		return uir.RecordFieldTypeString
	}
}
