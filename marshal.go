package uir

import (
	"encoding/json"
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
// Registry.stamp).
func MarshalStatement(stmt Statement) ([]byte, error) {
	out, err := StatementMarshaler.Marshal(stmt)
	if err != nil {
		return nil, fmt.Errorf("%T: %w", stmt, err)
	}
	return out, nil
}

// marshalBlock encodes a block-shaped statement. shadow holds the block's own fields
// under a type that does not carry BlockStmt's MarshalJSON, and every child is stamped
// with its own kind so the interface-typed children survive the round-trip.
func marshalBlock(shadow any, children []Statement, kind StatementType) ([]byte, error) {
	raw, err := json.Marshal(shadow)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if len(children) > 0 {
		encoded := make([]json.RawMessage, len(children))
		for i, child := range children {
			if encoded[i], err = MarshalStatement(child); err != nil {
				return nil, fmt.Errorf("children[%d]: %w", i, err)
			}
		}
		if fields["children"], err = json.Marshal(encoded); err != nil {
			return nil, err
		}
	}
	if err := StatementMarshaler.stamp(fields, string(kind)); err != nil {
		return nil, fmt.Errorf("%s: %w", kind, err)
	}
	return json.Marshal(fields)
}

func (b BlockStmt) MarshalJSON() ([]byte, error) {
	type alias BlockStmt
	shadow := alias(b)
	shadow.Children = nil
	return marshalBlock(shadow, b.Children, b.GetStatementType())
}

func (s *BlockStmt) UnmarshalJSON(data []byte) error {
	return s.decodeBlock(data, s.GetStatementType())
}

// decodeBlock decodes a block-shaped statement of the given kind. Its Type is a
// refinement when one travelled beside the kind (a block in a concrete slot, as
// marshalBlock writes it) or when the statement registry already moved it back
// under statement_type. A document stamped as, or refined to, anything that does
// not resolve to kind is refused rather than read as a block, and JSON null leaves
// the block as it is, as encoding/json does for every other struct.
func (s *BlockStmt) decodeBlock(data []byte, kind StatementType) error {
	if isJSONNull(data) {
		return nil
	}
	type alias BlockStmt
	aux := struct {
		*alias
		Children []json.RawMessage `json:"children,omitempty"`
		// Refinement is keyed statementRefinementField.
		Refinement *StatementType `json:"statement_refinement,omitempty"`
	}{alias: (*alias)(s)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.Refinement != nil {
		if s.Type != kind {
			return fmt.Errorf("a %s cannot be decoded from %s %q", kind, statementTypeField, s.Type)
		}
		s.Type = *aux.Refinement
	}
	if s.Type != "" && s.Type != kind {
		if err := StatementMarshaler.checkRefinement(string(kind), string(s.Type)); err != nil {
			return fmt.Errorf("a %s cannot be decoded: %w", kind, err)
		}
	}
	s.Children = nil
	for i, childData := range aux.Children {
		stmt, err := StatementMarshaler.UnmarshalByType(childData)
		if err != nil {
			return fmt.Errorf("children[%d]: %w", i, err)
		}
		s.Children = append(s.Children, stmt)
	}
	return nil
}

// TestStmt embeds BlockStmt, which would otherwise promote BlockStmt.MarshalJSON and
// drop Name, so it shadows only the block half.
func (t TestStmt) MarshalJSON() ([]byte, error) {
	type alias BlockStmt
	shadow := struct {
		alias
		Name string `json:"name,omitempty"`
	}{alias: alias(t.BlockStmt), Name: t.Name}
	shadow.Children = nil
	return marshalBlock(shadow, t.Children, t.GetStatementType())
}

// TestStmt likewise shadows the promoted BlockStmt.UnmarshalJSON, which knows nothing
// about Name and would refuse the test kind.
func (t *TestStmt) UnmarshalJSON(data []byte) error {
	if err := t.decodeBlock(data, t.GetStatementType()); err != nil {
		return err
	}
	if isJSONNull(data) {
		return nil
	}
	var aux struct {
		Name string `json:"name,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	t.Name = aux.Name
	return nil
}

// FunctionDeclStmt nests its method under "function": the statement and the method
// both carry Metadata, and flattened into one object the method's was shadowed
// and lost. It also shadows the MethodNode.UnmarshalJSON it would otherwise
// promote, which decoded the whole statement as a bare method.
func (f *FunctionDeclStmt) UnmarshalJSON(data []byte) error {
	if isJSONNull(data) {
		return nil
	}
	var aux struct {
		Function json.RawMessage `json:"function"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &f.statementBase); err != nil {
		return err
	}
	if isJSONNull(aux.Function) {
		return nil
	}
	if err := json.Unmarshal(aux.Function, &f.MethodNode); err != nil {
		return fmt.Errorf("function: %w", err)
	}
	return nil
}

func isJSONNull(data []byte) bool {
	return len(data) == 0 || string(data) == "null"
}

// MarshalNodes encodes nodes as the flat array UnmarshalJSON reads, each stamped
// with its node_kind.
func MarshalNodes(nodes []Node) ([]byte, error) {
	encoded := make([]json.RawMessage, len(nodes))
	for i, node := range nodes {
		data, err := MarshalNode(node)
		if err != nil {
			return nil, fmt.Errorf("nodes[%d]: %w", i, err)
		}
		encoded[i] = data
	}
	return json.Marshal(encoded)
}

// UnmarshalJSON decodes the flat node array MarshalNodes writes into a UIR.
func UnmarshalJSON(data []byte) (*UIR, error) {
	var out = UIR{}

	var nodes []json.RawMessage
	if err := json.Unmarshal(data, &nodes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal nodes: %w", err)
	}

	for i, raw := range nodes {
		if isJSONNull(raw) {
			return nil, fmt.Errorf("nodes[%d] is null", i)
		}
		node, err := UnmarshalNode(raw)
		if err != nil {
			return nil, fmt.Errorf("nodes[%d]: %w", i, err)
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
