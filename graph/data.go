package graph

import "strings"

// The kinds of data a body reads, writes or calls, and the kind of the node
// that stands for it. A data node's id is its kind, a colon, and its name
// (DataID), which is how a viewer tells data from code: clicky-ui's CallGraph
// reads these ids and kinds, so they are spelled here once.
const (
	DataTable     = "table"
	DataColumn    = "column"
	DataProcedure = "procedure"
	// DataFunction is a database function, scalar or table-valued: sqlfunction,
	// so that a language's own function kind never reads as one.
	DataFunction = "sqlfunction"
	DataEntity   = "entity"
	DataField    = "field"
)

// unresolvedPrefix starts the id of a leaf a reference nothing resolves is
// drawn to.
const unresolvedPrefix = "unresolved:"

// IsDataKind reports whether kind is the kind of a data node.
func IsDataKind(kind string) bool {
	switch kind {
	case DataTable, DataColumn, DataProcedure, DataFunction, DataEntity, DataField:
		return true
	}
	return false
}

// DataID is the id of the data node of kind named name: table:AsPolicy,
// column:AsPolicy.STATUSCODE, procedure:Update_ClassGroup, sqlfunction:fn_Age,
// entity:Policy, field:Policy.StatusCode.
func DataID(kind, name string) string {
	return kind + ":" + name
}

// SplitDataID splits a data node id into its kind and name; false for an id
// whose prefix is not a data kind.
func SplitDataID(id string) (kind, name string, ok bool) {
	kind, name, ok = strings.Cut(id, ":")
	return kind, name, ok && IsDataKind(kind)
}

// SplitMember splits a column or field name into what holds it and the
// member: a column at its last dot, since a table may be schema-qualified
// (audit.AsPlanLog.X); a field at its first, since a field may be nested
// (Valuation.Fund.Units is a Valuation field). A name with no dot is its own
// owner, with no member.
func SplitMember(kind, name string) (owner, member string) {
	at := strings.Index(name, ".")
	if kind == DataColumn {
		at = strings.LastIndex(name, ".")
	}
	if at < 0 {
		return name, ""
	}
	return name[:at], name[at+1:]
}

// UnresolvedID is the id of the leaf a reference of kind to name is drawn to
// when nothing resolves it: unresolved:<kind>:<name>.
func UnresolvedID(kind, name string) string {
	return unresolvedPrefix + kind + ":" + name
}
