package migrate

import (
	"fmt"
	"slices"
	"strings"

	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/sqlglot-go/sqlglot"
	"github.com/flanksource/uir"
)

// Enrich replaces database facts with the complete inspected catalogue, while
// retaining source locations, view output columns and routine signatures.
func Enrich(document uir.UIR, schema model.Schema, scopes []string) (uir.UIR, error) {
	out := uir.UIR{SQLCatalog: &uir.SQLCatalog{Version: CatalogVersion, Dialect: "tsql", Schemas: slices.Clone(scopes), Complete: true}}
	for _, table := range schema.Tables {
		t, err := importTable(table)
		if err != nil {
			return out, err
		}
		for _, old := range document.Tables {
			if old.Package == t.Package && old.Type == t.Type {
				t.SourceCode, t.Description = old.SourceCode, old.Description
			}
		}
		out.Tables = append(out.Tables, t)
	}
	for _, d := range schema.Definitions {
		if d.Kind == "TRIGGER" {
			continue
		}
		if d.Kind == "VIEW" {
			t := uir.NewTable(d.Name.Name, uir.Identifier{Package: d.Name.Schema}).Build()
			for _, old := range document.Tables {
				if old.Package == t.Package && old.Type == t.Type {
					t = old
				}
			}
			t.RecordType, t.Content, t.SQLDependencies, t.Triggers = uir.RecordTypeView, new(d.SQL), importDependencies(d.Dependencies), nil
			out.Tables = append(out.Tables, t)
		} else {
			fn := importMethod(d)
			for _, old := range document.Functions {
				if old.Package == fn.Package && old.Method == fn.Method {
					fn.Params, fn.Returns, fn.Location = old.Params, old.Returns, old.Location
				}
			}
			out.Functions = append(out.Functions, fn)
		}
	}
	for _, d := range schema.Definitions {
		if d.Kind != "TRIGGER" {
			continue
		}
		if d.Parent == nil {
			return out, fmt.Errorf("trigger %v has no parent", d.Name)
		}
		at := slices.IndexFunc(out.Tables, func(t uir.RecordTable) bool { return t.Package == d.Parent.Schema && t.Type == d.Parent.Name })
		if at < 0 {
			return out, fmt.Errorf("trigger %v parent %v is missing", d.Name, *d.Parent)
		}
		fn := importMethod(d)
		fn.Type = d.Parent.Name
		fn.Properties[uir.TriggerDisabledProperty] = d.Disabled
		out.Tables[at].Triggers = append(out.Tables[at].Triggers, fn)
	}
	if _, err := Schema(out); err != nil {
		return out, err
	}
	return out, nil
}

func importTable(input model.Table) (uir.RecordTable, error) {
	b := uir.NewTable(input.Name.Name, uir.Identifier{Package: input.Name.Schema})
	var primary []string
	for _, idx := range input.Indexes {
		i := uir.NewIndex(idx.Name)
		for _, c := range idx.Columns {
			i.Columns = append(i.Columns, c.Name)
			if c.Descending {
				i.Descending = append(i.Descending, c.Name)
			}
		}
		i.Included, i.Unique, i.Primary, i.Clustered, i.Constraint, i.Condition = slices.Clone(idx.Included), idx.Unique, idx.Primary, idx.Clustered, idx.Constraint, idx.Filter
		if idx.Primary {
			primary = i.Columns
		}
		b.WithIndexes(i)
	}
	for _, col := range input.Columns {
		c, err := importColumn(col)
		if err != nil {
			return uir.RecordTable{}, fmt.Errorf("column %v.%s: %w", input.Name, col.Name, err)
		}
		c.PrimaryKey = slices.Contains(primary, col.Name)
		b.WithColumns(c)
	}
	for _, key := range input.ForeignKeys {
		f := uir.NewForeignKey(key.Name, slices.Clone(key.Columns), key.References.Name, slices.Clone(key.ReferencedColumns))
		f.ReferencedSchema, f.OnDelete, f.OnUpdate = key.References.Schema, key.OnDelete, key.OnUpdate
		b.WithForeignKeys(f)
	}
	out := b.Build()
	out.PrimaryKey = slices.Clone(primary)
	for _, c := range input.Checks {
		out.Checks = append(out.Checks, uir.SQLCheck{Name: c.Name, Expression: c.Expression})
	}
	return out, nil
}

func importColumn(input model.Column) (uir.RecordColumn, error) {
	expr, err := sqlglot.ParseDataType(input.Type, "tsql")
	if err != nil {
		return uir.RecordColumn{}, err
	}
	spec, err := sqlglot.DataTypeSpecOf(expr, "tsql")
	if err != nil {
		return uir.RecordColumn{}, err
	}
	base, _, _ := strings.Cut(strings.ToLower(input.Type), "(")
	base = strings.TrimSpace(base)
	c := uir.NewColumn(input.Name, base, portableType(base))
	c.Nullable, c.Collation = input.Nullable, input.Collation
	c.MaxLength, c.Precision, c.Scale = spec.Length, spec.Precision, spec.Scale
	if spec.Max {
		c.MaxLength = new(-1)
	}
	if input.Identity != nil {
		c.AutoIncrement, c.Identity = true, &uir.SQLIdentity{Seed: input.Identity.Seed, Increment: input.Identity.Increment}
	}
	if input.Default != nil {
		c.SQLDefault = &uir.SQLDefault{Name: input.Default.Name, Expression: input.Default.Expression}
	}
	return c, nil
}

func portableType(typ string) uir.RecordFieldType {
	switch typ {
	case "int", "bigint", "smallint", "tinyint":
		return uir.RecordFieldTypeInt
	case "decimal", "numeric", "float", "real", "money", "smallmoney":
		return uir.RecordFieldTypeFloat
	case "bit":
		return uir.RecordFieldTypeBoolean
	case "uniqueidentifier":
		return uir.RecordFieldTypeUUID
	case "date", "datetime", "datetime2", "datetimeoffset", "smalldatetime", "time":
		return uir.RecordFieldTypeDate
	case "char", "varchar", "nchar", "nvarchar", "text", "ntext":
		return uir.RecordFieldTypeString
	case "binary", "varbinary", "image", "rowversion", "timestamp", "xml", "sql_variant":
		return uir.RecordFieldTypeText
	default:
		return ""
	}
}

func importMethod(d model.Definition) uir.MethodNode {
	fn := uir.NewMethod(d.Name.Name, uir.Identifier{Package: d.Name.Schema}).Build()
	fn.Content, fn.Language, fn.SQLDependencies = new(d.SQL), new("sql"), importDependencies(d.Dependencies)
	fn.Properties = map[string]any{"kind": strings.ToLower(d.Kind)}
	return fn
}

func importDependencies(input []model.Name) []uir.SQLDependency {
	input = slices.Clone(input)
	slices.SortFunc(input, compareNames)
	var out []uir.SQLDependency
	for _, name := range input {
		out = append(out, uir.SQLDependency{Schema: name.Schema, Name: name.Name})
	}
	return out
}
