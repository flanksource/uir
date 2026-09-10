package uir

import (
	"encoding/json"
	"fmt"
	"reflect"
)

var NodeMarshaler = NewRegistry[Node]("node_type")

var StatementMarshaler = NewRegistry[Statement]("statement_type")

func init() {
	for _, stmt := range Statements {
		t := reflect.TypeOf(stmt)
		StatementMarshaler.Register(string(stmt.GetStatementType()),
			func() Statement {
				instancePtr := reflect.New(t)
				instance, ok := instancePtr.Interface().(Statement)
				if !ok {
					panic("type does not implement Statement: " + t.Name())
				}
				return instance
			},
		)
	}

	for _, node := range Nodes {
		t := reflect.TypeOf(node)
		NodeMarshaler.Register(string(node.GetType()),
			func() Node {
				instancePtr := reflect.New(t)
				instance, ok := instancePtr.Interface().(Node)
				if !ok {
					panic("type does not implement Node: " + t.Name())
				}
				return instance
			},
		)
	}
}

// statementTypeField is the discriminator both statement registries key on.
const statementTypeField = "statement_type"

// MarshalStatement encodes a statement with its concrete kind stamped under
// statement_type. The kind comes from GetStatementType rather than the embedded
// statementBase.Type, because statements are routinely built as plain struct literals
// (uir.RawStmt{Source: src}) that leave the field unset — JSON written from those
// carries no discriminator and cannot be read back.
func MarshalStatement(stmt Statement) ([]byte, error) {
	raw, err := json.Marshal(stmt)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	out, err := encodeStatementFields(fields, stmt.GetStatementType())
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
	return encodeStatementFields(fields, kind)
}

func encodeStatementFields(fields map[string]json.RawMessage, kind StatementType) ([]byte, error) {
	if kind == "" {
		return nil, fmt.Errorf("statement has no statement type")
	}
	encoded, err := json.Marshal(string(kind))
	if err != nil {
		return nil, err
	}
	fields[statementTypeField] = encoded
	return json.Marshal(fields)
}

func (b BlockStmt) MarshalJSON() ([]byte, error) {
	type alias BlockStmt
	shadow := alias(b)
	shadow.Children = nil
	return marshalBlock(shadow, b.Children, b.GetStatementType())
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
// about Name.
func (t *TestStmt) UnmarshalJSON(data []byte) error {
	if err := t.BlockStmt.UnmarshalJSON(data); err != nil {
		return err
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

type untypedNode map[string]json.RawMessage
type untypedNodes []untypedNode

// UnmarshalJSON unmarshals JSON data into a slice of ModuleNodes
func UnmarshalJSON(data []byte) (*UIR, error) {
	var out = UIR{}

	var nodes untypedNodes
	if err := json.Unmarshal(data, &nodes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal nodes: %w", err)
	}

	for _, n := range nodes {
		node, err := NodeMarshaler.Unmarshal(n)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal node: %w", err)
		}
		out.Add(node)
	}

	return &out, nil
}

// unmarshalStatement unmarshals a JSON-encoded statement by reading its type field
// and using the typeMap to determine the concrete type to unmarshal into
func unmarshalStatement(data []byte) (Statement, error) {

	return StatementMarshaler.UnmarshalByType(data)
}

func (s *BlockStmt) UnmarshalJSON(data []byte) error {
	// First unmarshal into a temporary struct to get all fields except Children
	type Alias BlockStmt
	aux := &struct {
		Type     StatementType     `json:"statement_type"`
		Children []json.RawMessage `json:"children,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(s),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	// Set the type
	s.Type = aux.Type

	// Unmarshal each child statement using the helper function
	s.Children = make([]Statement, 0, len(aux.Children))
	for i, childData := range aux.Children {
		stmt, err := unmarshalStatement(childData)
		if err != nil {
			return fmt.Errorf("failed to unmarshal child statement %d: %w", i, err)
		}
		s.Children = append(s.Children, stmt)
	}

	return nil
}

func (n ASTRecord) GetType() NodeType {
	return NodeTypeRecord
}

func (n PackageNode) GetType() NodeType {
	return NodeTypePackage
}
