package uir

import (
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

// Each type below holds a Node-typed field, which encoding/json can neither stamp
// nor decode. Both directions go through the streaming codec (MarshalJSONTo,
// UnmarshalJSONFrom), which writes and reads each node under its node_kind.

func (s MethodCallStmt) MarshalJSON() ([]byte, error) {
	return Marshal(s)
}

func (s *MethodCallStmt) UnmarshalJSON(data []byte) error {
	return Unmarshal(data, s)
}

// DispatchCallStmt encodes its target under "Method" as MethodCallStmt does, and
// each candidate as a node. A nil candidate is refused in both directions: it
// names nothing a call could dispatch to.
func (s DispatchCallStmt) MarshalJSON() ([]byte, error) {
	return Marshal(s)
}

func (s *DispatchCallStmt) UnmarshalJSON(data []byte) error {
	return Unmarshal(data, s)
}

func (s EndpointCallStmt) MarshalJSON() ([]byte, error) {
	return Marshal(s)
}

func (s *EndpointCallStmt) UnmarshalJSON(data []byte) error {
	return Unmarshal(data, s)
}

func (s RecordReadStmt) MarshalJSON() ([]byte, error) {
	return Marshal(s)
}

func (s *RecordReadStmt) UnmarshalJSON(data []byte) error {
	return Unmarshal(data, s)
}

func (s RecordWriteStmt) MarshalJSON() ([]byte, error) {
	return Marshal(s)
}

func (s *RecordWriteStmt) UnmarshalJSON(data []byte) error {
	return Unmarshal(data, s)
}

func (r UIRRelationship) MarshalJSON() ([]byte, error) {
	return Marshal(r)
}

// UnmarshalJSON leaves From nil when the relationship has no source, as
// NewRelationship builds it.
func (r *UIRRelationship) UnmarshalJSON(data []byte) error {
	return Unmarshal(data, r)
}
