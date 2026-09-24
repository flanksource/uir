package uir

import (
	"fmt"

	"github.com/flanksource/clicky/api"
)

// NodeRef is a leaf reference to a UIR node by its identifier. It names the node
// without holding it, so it has no children to walk — the referenced node is
// walked where it is declared — and no location or language, which its
// Identifier does not carry.
type NodeRef struct {
	Identifier `json:",inline"`
}

// GetChildren implements Node.
func (ref NodeRef) GetChildren() []Node {
	return nil
}

// GetIdentifier implements Node.
// Subtle: this method shadows the method (Identifier).GetIdentifier of NodeRef.Identifier.
func (ref NodeRef) GetIdentifier() Identifier {
	return ref.Identifier
}

// GetLanguage implements Node.
func (ref NodeRef) GetLanguage() string {
	return ""
}

// GetLocation implements Node.
func (ref NodeRef) GetLocation() Location {
	return Location{}
}

// GetType implements Node.
func (ref NodeRef) GetType() NodeType {
	return ref.NodeType
}

func (ref NodeRef) Pretty() api.Text {
	return ref.Identifier.Pretty()
}

type NodeList []Node

// GetIdentifier implements Node.
func (nl NodeList) GetIdentifier() Identifier {
	return NilIdentifier
}

// GetLanguage implements Node.
func (nl NodeList) GetLanguage() string {
	return ""

}

// GetLocation implements Node.
func (nl NodeList) GetLocation() Location {
	return Location{}
}

// GetType implements Node.
func (nl NodeList) GetType() NodeType {
	return NodeTypeUnknown
}

// Pretty implements Node.
func (nl NodeList) Pretty() api.Text {
	return api.Text{}
}

func (nl NodeList) GetChildren() []Node {
	return nl
}

// UIRNode represents a node in the UIR tree, which can be a module, package, type, record, endpoint, or function.
type UIRNode struct {
	Module   *ModuleNode  `json:"module,omitempty" gorm:"serializer:json"`
	Package  *PackageNode `json:"package,omitempty" gorm:"serializer:json"`
	Type     *TypedNode   `json:"type,omitempty" gorm:"serializer:json"`
	Record   *ASTRecord   `json:"record,omitempty" gorm:"serializer:json"`
	Table    *RecordTable `json:"table,omitempty" gorm:"serializer:json"`
	Endpoint *ASTEndpoint `json:"endpoint,omitempty" gorm:"serializer:json"`
	Function *MethodNode  `json:"function,omitempty" gorm:"serializer:json"`
}

type RecordIdentifier struct {
	EnvironmentIdentifier `json:",inline"`
	ID                    string     `json:"id"`
	RecordType            RecordType `json:"recordType,omitempty"`
}

func (node UIRNode) GetType() NodeType {
	nodeType := node.Value().GetIdentifier().NodeType
	if nodeType != NodeTypeUnknown {
		return nodeType
	}
	if node.Function != nil {
		return NodeTypeFunction
	}
	if node.Endpoint != nil {
		return NodeTypeEndpoint
	}
	if node.Record != nil {
		return NodeTypeRecord
	}
	if node.Table != nil {
		return NodeTypeTable
	}
	if node.Type != nil {
		return NodeTypeType
	}
	if node.Package != nil {
		return NodeTypePackage
	}
	if node.Module != nil {
		return NodeTypeModule
	}
	return NodeTypeUnknown
}

func (node UIRNode) Validate() error {
	count := 0
	if node.Module != nil {
		count++
	}
	if node.Package != nil {
		count++
	}
	if node.Type != nil {
		count++
	}
	if node.Record != nil {
		count++
	}
	if node.Table != nil {
		count++
	}
	if node.Endpoint != nil {
		count++
	}
	if node.Function != nil {
		count++
	}
	if count == 0 {
		return fmt.Errorf("invalid: must have exactly one non-nil child node")
	}
	if count > 1 {
		return fmt.Errorf("invalid: multiple child nodes present")
	}
	return nil
}

func (u UIRNode) AsTree() NodeTree {
	return NodeTree{Node: u.Value()}
}

type nilNode struct{}

// GetChildren implements Node.
func (n nilNode) GetChildren() []Node {
	return nil
}

// GetIdentifier implements Node.
func (n nilNode) GetIdentifier() Identifier {
	return Identifier{NodeType: NodeTypeUnknown}
}

// GetLanguage implements Node.
func (n nilNode) GetLanguage() string {
	return ""
}

// GetLocation implements Node.
func (n nilNode) GetLocation() Location {
	return Location{}
}

// Pretty implements Node.
func (n nilNode) Pretty() api.Text {
	return api.Text{}
}

func (n nilNode) GetType() NodeType {
	return NodeTypeUnknown
}

var NIL_NODE Node = nilNode{}

func (u UIRNode) Value() Node {
	if u.Module != nil {
		return u.Module
	}
	if u.Package != nil {
		return u.Package
	}
	if u.Type != nil {
		return u.Type
	}
	if u.Record != nil {
		return u.Record
	}
	if u.Table != nil {
		return u.Table
	}
	if u.Endpoint != nil {
		return u.Endpoint
	}
	if u.Function != nil {
		return u.Function
	}
	return NIL_NODE
}

func (u UIRNode) GetIdentifier() Identifier {
	return u.Value().GetIdentifier()
}

func (u UIRNode) Pretty() api.Text {
	return u.Value().Pretty()
}

func (u UIRNode) String() string {
	return u.Value().GetIdentifier().String()
}

func (u UIRNode) GetChildren() []Node {
	return u.Value().GetChildren()
}

func (u UIRNode) GetLocation() Location {
	return u.Value().GetLocation()
}

func (u UIRNode) GetLanguage() string {
	return u.Value().GetLanguage()
}

// Helper methods for backwards compatibility with legacy ASTNode API
func (u UIRNode) GetPackage() Identifier {
	return u.GetIdentifier().GetPackage()
}

func (u UIRNode) FullName() api.Text {
	return u.GetIdentifier().Pretty()
}

func (u UIRNode) TypeName() string {
	return u.GetIdentifier().Type
}

func (u UIRNode) MethodName() string {
	return u.GetIdentifier().Method
}

func (u UIRNode) FieldName() string {
	return u.GetIdentifier().Field
}

func (r RecordIdentifier) String() string {
	if r.EnvironmentIdentifier.String() == "" {
		return string(r.RecordType) + "//" + r.ID
	}
	return string(r.RecordType) + "//" + r.EnvironmentIdentifier.String() + "/" + r.ID
}

func (r RecordIdentifier) GetType() string {
	return string(ASTStatementRefRecord)
}

var Nodes = []Node{
	ModuleNode{},
	PackageNode{},
	TypedNode{},
	MethodNode{},
	ASTRecord{},
	ASTEndpoint{},
	RecordField{},
	RecordTable{},
	RecordColumn{},
	RecordIndex{},
	RecordForeignKey{},
	// NodeRef is registered so a reference round-trips as a reference; see
	// registeredNodeKind for the kind it is encoded under.
	NodeRef{},
}
