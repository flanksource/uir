package migrate

import (
	"fmt"
	"slices"
	"strings"

	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/sqlglot-go/migrate/sqlserver"
	"github.com/flanksource/uir"
)

func Schema(document uir.UIR) (model.Schema, error) {
	catalog := document.SQLCatalog
	if catalog == nil || !catalog.Complete || catalog.Version != CatalogVersion || catalog.Dialect != "tsql" || len(catalog.Schemas) == 0 {
		return model.Schema{}, fmt.Errorf("migration requires a complete SQL Server catalogue at version %d", CatalogVersion)
	}
	seen := map[string]bool{}
	for _, scope := range catalog.Schemas {
		if strings.TrimSpace(scope) == "" || seen[scope] {
			return model.Schema{}, fmt.Errorf("catalogue scope contains an empty or duplicate schema %q", scope)
		}
		seen[scope] = true
	}
	if len(document.Modules)+len(document.Packages)+len(document.Types)+len(document.Records)+len(document.Endpoints)+len(document.Warnings) != 0 {
		return model.Schema{}, fmt.Errorf("migration catalogue contains unsupported or discarded UIR nodes")
	}
	var out model.Schema
	for _, table := range document.Tables {
		name := model.Name{Schema: table.Package, Name: table.Type}
		switch table.RecordType {
		case uir.RecordTypeTable:
			t, err := tableSchema(table)
			if err != nil {
				return out, fmt.Errorf("table %s.%s: %w", name.Schema, name.Name, err)
			}
			out.Tables = append(out.Tables, t)
		case uir.RecordTypeView:
			if table.Content == nil {
				return out, fmt.Errorf("view %v has no definition", name)
			}
			out.Definitions = append(out.Definitions, model.Definition{Name: name, Kind: "VIEW", SQL: *table.Content, Dependencies: dependencies(table.SQLDependencies)})
		default:
			return out, fmt.Errorf("unsupported record type %q on %v", table.RecordType, name)
		}
		for _, trigger := range table.Triggers {
			definition, err := methodDefinition(trigger, "TRIGGER")
			if err != nil {
				return out, err
			}
			definition.Parent = &name
			disabled, ok := trigger.Properties[uir.TriggerDisabledProperty].(bool)
			if !ok {
				return out, fmt.Errorf("trigger %v has no disabled state", definition.Name)
			}
			definition.Disabled = disabled
			out.Definitions = append(out.Definitions, definition)
		}
	}
	for _, routine := range document.Functions {
		kind, ok := routine.Properties["kind"].(string)
		if !ok || (kind != "function" && kind != "procedure") {
			return out, fmt.Errorf("routine %s.%s has unsupported kind %q", routine.Package, routine.Method, kind)
		}
		definition, err := methodDefinition(routine, strings.ToUpper(kind))
		if err != nil {
			return out, err
		}
		out.Definitions = append(out.Definitions, definition)
	}
	for _, name := range objectNames(out) {
		if !slices.Contains(catalog.Schemas, name.Schema) {
			return out, fmt.Errorf("object %v is outside catalogue coverage", name)
		}
	}
	slices.SortFunc(out.Tables, func(a, b model.Table) int { return compareNames(a.Name, b.Name) })
	slices.SortFunc(out.Definitions, func(a, b model.Definition) int { return compareNames(a.Name, b.Name) })
	if _, err := sqlserver.Plan(model.Request{Current: out, Desired: out}); err != nil {
		return out, err
	}
	return out, nil
}

func compareNames(a, b model.Name) int {
	if a.Schema != b.Schema {
		return strings.Compare(a.Schema, b.Schema)
	}
	return strings.Compare(a.Name, b.Name)
}

func tableSchema(table uir.RecordTable) (model.Table, error) {
	out := model.Table{Name: model.Name{Schema: table.Package, Name: table.Type}}
	for _, column := range table.Columns {
		c, err := columnSchema(column)
		if err != nil {
			return out, fmt.Errorf("column %s: %w", column.Field, err)
		}
		out.Columns = append(out.Columns, c)
	}
	var primary []string
	for _, index := range table.Indexes {
		i := model.Index{Name: index.Field, Unique: index.Unique, Primary: index.Primary, Clustered: index.Clustered, Constraint: index.Constraint, Included: slices.Clone(index.Included), Filter: index.Condition}
		if index.IndexType != "" && index.IndexType != "CLUSTERED" && index.IndexType != "NONCLUSTERED" {
			return out, fmt.Errorf("unsupported index type %q", index.IndexType)
		}
		for _, name := range index.Columns {
			i.Columns = append(i.Columns, model.IndexColumn{Name: name, Descending: slices.Contains(index.Descending, name)})
		}
		for _, name := range index.Descending {
			if !slices.Contains(index.Columns, name) {
				return out, fmt.Errorf("index %s has unknown descending key %s", index.Field, name)
			}
		}
		if index.Primary {
			primary = index.Columns
		}
		out.Indexes = append(out.Indexes, i)
	}
	if !slices.Equal(primary, table.PrimaryKey) {
		return out, fmt.Errorf("primary key columns disagree with the named primary index")
	}
	for _, key := range table.ForeignKeys {
		out.ForeignKeys = append(out.ForeignKeys, model.ForeignKey{Name: key.Field, Columns: slices.Clone(key.Columns), References: model.Name{Schema: key.ReferencedSchema, Name: key.ReferencedTable}, ReferencedColumns: slices.Clone(key.ReferencedColumns), OnDelete: key.OnDelete, OnUpdate: key.OnUpdate})
	}
	for _, check := range table.Checks {
		out.Checks = append(out.Checks, model.Check{Name: check.Name, Expression: check.Expression})
	}
	return out, nil
}

func columnSchema(column uir.RecordColumn) (model.Column, error) {
	typ, err := columnType(column)
	if err != nil {
		return model.Column{}, err
	}
	out := model.Column{Name: column.Field, Type: typ, Nullable: column.Nullable, Collation: column.Collation}
	if column.AutoIncrement != (column.Identity != nil) {
		return out, fmt.Errorf("identity requires its seed and increment")
	}
	if column.Identity != nil {
		out.Identity = &model.Identity{Seed: column.Identity.Seed, Increment: column.Identity.Increment}
	}
	if column.DefaultValue != nil && column.SQLDefault == nil {
		return out, fmt.Errorf("default requires the original SQL expression and constraint name")
	}
	if column.SQLDefault != nil {
		if column.SQLDefault.Name == "" {
			return out, fmt.Errorf("default constraint name is missing")
		}
		out.Default = &model.Default{Name: column.SQLDefault.Name, Expression: column.SQLDefault.Expression}
	}
	return out, nil
}

func columnType(column uir.RecordColumn) (string, error) {
	typ := strings.ToLower(strings.TrimSpace(column.SQLType))
	if strings.Contains(typ, "(") {
		return typ, nil
	}
	switch typ {
	case "char", "varchar", "nchar", "nvarchar", "binary", "varbinary":
		if column.MaxLength == nil {
			return "", fmt.Errorf("%s requires a catalogue width", typ)
		}
		if *column.MaxLength == -1 {
			return typ + "(max)", nil
		}
		return fmt.Sprintf("%s(%d)", typ, *column.MaxLength), nil
	case "decimal", "numeric":
		if column.Precision == nil || column.Scale == nil {
			return "", fmt.Errorf("%s requires precision and scale", typ)
		}
		return fmt.Sprintf("%s(%d,%d)", typ, *column.Precision, *column.Scale), nil
	case "float":
		if column.Precision != nil {
			return fmt.Sprintf("float(%d)", *column.Precision), nil
		}
	case "datetime2", "datetimeoffset", "time":
		if column.Scale == nil {
			return "", fmt.Errorf("%s requires fractional-second scale", typ)
		}
		return fmt.Sprintf("%s(%d)", typ, *column.Scale), nil
	}
	if typ == "" {
		return "", fmt.Errorf("SQL type is missing")
	}
	return typ, nil
}

func methodDefinition(method uir.MethodNode, kind string) (model.Definition, error) {
	out := model.Definition{Name: model.Name{Schema: method.Package, Name: method.Method}, Kind: kind, Dependencies: dependencies(method.SQLDependencies)}
	if method.Content == nil || strings.TrimSpace(*method.Content) == "" {
		return out, fmt.Errorf("%s %v has no full SQL definition", kind, out.Name)
	}
	out.SQL = *method.Content
	return out, nil
}

func dependencies(input []uir.SQLDependency) []model.Name {
	var out []model.Name
	for _, name := range input {
		out = append(out, model.Name{Schema: name.Schema, Name: name.Name})
	}
	return out
}

func objectNames(schema model.Schema) []model.Name {
	var out []model.Name
	for _, t := range schema.Tables {
		out = append(out, t.Name)
	}
	for _, d := range schema.Definitions {
		out = append(out, d.Name)
	}
	return out
}

func sameScopes(a, b *uir.SQLCatalog) error {
	x, y := slices.Clone(a.Schemas), slices.Clone(b.Schemas)
	slices.Sort(x)
	slices.Sort(y)
	if !slices.Equal(x, y) {
		return fmt.Errorf("reference and target catalogue scopes differ")
	}
	return nil
}
