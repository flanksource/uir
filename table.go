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
	RecordType      RecordType         `json:"recordType,omitempty"`
	Columns         []RecordColumn     `json:"columns,omitempty"`
	PrimaryKey      []string           `json:"primaryKey,omitempty"`
	Indexes         []RecordIndex      `json:"indexes,omitempty"`
	ForeignKeys     []RecordForeignKey `json:"foreignKeys,omitempty"`
	Checks          []SQLCheck         `json:"checks,omitempty"`
	SQLDependencies []SQLDependency    `json:"sqlDependencies,omitempty"`
	// RowCount is the catalogue's estimate, nil when it was not asked for. It is a
	// statistic, so no fingerprint layer reads it.
	RowCount *int64 `json:"rowCount,omitempty"`
	// Definition is the lowered body of a view: the query that produces its
	// columns. Rewriting it without changing the columns is a content change.
	Definition *BlockStmt `json:"definition,omitempty"`
	// Triggers defined against the table. Each carries its event and timing in
	// Properties under TriggerEventProperty and TriggerTimingProperty, and its
	// lowered body in Body.
	Triggers []MethodNode `json:"triggers,omitempty"`
}

// The Properties keys a trigger's declaration is read from. Both are private
// facts of the trigger and of its table.
const (
	TriggerEventProperty  = "event"
	TriggerTimingProperty = "timing"
)

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
	MaxLength    *int         `json:"maxLength,omitempty"`
	Precision    *int         `json:"precision,omitempty"`
	Scale        *int         `json:"scale,omitempty"`
	DefaultValue *TypedValue  `json:"defaultValue,omitempty"`
	Identity     *SQLIdentity `json:"identity,omitempty"`
	SQLDefault   *SQLDefault  `json:"sqlDefault,omitempty"`
	Collation    string       `json:"collation,omitempty"`
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
	Condition  string   `json:"condition,omitempty"`
	Descending []string `json:"descending,omitempty"`
	Constraint bool     `json:"constraint,omitempty"`
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
	children := make([]Node, 0, len(t.Columns)+len(t.Indexes)+len(t.ForeignKeys)+len(t.Triggers))
	for i := range t.Columns {
		children = append(children, t.Columns[i])
	}
	for i := range t.Indexes {
		children = append(children, t.Indexes[i])
	}
	for i := range t.ForeignKeys {
		children = append(children, t.ForeignKeys[i])
	}
	for i := range t.Triggers {
		children = append(children, t.Triggers[i])
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
	p := api.Text{Content: string(t.RecordType) + " ", Style: StyleKeyword}.
		Append(t.QualifiedName(), StyleName).
		Append(" {", StylePunctuation).NewLine()
	for _, column := range t.Columns {
		p = p.Append("  ").Add(column.Pretty()).Append(";", StylePunctuation).NewLine()
	}
	for _, index := range t.Indexes {
		p = p.Append("  ").Add(index.Pretty()).NewLine()
	}
	for _, fk := range t.ForeignKeys {
		p = p.Append("  ").Add(fk.Pretty()).NewLine()
	}
	for _, trigger := range t.Triggers {
		p = p.Append("  TRIGGER ", StyleKeyword).Add(trigger.Pretty().Indent(2)).NewLine()
	}
	if t.Definition != nil {
		p = p.Append("  AS ", StyleKeyword).Add(t.Definition.Pretty().Indent(2)).NewLine()
	}
	return p.Append("}", StylePunctuation)
}

func (c RecordColumn) Pretty() api.Text {
	p := api.Text{Content: c.Field, Style: StyleName}.Append(": ", StylePunctuation)
	if c.SQLType != "" {
		p = p.Append(c.SQLType, StyleSQLType)
	} else {
		p = p.Add(c.FieldType.Pretty())
	}
	if c.MaxLength != nil && *c.MaxLength != 0 {
		if *c.MaxLength < 0 {
			p = p.Append("(max)", StyleFaint)
		} else {
			p = p.Append("("+strconv.Itoa(*c.MaxLength)+")", StyleFaint)
		}
	}
	if !c.Nullable {
		p = p.Append(" NOT NULL", StyleDim)
	}
	if c.PrimaryKey {
		p = p.Append(" PK", StylePrimaryKey)
	}
	if c.AutoIncrement {
		p = p.Append(" IDENTITY", StyleIdentity)
	}
	if c.DefaultValue != nil {
		p = p.Append(" = ", StyleFaint).Add(c.DefaultValue.Pretty())
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
	p := api.Text{Content: kind + " ", Style: StyleIndexKind}.
		Append(i.Field, StyleName).
		Append(" ("+strings.Join(i.Columns, ", ")+")", StylePunctuation)
	if len(i.Included) > 0 {
		p = p.Append(" INCLUDE ("+strings.Join(i.Included, ", ")+")", StyleFaint)
	}
	if i.Condition != "" {
		p = p.Append(" WHERE "+i.Condition, StyleFaint)
	}
	return p
}

func (f RecordForeignKey) Pretty() api.Text {
	target := f.ReferencedTable
	if f.ReferencedSchema != "" {
		target = f.ReferencedSchema + "." + f.ReferencedTable
	}
	return api.Text{Content: "FOREIGN KEY ", Style: StyleForeignKey}.
		Append("("+strings.Join(f.Columns, ", ")+")", StylePunctuation).
		Append(" REFERENCES ", StyleForeignKey).
		Append(target, StyleName).
		Append(" ("+strings.Join(f.ReferencedColumns, ", ")+")", StylePunctuation)
}

func (t RecordTable) Hash() string { return Fingerprint(t).Hash() }

func (c RecordColumn) Hash() string { return Fingerprint(c).Hash() }

func (i RecordIndex) Hash() string { return Fingerprint(i).Hash() }

func (f RecordForeignKey) Hash() string { return Fingerprint(f).Hash() }

type TableBuilder struct {
	node RecordTable
}

func (b *TableBuilder) Build() RecordTable { return b.node }

// memberOf qualifies a member's identifier with its table, keeping the member's
// own name and node type, so two tables' "id" columns are distinct symbols.
func (b *TableBuilder) memberOf(member Identifier) Identifier {
	member.Module = b.node.Module
	member.Package = b.node.Package
	member.Type = b.node.Type
	return member
}

func (b *TableBuilder) WithColumns(columns ...RecordColumn) *TableBuilder {
	for _, c := range columns {
		c.Identifier = b.memberOf(c.Identifier)
		b.node.Columns = append(b.node.Columns, c)
		if c.PrimaryKey {
			b.node.PrimaryKey = append(b.node.PrimaryKey, c.Field)
		}
	}
	return b
}

func (b *TableBuilder) WithIndexes(indexes ...RecordIndex) *TableBuilder {
	for _, index := range indexes {
		index.Identifier = b.memberOf(index.Identifier)
		b.node.Indexes = append(b.node.Indexes, index)
	}
	return b
}

func (b *TableBuilder) WithForeignKeys(keys ...RecordForeignKey) *TableBuilder {
	for _, key := range keys {
		key.Identifier = b.memberOf(key.Identifier)
		b.node.ForeignKeys = append(b.node.ForeignKeys, key)
	}
	return b
}

// WithTriggers adds triggers, each a MethodNode named by Identifier.Method.
func (b *TableBuilder) WithTriggers(triggers ...MethodNode) *TableBuilder {
	for _, trigger := range triggers {
		trigger.Identifier = b.memberOf(trigger.Identifier)
		b.node.Triggers = append(b.node.Triggers, trigger)
	}
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
