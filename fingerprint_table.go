package uir

import (
	"slices"
	"strconv"
	"strings"
)

// tableLayers: the public layer is the record type and the output columns as a
// set; the private layer adds column order and private column facts, the primary
// key, indexes, foreign keys and trigger declarations; the content layer is the
// view definition and the trigger bodies. RowCount is in no layer.
func tableLayers(t RecordTable) layers {
	var c composition
	c.add(NodesOf(t.Columns), orderedPrivately, false)
	if len(t.PrimaryKey) > 0 {
		c.private = append(c.private, "primary key ("+strings.Join(t.PrimaryKey, ", ")+")")
	}
	c.add(NodesOf(t.Indexes), unordered, true)
	c.add(NodesOf(t.ForeignKeys), unordered, true)
	c.add(NodesOf(t.Triggers), unordered, true)
	if dependencies := sqlDependenciesText(t.SQLDependencies); dependencies != "" {
		c.private = append(c.private, dependencies)
	}
	checks := slices.Clone(t.Checks)
	slices.SortFunc(checks, func(a, b SQLCheck) int { return strings.Compare(a.Name, b.Name) })
	if len(checks) > 0 {
		c.private = append(c.private, "checks="+canonicalJSON(checks))
	}
	definition := ""
	if t.Definition != nil {
		definition = t.Definition.Hash()
	}
	facts := ""
	if t.RecordType != "" {
		facts = " " + string(t.RecordType)
	}
	return c.layers(ShapeKind(t), facts, definition)
}

// columnLayers: Ordinal is not hashed — a column's position is its table's
// private fact, not the column's.
func columnLayers(col RecordColumn) layers {
	public := sqlTypeText(col.SQLType, col.FieldType)
	if col.Nullable {
		public += " null"
	} else {
		public += " not null"
	}
	public += flagText(col.AutoIncrement, "identity")
	private := public + flagText(col.PrimaryKey, "pk") + flagText(col.Unique, "unique") +
		intText("length", col.MaxLength) + intText("precision", col.Precision) + intText("scale", col.Scale)
	if col.DefaultValue != nil {
		private += " default=" + canonicalJSON(*col.DefaultValue)
	}
	if col.Identity != nil {
		private += " identity=" + canonicalJSON(col.Identity)
	}
	if col.SQLDefault != nil {
		private += " sql-default=" + canonicalJSON(col.SQLDefault)
	}
	if col.Collation != "" {
		private += " collation=" + strconv.Quote(col.Collation)
	}
	if len(col.Enum) > 0 {
		private += " enum=" + canonicalJSON(col.Enum)
	}
	if col.Label != "" {
		private += " label=" + strconv.Quote(col.Label)
	}
	return leafLayers(ShapeKind(col), public, private, "")
}

// indexLayers: an index is a private member, so its public layer is its kind.
func indexLayers(i RecordIndex) layers {
	private := " (" + strings.Join(i.Columns, ", ") + ")"
	if len(i.Included) > 0 {
		private += " include (" + strings.Join(i.Included, ", ") + ")"
	}
	private += flagText(i.Unique, "unique") + flagText(i.Primary, "primary") + flagText(i.Clustered, "clustered")
	private += flagText(i.Constraint, "constraint")
	if len(i.Descending) > 0 {
		descending := slices.Clone(i.Descending)
		slices.Sort(descending)
		private += " descending=" + canonicalJSON(descending)
	}
	if i.IndexType != "" {
		private += " using " + i.IndexType
	}
	if i.Condition != "" {
		private += " where " + i.Condition
	}
	return leafLayers(ShapeKind(i), "", private, "")
}

func sqlDependenciesText(input []SQLDependency) string {
	if len(input) == 0 {
		return ""
	}
	dependencies := slices.Clone(input)
	slices.SortFunc(dependencies, func(a, b SQLDependency) int {
		if a.Schema != b.Schema {
			return strings.Compare(a.Schema, b.Schema)
		}
		return strings.Compare(a.Name, b.Name)
	})
	return " sql-dependencies=" + canonicalJSON(dependencies)
}

// foreignKeyLayers: a foreign key is a private member, like an index.
func foreignKeyLayers(f RecordForeignKey) layers {
	target := f.ReferencedTable
	if f.ReferencedSchema != "" {
		target = f.ReferencedSchema + "." + target
	}
	private := " (" + strings.Join(f.Columns, ", ") + ") references " + target +
		" (" + strings.Join(f.ReferencedColumns, ", ") + ")"
	if f.OnDelete != "" {
		private += " on delete " + f.OnDelete
	}
	if f.OnUpdate != "" {
		private += " on update " + f.OnUpdate
	}
	return leafLayers(ShapeKind(f), "", private, "")
}

func intText(name string, value *int) string {
	if value == nil {
		return ""
	}
	return " " + name + "=" + strconv.Itoa(*value)
}
