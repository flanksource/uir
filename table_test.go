package uir

import (
	"encoding/json"
	"testing"
)

// sampleTable is a table with every kind of structure a generic ASTRecord cannot
// carry: a dialect type, a width, an ordinal, a real primary key, a multi-column
// index and a foreign key.
func sampleTable() RecordTable {
	pk := NewColumn("ActivityGUID", "uniqueidentifier", RecordFieldTypeUUID)
	pk.PrimaryKey = true
	pk.Ordinal = 1

	width := 50
	status := NewColumn("StatusCode", "nvarchar", RecordFieldTypeString)
	status.Ordinal = 2
	status.Nullable = true
	status.MaxLength = &width

	index := NewIndex("IX_7_ASACTIVITY", "PolicyGUID", "EffectiveDate")
	index.Clustered = true

	return NewTable("AsActivity", Identifier{Package: "dbo"}).
		WithColumns(pk, status).
		WithIndexes(index).
		WithForeignKeys(NewForeignKey("FK_Activity_Policy",
			[]string{"PolicyGUID"}, "AsPolicy", []string{"PolicyGUID"})).
		Build()
}

func assertTableSurvived(t *testing.T, got UIR, path string) {
	t.Helper()
	if len(got.Tables) != 1 {
		t.Fatalf("%s: expected 1 table, got %d — the table was dropped in transit", path, len(got.Tables))
	}
	table := got.Tables[0]
	if table.QualifiedName() != "dbo.AsActivity" {
		t.Errorf("%s: identity lost, got %q", path, table.QualifiedName())
	}
	if len(table.Columns) != 2 {
		t.Fatalf("%s: expected 2 columns, got %d", path, len(table.Columns))
	}

	// The four facts today's UIR throws away. Each is asserted separately so a
	// regression names the one that was lost.
	pk, ok := table.Column("activityguid")
	if !ok {
		t.Fatalf("%s: column lookup is meant to be case-insensitive, like SQL identifiers", path)
	}
	if pk.SQLType != "uniqueidentifier" {
		t.Errorf("%s: raw SQL type lost, got %q", path, pk.SQLType)
	}
	if !pk.PrimaryKey {
		t.Errorf("%s: primary key lost", path)
	}
	if pk.Ordinal != 1 {
		t.Errorf("%s: ordinal lost, got %d", path, pk.Ordinal)
	}
	if len(table.PrimaryKey) != 1 || table.PrimaryKey[0] != "ActivityGUID" {
		t.Errorf("%s: table primary key lost, got %v", path, table.PrimaryKey)
	}

	status, _ := table.Column("StatusCode")
	if status.MaxLength == nil || *status.MaxLength != 50 {
		t.Errorf("%s: column width lost, got %v", path, status.MaxLength)
	}

	if len(table.Indexes) != 1 {
		t.Fatalf("%s: expected 1 index, got %d", path, len(table.Indexes))
	}
	// Index column order is what makes an index usable for a predicate, so it
	// must survive in order rather than as a set.
	if got, want := table.Indexes[0].Columns, []string{"PolicyGUID", "EffectiveDate"}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("%s: index columns lost or reordered, got %v", path, got)
	}

	if len(table.ForeignKeys) != 1 {
		t.Fatalf("%s: expected 1 foreign key, got %d", path, len(table.ForeignKeys))
	}
	if fk := table.ForeignKeys[0]; fk.ReferencedTable != "AsPolicy" || len(fk.ReferencedColumns) != 1 {
		t.Errorf("%s: foreign-key target lost, got %+v", path, fk)
	}
}

// The Redis/disk schema cache stores a UIR as plain struct JSON, so a table has
// to survive Marshal→Unmarshal with everything the catalogue told us about it.
// Without this, a cache hit would look healthy and return a schema with no types,
// no keys and no indexes.
func TestUIRTablesSurviveStructJSONRoundTrip(t *testing.T) {
	original := UIR{Tables: []RecordTable{sampleTable()}}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got UIR
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	assertTableSurvived(t, got, "struct json")

	if got.Hash() != original.Hash() {
		t.Errorf("a round-tripped UIR must hash the same, else every cache read looks like a schema change")
	}
}

// Plugin extractors hand back a JSON array of nodes (MarshalNodes), which UnmarshalJSON decodes
// through the polymorphic registry and routes into UIR.Add. A node type missing
// from either the registry seed (var Nodes) or Add's type switch is dropped
// without an error, so this asserts the whole path rather than the decode alone.
func TestRecordTableSurvivesNodeRegistryRoundTrip(t *testing.T) {
	data, err := MarshalNodes([]Node{sampleTable()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalJSON(data)
	if err != nil {
		t.Fatalf("unmarshal nodes: %v", err)
	}
	assertTableSurvived(t, *got, "node registry")
}

// The node registry builds every node with reflect.New, so it hands Add a
// pointer. Add used to match only value types and had no default arm, so every
// decoded node — of every type, not just tables — was dropped and UnmarshalJSON
// returned an empty UIR.
func TestUnmarshalJSONKeepsPointerNodesOfEveryType(t *testing.T) {
	record := NewRecord("AsPolicy", Identifier{Package: "dbo"}).Build()
	endpoint := NewEndpoint("getPolicy").Build()
	table := sampleTable()

	data, err := MarshalNodes([]Node{record, endpoint, table})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalJSON(data)
	if err != nil {
		t.Fatalf("unmarshal nodes: %v", err)
	}
	if len(got.Records) != 1 {
		t.Errorf("record dropped: got %d", len(got.Records))
	}
	if len(got.Endpoints) != 1 {
		t.Errorf("endpoint dropped: got %d", len(got.Endpoints))
	}
	if len(got.Tables) != 1 {
		t.Errorf("table dropped: got %d", len(got.Tables))
	}
}

// A table hashes over its structure, not just its name — a schema diff exists to
// notice a dropped index or a widened column.
func TestTableHashTracksStructuralChange(t *testing.T) {
	base := sampleTable()
	for _, tc := range []struct {
		name   string
		mutate func(*RecordTable)
	}{
		{"a widened column", func(tbl *RecordTable) { wider := 100; tbl.Columns[1].MaxLength = &wider }},
		{"a retyped column", func(tbl *RecordTable) { tbl.Columns[1].SQLType = "nvarchar(max)" }},
		{"a dropped index", func(tbl *RecordTable) { tbl.Indexes = nil }},
		{"a reordered index", func(tbl *RecordTable) {
			tbl.Indexes[0].Columns = []string{"EffectiveDate", "PolicyGUID"}
		}},
		{"a dropped foreign key", func(tbl *RecordTable) { tbl.ForeignKeys = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := sampleTable()
			tc.mutate(&changed)
			if changed.Hash() == base.Hash() {
				t.Errorf("%s did not change the hash, so a schema diff would miss it", tc.name)
			}
		})
	}
}

// The projections are the single place the lossy mapping to the generic node
// shapes happens, so the emitters that only understand records keep working.
func TestTableProjectsOntoRecord(t *testing.T) {
	record := sampleTable().AsRecord()

	if record.GetType() != NodeTypeRecord {
		t.Errorf("a projected table must present as a record, got %q", record.GetType())
	}
	if len(record.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(record.Fields))
	}
	// Nullability inverts into Validation.Required; a nullable column is not required.
	if !record.Fields[0].Validation.Required {
		t.Error("a NOT NULL column must project as required")
	}
	if record.Fields[1].Validation.Required {
		t.Error("a nullable column must not project as required")
	}
	if record.Fields[1].Validation.MaxLength == nil || *record.Fields[1].Validation.MaxLength != 50 {
		t.Error("the column width must reach the projected field's validation")
	}
	// A primary key is not an identity column, and conflating them is the bug
	// this type exists to fix — so the projection must not mark it read-only.
	if record.Fields[0].ReadOnly {
		t.Error("a primary key is not auto-generated; only identity/computed columns are read-only")
	}
	if len(record.References) != 1 || record.References[0].RecordReferenceType != RecordReferenceTypeForeignKey {
		t.Errorf("foreign keys must project onto record references, got %+v", record.References)
	}
}
