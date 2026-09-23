package uir

import (
	"strconv"
	"strings"

	"github.com/flanksource/clicky/api"
)

// RecordTable is a database table or view.
//
// It exists because ASTRecord cannot carry what a relational schema actually
// knows. A generic record has fields; a table has columns with a dialect type, an
// ordinal position and a nullability, an ordered primary key, indexes, and
// foreign keys pointing at other tables. Squeezing that into RecordField meant
// dropping most of it — the raw SQL type collapsed into a portable one, the
// primary key merged with auto-increment into ReadOnly, and indexes smuggled
// through a Properties map — so every consumer that needed the real thing went
// back to the database catalogue itself.
//
// Identity follows ASTRecord: Identifier.Package is the schema, Identifier.Type
// is the table name.
type RecordTable struct {
	nodeBase
	Description string `json:"description,omitempty"`
	// RecordTypeTable or RecordTypeView.
	RecordType  RecordType         `json:"recordType,omitempty"`
	Columns     []RecordColumn     `json:"columns,omitempty"`
	PrimaryKey  []string           `json:"primaryKey,omitempty"`
	Indexes     []RecordIndex      `json:"indexes,omitempty"`
	ForeignKeys []RecordForeignKey `json:"foreignKeys,omitempty"`
	// RowCount is the catalogue's estimate, nil when it was not asked for.
	RowCount *int64 `json:"rowCount,omitempty"`
	// Triggers and other rules defined against the table.
	Extends []ASTRecord `json:"extends,omitempty"`
}

// RecordColumn is one column of a RecordTable.
//
// SQLType and FieldType both survive on purpose: FieldType is the portable
// mapping a TypeScript or JSON-schema emitter wants, SQLType is the dialect's own
// spelling that a CONVERT() or a DDL round-trip needs. Identifier.Field holds the
// column name.
type RecordColumn struct {
	nodeBase
	Label string `json:"label,omitempty"`
	// SQLType is the dialect type verbatim, e.g. "uniqueidentifier",
	// "nvarchar", "datetime2".
	SQLType string `json:"sqlType,omitempty"`
	// FieldType is the portable mapping of SQLType.
	FieldType RecordFieldType `json:"fieldType,omitempty"`
	// Ordinal is the 1-based position in the table, as the catalogue reports it.
	Ordinal       int  `json:"ordinal,omitempty"`
	Nullable      bool `json:"nullable,omitempty"`
	PrimaryKey    bool `json:"primaryKey,omitempty"`
	AutoIncrement bool `json:"autoIncrement,omitempty"`
	Unique        bool `json:"unique,omitempty"`
	// MaxLength is the character width; -1 means unbounded ("max"/text).
	MaxLength    *int        `json:"maxLength,omitempty"`
	Precision    *int        `json:"precision,omitempty"`
	Scale        *int        `json:"scale,omitempty"`
	DefaultValue *TypedValue `json:"defaultValue,omitempty"`
	// Enum is the closed value set when the dialect declares one.
	Enum []string `json:"enum,omitempty"`
}

// RecordIndex is an index on a RecordTable. Identifier.Field holds the index
// name.
type RecordIndex struct {
	nodeBase
	// Columns are the key columns in index order — the order is what makes an
	// index usable for a given predicate, so it is not sorted.
	Columns   []string `json:"columns,omitempty"`
	Included  []string `json:"included,omitempty"`
	Unique    bool     `json:"unique,omitempty"`
	Primary   bool     `json:"primary,omitempty"`
	Clustered bool     `json:"clustered,omitempty"`
	// IndexType is the dialect's own kind, e.g. "BTREE", "CLUSTERED".
	IndexType string `json:"indexType,omitempty"`
	// Condition is the filter predicate of a partial/filtered index.
	Condition string `json:"condition,omitempty"`
}

// RecordForeignKey is a foreign-key constraint on a RecordTable, kept as
// structure rather than a formatted string so a consumer can follow it.
// Identifier.Field holds the constraint name.
type RecordForeignKey struct {
	nodeBase
	// Columns and ReferencedColumns are positionally paired.
	Columns           []string `json:"columns,omitempty"`
	ReferencedSchema  string   `json:"referencedSchema,omitempty"`
	ReferencedTable   string   `json:"referencedTable,omitempty"`
	ReferencedColumns []string `json:"referencedColumns,omitempty"`
	OnDelete          string   `json:"onDelete,omitempty"`
	OnUpdate          string   `json:"onUpdate,omitempty"`
}

func (t RecordTable) GetType() NodeType { return NodeTypeTable }

func (c RecordColumn) GetType() NodeType { return NodeTypeColumn }

func (i RecordIndex) GetType() NodeType { return NodeTypeIndex }

func (f RecordForeignKey) GetType() NodeType { return NodeTypeForeignKey }

func (t RecordTable) GetChildren() []Node {
	children := make([]Node, 0, len(t.Columns)+len(t.Indexes)+len(t.ForeignKeys)+len(t.Extends))
	for i := range t.Columns {
		children = append(children, t.Columns[i])
	}
	for i := range t.Indexes {
		children = append(children, t.Indexes[i])
	}
	for i := range t.ForeignKeys {
		children = append(children, t.ForeignKeys[i])
	}
	for i := range t.Extends {
		children = append(children, t.Extends[i])
	}
	return children
}

func (c RecordColumn) GetChildren() []Node { return nil }

func (i RecordIndex) GetChildren() []Node { return nil }

func (f RecordForeignKey) GetChildren() []Node { return nil }

// QualifiedName is "<schema>.<table>", or just the table when it has no schema.
func (t RecordTable) QualifiedName() string {
	if t.Package == "" {
		return t.Type
	}
	return t.Package + "." + t.Type
}

// Column returns the named column, matched case-insensitively because SQL
// identifiers are.
func (t RecordTable) Column(name string) (RecordColumn, bool) {
	for _, c := range t.Columns {
		if strings.EqualFold(c.Field, name) {
			return c, true
		}
	}
	return RecordColumn{}, false
}

// AsRecordField projects a column onto the generic RecordField the non-SQL emitters
// consume. It is the one place the lossy mapping happens, so a caller that wants
// the full picture can still reach for the RecordColumn.
func (c RecordColumn) AsRecordField() RecordField {
	field := Field(c.Field, c.FieldType)
	field.Metadata = c.Metadata
	field.SourceCode = c.SourceCode
	field.Label = c.Label
	field.DefaultValue = c.DefaultValue
	field.ReadOnly = c.AutoIncrement
	field.Validation.Required = !c.Nullable
	field.Validation.Unique = c.Unique
	if c.MaxLength != nil && *c.MaxLength > 0 {
		field.Validation.MaxLength = c.MaxLength
	}
	if len(c.Enum) > 0 {
		field.FieldType = RecordFieldTypeEnum
		field.Validation.Enum = c.Enum
	}
	return field
}

// AsRecord projects a table onto the generic ASTRecord, so consumers that only
// understand records keep working without each of them re-deriving the mapping.
func (t RecordTable) AsRecord() ASTRecord {
	record := ASTRecord{
		nodeBase:    t.nodeBase,
		Description: t.Description,
		RecordType:  t.RecordType,
		Extends:     t.Extends,
	}
	record.NodeType = NodeTypeRecord
	for _, column := range t.Columns {
		record.Fields = append(record.Fields, column.AsRecordField())
	}
	for _, fk := range t.ForeignKeys {
		record.References = append(record.References, fk.Reference())
	}
	return record
}

// Reference projects a foreign key onto the generic RecordReference.
func (f RecordForeignKey) Reference() RecordReference {
	return RecordReference{
		Name:                f.Field,
		Mapping:             strings.Join(f.Columns, ",") + "→" + f.ReferencedTable + "." + strings.Join(f.ReferencedColumns, ","),
		RecordReferenceType: RecordReferenceTypeForeignKey,
	}
}

func (t RecordTable) Pretty() api.Text {
	p := api.Text{Content: string(t.RecordType) + " ", Style: "text-blue-500"}.
		Append(t.QualifiedName(), "text-green-600").
		Append(" {", "text-gray-600").NewLine()
	for _, column := range t.Columns {
		p = p.Append("  ").Add(column.Pretty()).Append(";", "text-gray-600").NewLine()
	}
	for _, index := range t.Indexes {
		p = p.Append("  ").Add(index.Pretty()).NewLine()
	}
	for _, fk := range t.ForeignKeys {
		p = p.Append("  ").Add(fk.Pretty()).NewLine()
	}
	return p.Append("}", "text-gray-600")
}

func (c RecordColumn) Pretty() api.Text {
	p := api.Text{Content: c.Field, Style: "text-green-600"}.Append(": ", "text-gray-600")
	if c.SQLType != "" {
		p = p.Append(c.SQLType, "text-blue-400")
	} else {
		p = p.Add(c.FieldType.Pretty())
	}
	if c.MaxLength != nil && *c.MaxLength != 0 {
		if *c.MaxLength < 0 {
			p = p.Append("(max)", "text-gray-400")
		} else {
			p = p.Append("("+strconv.Itoa(*c.MaxLength)+")", "text-gray-400")
		}
	}
	if !c.Nullable {
		p = p.Append(" NOT NULL", "text-gray-500")
	}
	if c.PrimaryKey {
		p = p.Append(" PK", "text-amber-500")
	}
	if c.AutoIncrement {
		p = p.Append(" IDENTITY", "text-amber-400")
	}
	if c.DefaultValue != nil {
		p = p.Append(" = ", "text-gray-400").Add(c.DefaultValue.Pretty())
	}
	return p
}

func (i RecordIndex) Pretty() api.Text {
	kind := "INDEX"
	switch {
	case i.Primary:
		kind = "PRIMARY KEY"
	case i.Unique:
		kind = "UNIQUE INDEX"
	}
	p := api.Text{Content: kind + " ", Style: "text-purple-500"}.
		Append(i.Field, "text-green-600").
		Append(" ("+strings.Join(i.Columns, ", ")+")", "text-gray-600")
	if len(i.Included) > 0 {
		p = p.Append(" INCLUDE ("+strings.Join(i.Included, ", ")+")", "text-gray-400")
	}
	if i.Condition != "" {
		p = p.Append(" WHERE "+i.Condition, "text-gray-400")
	}
	return p
}

func (f RecordForeignKey) Pretty() api.Text {
	target := f.ReferencedTable
	if f.ReferencedSchema != "" {
		target = f.ReferencedSchema + "." + f.ReferencedTable
	}
	return api.Text{Content: "FOREIGN KEY ", Style: "text-teal-500"}.
		Append("("+strings.Join(f.Columns, ", ")+")", "text-gray-600").
		Append(" REFERENCES ", "text-teal-500").
		Append(target, "text-green-600").
		Append(" ("+strings.Join(f.ReferencedColumns, ", ")+")", "text-gray-600")
}

func (t RecordTable) Hash() string {
	h := NewHasher("table")
	h.AddString("description", t.Description)
	h.AddString("record_type", string(t.RecordType))
	h.AddNodeList("columns", NodesOf(t.Columns))
	h.AddStringSlice("primary_key", t.PrimaryKey)
	h.AddNodeList("indexes", NodesOf(t.Indexes))
	h.AddNodeList("foreign_keys", NodesOf(t.ForeignKeys))
	h.AddRecords("extends", t.Extends)
	if t.RowCount != nil {
		h.AddInt("row_count", int(*t.RowCount))
	}
	return h.String()
}

func (c RecordColumn) Hash() string {
	h := NewHasher("column")
	h.AddString("name", c.Field)
	h.AddString("label", c.Label)
	h.AddString("sql_type", c.SQLType)
	h.AddString("field_type", string(c.FieldType))
	h.AddInt("ordinal", c.Ordinal)
	h.AddBool("nullable", c.Nullable)
	h.AddBool("primary_key", c.PrimaryKey)
	h.AddBool("auto_increment", c.AutoIncrement)
	h.AddBool("unique", c.Unique)
	h.AddStringSlice("enum", c.Enum)
	addIntPtr(h, "max_length", c.MaxLength)
	addIntPtr(h, "precision", c.Precision)
	addIntPtr(h, "scale", c.Scale)
	h.AddTypedValue("default", c.DefaultValue)
	return h.String()
}

func (i RecordIndex) Hash() string {
	h := NewHasher("index")
	h.AddString("name", i.Field)
	h.AddStringSlice("columns", i.Columns)
	h.AddStringSlice("included", i.Included)
	h.AddBool("unique", i.Unique)
	h.AddBool("primary", i.Primary)
	h.AddBool("clustered", i.Clustered)
	h.AddString("index_type", i.IndexType)
	h.AddString("condition", i.Condition)
	return h.String()
}

func (f RecordForeignKey) Hash() string {
	h := NewHasher("foreignKey")
	h.AddString("name", f.Field)
	h.AddStringSlice("columns", f.Columns)
	h.AddString("referenced_schema", f.ReferencedSchema)
	h.AddString("referenced_table", f.ReferencedTable)
	h.AddStringSlice("referenced_columns", f.ReferencedColumns)
	h.AddString("on_delete", f.OnDelete)
	h.AddString("on_update", f.OnUpdate)
	return h.String()
}

type TableBuilder struct {
	node RecordTable
}

func (b *TableBuilder) Build() RecordTable { return b.node }

func (b *TableBuilder) WithColumns(columns ...RecordColumn) *TableBuilder {
	b.node.Columns = append(b.node.Columns, columns...)
	for _, c := range columns {
		if c.PrimaryKey {
			b.node.PrimaryKey = append(b.node.PrimaryKey, c.Field)
		}
	}
	return b
}

func (b *TableBuilder) WithIndexes(indexes ...RecordIndex) *TableBuilder {
	b.node.Indexes = append(b.node.Indexes, indexes...)
	return b
}

func (b *TableBuilder) WithForeignKeys(keys ...RecordForeignKey) *TableBuilder {
	b.node.ForeignKeys = append(b.node.ForeignKeys, keys...)
	return b
}

// NewTable starts a table named name. Like NewRecord it stamps Identifier.NodeType,
// which is the field the polymorphic node registry keys on when reconstructing a
// node from JSON — an unstamped node cannot be unmarshalled back into its type.
func NewTable(name string, id ...Identifier) *TableBuilder {
	val := RecordTable{RecordType: RecordTypeTable}
	if len(id) > 0 {
		val.Identifier = id[0]
	}
	val.Type = name
	if val.NodeType == "" {
		val.NodeType = NodeTypeTable
	}
	return &TableBuilder{node: val}
}

// NewColumn creates a table column carrying both the dialect type and the
// portable one.
func NewColumn(name, sqlType string, fieldType RecordFieldType) RecordColumn {
	val := RecordColumn{SQLType: sqlType, FieldType: fieldType}
	val.Field = name
	val.NodeType = NodeTypeColumn
	return val
}

// NewIndex creates an index over columns, in index order.
func NewIndex(name string, columns ...string) RecordIndex {
	val := RecordIndex{Columns: columns}
	val.Field = name
	val.NodeType = NodeTypeIndex
	return val
}

// NewForeignKey creates a constraint from columns to table.refColumns.
func NewForeignKey(name string, columns []string, table string, refColumns []string) RecordForeignKey {
	val := RecordForeignKey{Columns: columns, ReferencedTable: table, ReferencedColumns: refColumns}
	val.Field = name
	val.NodeType = NodeTypeForeignKey
	return val
}
