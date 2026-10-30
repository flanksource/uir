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
// retaining what only the extractor knows: source locations, descriptions,
// annotations and row counts of tables and their columns, view output columns
// and routine signatures. A fact the catalogue holds always comes from it.
func Enrich(document uir.UIR, schema model.Schema, scopes []string) (uir.UIR, error) {
	out := uir.UIR{SQLCatalog: &uir.SQLCatalog{Version: CatalogVersion, Dialect: "tsql", Schemas: slices.Clone(scopes), Complete: true}}
	for _, tableType := range schema.TableTypes {
		t, err := importTableType(tableType)
		if err != nil {
			return out, err
		}
		out.SQLCatalog.TableTypes = append(out.SQLCatalog.TableTypes, t)
	}
	for _, sequence := range schema.Sequences {
		out.SQLCatalog.Sequences = append(out.SQLCatalog.Sequences, importSequence(sequence))
	}
	for _, table := range schema.Tables {
		t, err := importTable(table)
		if err != nil {
			return out, err
		}
		if at := slices.IndexFunc(document.Tables, func(old uir.RecordTable) bool { return old.Package == t.Package && old.Type == t.Type }); at >= 0 {
			keepExtracted(&t, document.Tables[at])
		}
		out.Tables = append(out.Tables, t)
	}
	for _, d := range schema.Definitions {
		switch d.Kind {
		case "TRIGGER":
		case "VIEW":
			out.Tables = append(out.Tables, importView(document, d))
		default:
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

// keepExtracted copies onto an imported table the facts the catalogue does not
// hold. A column's extracted default value is kept only while the catalogue
// still declares a default for it.
func keepExtracted(table *uir.RecordTable, old uir.RecordTable) {
	table.Metadata, table.SourceCode, table.Description, table.RowCount = old.Metadata, old.SourceCode, old.Description, old.RowCount
	for i := range table.Columns {
		c := &table.Columns[i]
		at := slices.IndexFunc(old.Columns, func(o uir.RecordColumn) bool { return o.Field == c.Field })
		if at < 0 {
			continue
		}
		o := old.Columns[at]
		c.Metadata, c.SourceCode, c.Label, c.Enum = o.Metadata, o.SourceCode, o.Label, o.Enum
		if c.SQLDefault != nil {
			c.DefaultValue = o.DefaultValue
		}
	}
}

func importView(document uir.UIR, d model.Definition) uir.RecordTable {
	t := uir.NewTable(d.Name.Name, uir.Identifier{Package: d.Name.Schema}).Build()
	for _, old := range document.Tables {
		if old.Package == t.Package && old.Type == t.Type {
			t = old
		}
	}
	t.RecordType, t.Content, t.SQLDependencies, t.Triggers = uir.RecordTypeView, new(d.SQL), importDependencies(d.Dependencies), nil
	t.SQLRuntimeDependencies, t.SQLQuotedIdentifierOff = importRuntimeDependencies(d.RuntimeDependencies), d.QuotedIdentifierOff
	return t
}

func importTable(input model.Table) (uir.RecordTable, error) {
	b := uir.NewTable(input.Name.Name, uir.Identifier{Package: input.Name.Schema})
	var primary []string
	for _, idx := range input.Indexes {
		i := importIndex(idx)
		if idx.Primary {
			primary = i.Columns
		}
		b.WithIndexes(i)
	}
	for at, col := range input.Columns {
		c, err := importColumn(col)
		if err != nil {
			return uir.RecordTable{}, fmt.Errorf("column %v.%s: %w", input.Name, col.Name, err)
		}
		c.PrimaryKey, c.Ordinal = slices.Contains(primary, col.Name), at+1
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

func importIndex(input model.Index) uir.RecordIndex {
	i := uir.NewIndex(input.Name)
	for _, c := range input.Columns {
		i.Columns = append(i.Columns, c.Name)
		if c.Descending {
			i.Descending = append(i.Descending, c.Name)
		}
	}
	i.Included, i.Unique, i.Primary, i.Clustered, i.Constraint, i.Condition = slices.Clone(input.Included), input.Unique, input.Primary, input.Clustered, input.Constraint, input.Filter
	i.FillFactor = input.FillFactor
	return i
}

func importTableType(input model.TableType) (uir.SQLTableType, error) {
	out := uir.SQLTableType{Schema: input.Name.Schema, Name: input.Name.Name, Checks: slices.Clone(input.Checks)}
	for at, col := range input.Columns {
		c, err := importColumn(col)
		if err != nil {
			return out, fmt.Errorf("table type %v column %s: %w", input.Name, col.Name, err)
		}
		c.Ordinal = at + 1
		out.Columns = append(out.Columns, c)
	}
	for _, key := range input.Keys {
		out.Keys = append(out.Keys, importIndex(key))
	}
	return out, nil
}

func importSequence(s model.Sequence) uir.SQLSequence {
	return uir.SQLSequence{Schema: s.Name.Schema, Name: s.Name.Name, Type: s.Type, Start: s.Start, Increment: s.Increment,
		Minimum: s.Minimum, Maximum: s.Maximum, Cycle: s.Cycle, NoCache: s.NoCache, CacheSize: s.CacheSize}
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
	c.Nullable, c.Collation, c.RowGUID = input.Nullable, input.Collation, input.RowGUID
	c.MaxLength, c.Precision, c.Scale = spec.Length, spec.Precision, spec.Scale
	if spec.Max {
		c.MaxLength = new(-1)
	}
	if input.Identity != nil {
		c.AutoIncrement, c.Identity = true, &uir.SQLIdentity{Seed: input.Identity.Seed, Increment: input.Identity.Increment}
	}
	if input.Default != nil {
		c.SQLDefault = &uir.SQLDefault{Name: input.Default.Name, Expression: input.Default.Expression, SystemNamed: input.Default.SystemNamed}
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
	fn.SQLRuntimeDependencies, fn.SQLQuotedIdentifierOff = importRuntimeDependencies(d.RuntimeDependencies), d.QuotedIdentifierOff
	fn.Properties = map[string]any{"kind": strings.ToLower(d.Kind)}
	return fn
}

func importRuntimeDependencies(input []model.RuntimeDependency) []uir.SQLRuntimeDependency {
	var out []uir.SQLRuntimeDependency
	for _, d := range input {
		out = append(out, uir.SQLRuntimeDependency{Server: d.Server, Database: d.Database, Schema: d.Schema, Name: d.Name})
	}
	return out
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
