package uir

import (
	"encoding/json"
	"fmt"
)

// nodeKindField is the discriminator a Node carries across an interface-typed
// slot. It is deliberately not node_type: that key is Identifier.NodeType, which
// is data — a MethodNode may be a "constructor", and a NodeRef carries the type of
// the node it points at — so stamping the concrete kind over it would change the
// node's identity, and dispatching on it would decode a reference to a method as
// a MethodNode.
const nodeKindField = "node_kind"

// NodeMarshaler decodes a Node by its node_kind.
var NodeMarshaler = NewRegistry[Node](nodeKindField)

// registeredNodeKind is the kind a registered node is encoded under: its GetType
// for every node whose type is static. NodeRef.GetType reports the type of the node
// it references, so a reference gets a kind of its own and never decodes as the
// node it points at.
func registeredNodeKind(n Node) NodeType {
	if _, ok := n.(NodeRef); ok {
		return NodeTypeRef
	}
	return n.GetType()
}

// MarshalNode encodes a node with its registered kind stamped under node_kind.
func MarshalNode(n Node) ([]byte, error) {
	out, err := NodeMarshaler.Marshal(n)
	if err != nil {
		return nil, fmt.Errorf("%T: %w", n, err)
	}
	return out, nil
}

// UnmarshalNode decodes a node written by MarshalNode. JSON null is the absent
// node; a node without a node_kind, or with one nothing is registered under, is
// an error rather than a guess.
func UnmarshalNode(data []byte) (Node, error) {
	if isJSONNull(data) {
		return nil, nil
	}
	return NodeMarshaler.UnmarshalByType(data)
}

// encodeNodeField and UnmarshalNode are the one way a Node-typed struct field
// crosses JSON. A nil node is omitted; a typed nil pointer is refused, since it
// would come back as no node at all.
func encodeNodeField(n Node) (json.RawMessage, error) {
	if n == nil {
		return nil, nil
	}
	return MarshalNode(n)
}

// Each type below holds a Node-typed field, which encoding/json can neither stamp
// nor decode. The shadow field at depth 0 takes the node's key from the promoted
// one, so every other field still encodes and decodes through the alias.

func (s MethodCallStmt) MarshalJSON() ([]byte, error) {
	type alias MethodCallStmt
	method, err := encodeNodeField(s.Method)
	if err != nil {
		return nil, fmt.Errorf("MethodCallStmt.Method: %w", err)
	}
	return json.Marshal(struct {
		alias
		Method json.RawMessage `json:"Method,omitempty"`
	}{alias(s), method})
}

func (s *MethodCallStmt) UnmarshalJSON(data []byte) error {
	type alias MethodCallStmt
	aux := struct {
		*alias
		Method json.RawMessage `json:"Method,omitempty"`
	}{alias: (*alias)(s)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	method, err := UnmarshalNode(aux.Method)
	if err != nil {
		return fmt.Errorf("MethodCallStmt.Method: %w", err)
	}
	s.Method = method
	return nil
}

func (s EndpointCallStmt) MarshalJSON() ([]byte, error) {
	type alias EndpointCallStmt
	endpoint, err := encodeNodeField(s.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("EndpointCallStmt.Endpoint: %w", err)
	}
	return json.Marshal(struct {
		alias
		Endpoint json.RawMessage `json:"endpoint,omitempty"`
	}{alias(s), endpoint})
}

func (s *EndpointCallStmt) UnmarshalJSON(data []byte) error {
	type alias EndpointCallStmt
	aux := struct {
		*alias
		Endpoint json.RawMessage `json:"endpoint,omitempty"`
	}{alias: (*alias)(s)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	endpoint, err := UnmarshalNode(aux.Endpoint)
	if err != nil {
		return fmt.Errorf("EndpointCallStmt.Endpoint: %w", err)
	}
	s.Endpoint = endpoint
	return nil
}

func (s RecordReadStmt) MarshalJSON() ([]byte, error) {
	type alias RecordReadStmt
	record, err := encodeNodeField(s.Record)
	if err != nil {
		return nil, fmt.Errorf("RecordReadStmt.Record: %w", err)
	}
	return json.Marshal(struct {
		alias
		Record json.RawMessage `json:"Record,omitempty"`
	}{alias(s), record})
}

func (s *RecordReadStmt) UnmarshalJSON(data []byte) error {
	type alias RecordReadStmt
	aux := struct {
		*alias
		Record json.RawMessage `json:"Record,omitempty"`
	}{alias: (*alias)(s)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	record, err := UnmarshalNode(aux.Record)
	if err != nil {
		return fmt.Errorf("RecordReadStmt.Record: %w", err)
	}
	s.Record = record
	return nil
}

func (s RecordWriteStmt) MarshalJSON() ([]byte, error) {
	type alias RecordWriteStmt
	record, err := encodeNodeField(s.Record)
	if err != nil {
		return nil, fmt.Errorf("RecordWriteStmt.Record: %w", err)
	}
	return json.Marshal(struct {
		alias
		Record json.RawMessage `json:"Record,omitempty"`
	}{alias(s), record})
}

func (s *RecordWriteStmt) UnmarshalJSON(data []byte) error {
	type alias RecordWriteStmt
	aux := struct {
		*alias
		Record json.RawMessage `json:"Record,omitempty"`
	}{alias: (*alias)(s)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	record, err := UnmarshalNode(aux.Record)
	if err != nil {
		return fmt.Errorf("RecordWriteStmt.Record: %w", err)
	}
	s.Record = record
	return nil
}

func (r UIRRelationship) MarshalJSON() ([]byte, error) {
	type alias UIRRelationship
	from, err := encodeNodeField(r.GetFrom())
	if err != nil {
		return nil, fmt.Errorf("UIRRelationship.From: %w", err)
	}
	to, err := encodeNodeField(r.To)
	if err != nil {
		return nil, fmt.Errorf("UIRRelationship.To: %w", err)
	}
	return json.Marshal(struct {
		alias
		From json.RawMessage `json:"from,omitempty"`
		To   json.RawMessage `json:"to,omitempty"`
	}{alias(r), from, to})
}

// UnmarshalJSON leaves From nil when the relationship has no source, as
// NewRelationship builds it.
func (r *UIRRelationship) UnmarshalJSON(data []byte) error {
	type alias UIRRelationship
	aux := struct {
		*alias
		From json.RawMessage `json:"from,omitempty"`
		To   json.RawMessage `json:"to,omitempty"`
	}{alias: (*alias)(r)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	from, err := UnmarshalNode(aux.From)
	if err != nil {
		return fmt.Errorf("UIRRelationship.From: %w", err)
	}
	to, err := UnmarshalNode(aux.To)
	if err != nil {
		return fmt.Errorf("UIRRelationship.To: %w", err)
	}
	r.From = nil
	if from != nil {
		r.From = &from
	}
	r.To = to
	return nil
}
