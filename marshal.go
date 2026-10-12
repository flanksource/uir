package uir

import (
	"fmt"
	"reflect"
)

const (
	// statementTypeField is the discriminator the statement registry keys on.
	statementTypeField = "statement_type"
	// statementRefinementField carries a statement's Type when it is refined past
	// the kind stamped under statement_type.
	statementRefinementField = "statement_refinement"
)

var StatementMarshaler = NewRegistry[Statement](statementTypeField)

// crossHierarchyRefinements are the refinements a statement kind accepts from
// outside its own ':'-hierarchy: the markdown extractor marks a section's body
// block as doc, which as a kind of its own names a DocStmt.
var crossHierarchyRefinements = map[StatementType][]StatementType{
	ASTStatementTypeBlock: {ASTStatementTypeDoc},
}

func init() {
	for _, stmt := range Statements {
		registerPrototype(StatementMarshaler, string(stmt.GetStatementType()), stmt)
	}
	for _, node := range Nodes {
		registerPrototype(NodeMarshaler, string(registeredNodeKind(node)), node)
	}
	crossHierarchy := map[string][]string{}
	for kind, refinements := range crossHierarchyRefinements {
		for _, refinement := range refinements {
			crossHierarchy[string(kind)] = append(crossHierarchy[string(kind)], string(refinement))
		}
	}
	StatementMarshaler.acceptRefinements(statementRefinementField, crossHierarchy)
}

// registerPrototype registers the concrete type behind prototype; the registry
// always builds the pointer form, so a decoded value is addressable.
func registerPrototype[T any](r *Registry[T], kind string, prototype T) {
	t := concreteType(prototype)
	r.Register(kind, func() T {
		instance, ok := reflect.New(t).Interface().(T)
		if !ok {
			panic(fmt.Sprintf("*%s does not implement %s", t, reflect.TypeFor[T]()))
		}
		return instance
	})
}

// MarshalStatement encodes a statement with its concrete kind stamped under
// statement_type. The kind comes from the concrete type rather than the embedded
// statementBase.Type, because statements are routinely built as plain struct
// literals (uir.RawStmt{Source: src}) that leave the field unset — JSON written
// from those carries no discriminator and cannot be read back. An unset Type
// decodes as the kind; a Type refined past it (call:package) travels under
// statement_refinement and decodes back exactly; any other Type is refused (see
// Registry.Marshal).
func MarshalStatement(stmt Statement) ([]byte, error) {
	out, err := StatementMarshaler.Marshal(stmt)
	if err != nil {
		return nil, fmt.Errorf("%T: %w", stmt, err)
	}
	return out, nil
}

// MarshalJSON encodes a block-shaped statement through the streaming codec
// (BlockStmt.MarshalJSONTo): its kind under statement_type, a Type refined past it
// under statement_refinement, and every child under its kind.
func (b BlockStmt) MarshalJSON() ([]byte, error) {
	return Marshal(b)
}

// UnmarshalJSON decodes a block-shaped statement through the streaming codec
// (BlockStmt.UnmarshalJSONFrom). Its Type is a refinement when one travelled
// beside the kind (a block in a concrete slot, as MarshalJSONTo writes it) or when
// the statement registry already moved it back under statement_type. A document
// stamped as, or refined to, anything that does not resolve to the block's kind is
// refused rather than read as a block, and JSON null leaves the block as it is, as
// encoding/json does for every other struct.
func (s *BlockStmt) UnmarshalJSON(data []byte) error {
	return Unmarshal(data, s)
}

// TestStmt embeds BlockStmt, which would otherwise promote BlockStmt.MarshalJSON and
// drop Name, so it shadows only the block half.
func (t TestStmt) MarshalJSON() ([]byte, error) {
	return Marshal(t)
}

// TestStmt likewise shadows the promoted BlockStmt.UnmarshalJSON, which knows nothing
// about Name and would refuse the test kind.
func (t *TestStmt) UnmarshalJSON(data []byte) error {
	return Unmarshal(data, t)
}

// FunctionDeclStmt nests its method under "function": the statement and the method
// both carry Metadata, and flattened into one object the method's was shadowed
// and lost. It also shadows the MethodNode.UnmarshalJSON it would otherwise
// promote, which decoded the whole statement as a bare method.
func (f *FunctionDeclStmt) UnmarshalJSON(data []byte) error {
	return Unmarshal(data, f)
}

func isJSONNull(data []byte) bool {
	return len(data) == 0 || string(data) == "null"
}

// MarshalNodes encodes nodes as the flat array UnmarshalJSON reads, each written
// under its node_kind.
func MarshalNodes(nodes []Node) ([]byte, error) {
	return Marshal(nodeSlots(nodes))
}

// UnmarshalJSON decodes the flat node array MarshalNodes writes into a UIR.
func UnmarshalJSON(data []byte) (*UIR, error) {
	var nodes []Node
	if err := Unmarshal(data, &nodes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal nodes: %w", err)
	}
	out := UIR{}
	for i, node := range nodes {
		if node == nil {
			return nil, fmt.Errorf("nodes[%d] is null", i)
		}
		out.Add(node)
	}
	return &out, nil
}

func (n ASTRecord) GetType() NodeType {
	return NodeTypeRecord
}

func (n PackageNode) GetType() NodeType {
	return NodeTypePackage
}
