package uir

import (
	"strconv"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/google/uuid"
)

var NilIdentifier = Identifier{}

type Identifier struct {
	Id *uuid.UUID `json:"id,omitempty"`
	// The module, namespace, database server, API base url the node belongs to
	Module string `json:"module,omitempty"`
	// The package or database scheme the node belongs to
	Package string `json:"package,omitempty"`
	// The type, class, table, API group the node belongs to
	Type string `json:"type,omitempty"`
	// The method  the node belongs to
	Method string `json:"method,omitempty"`
	Field  string `json:"field,omitempty"`
	// Signature is a way of distinguishing nodes at the Method/Field level and below
	// It is not guaranteed to be unique across different methods/types/packages/modules
	// e.g. For overloaded methods it will be the parameter types and return type that distinguish them
	// for statements within a method it would be the line number and column number
	Signature string   `json:"signature,omitempty"`
	NodeType  NodeType `json:"node_type,omitempty"`
}

func (id Identifier) AsMap() map[string]interface{} {
	m := map[string]interface{}{}
	if id.Module != "" {
		m["module"] = id.Module
	}
	if id.Package != "" {
		m["package"] = id.Package
	}
	if id.Type != "" {
		m["type"] = id.Type
	}
	if id.Method != "" {
		m["method"] = id.Method
	}
	if id.Field != "" {
		m["field"] = id.Field
	}
	if id.Signature != "" {
		m["signature"] = id.Signature
	}
	if id.NodeType != NodeTypeUnknown {
		m["node_type"] = strings.ToLower(string(id.NodeType))
	}
	return m
}

// GetID returns a deterministic uuid-like string for the identifier
// For the same identifier components, it will always return the same ID
func (id Identifier) GetID() string {
	u := id.GetUUID()
	if u == uuid.Nil {
		return ""
	}
	return u.String()
}

func (id Identifier) GetUUID() uuid.UUID {
	if id.Id != nil {
		return *id.Id
	}
	u := uuid.NewSHA1(uuid.NameSpaceOID, []byte(id.SymbolKey()))
	return u
}

func (id Identifier) SymbolKey() string {
	nodeType := id.GetNodeType()
	parts := []string{strings.ToLower(string(nodeType)), id.String()}
	return strings.Join(parts, ":")
}

// IdentityKey returns the canonical, lossless persistence key for an identifier.
func (id Identifier) IdentityKey() string {
	components := [7]string{
		strings.ToLower(string(id.GetNodeType())),
		id.Module,
		id.Package,
		id.Type,
		id.Method,
		id.Field,
		id.Signature,
	}
	var key strings.Builder
	key.WriteString("v1:[")
	for index, component := range components {
		if index > 0 {
			key.WriteByte(',')
		}
		key.WriteString(strconv.Quote(component))
	}
	key.WriteByte(']')
	return key.String()
}

func (id Identifier) AsRef() Node {
	return NodeRef{
		Identifier: id,
	}
}

type IdentifierBuilder struct {
	id Identifier
}

func (b IdentifierBuilder) Module(module string) IdentifierBuilder {
	b.id.Module = module
	return b
}

func (b IdentifierBuilder) Package(pkg string) IdentifierBuilder {
	b.id.Package = pkg
	return b
}

func (b IdentifierBuilder) Type(typ string) IdentifierBuilder {
	b.id.Type = typ
	return b
}

func (b IdentifierBuilder) Method(method string) IdentifierBuilder {
	b.id.Method = method
	return b
}

func (b IdentifierBuilder) Field(field string) IdentifierBuilder {
	b.id.Field = field
	return b
}

func (b IdentifierBuilder) NodeType(nodeType NodeType) IdentifierBuilder {
	b.id.NodeType = nodeType
	return b
}

func (b IdentifierBuilder) Build() Identifier {
	return b.id
}

func NewID() IdentifierBuilder {
	return IdentifierBuilder{id: Identifier{}}
}

type Table interface {
	GetTableName() string
	GetDatabaseName() string
}

func (r ASTRecord) GetTableName() string {
	return r.Type
}

func (r ASTRecord) GetDatabaseName() string {
	return r.Package
}

type Endpoint interface {
	GetURL() string
}

func (e ASTEndpoint) GetURL() string {
	return e.String()
}

func (id Identifier) Equals(other Identifier) bool {
	return id.String() == other.String()
}

func (id Identifier) IsParentOf(other Identifier) bool {
	if id.Module != "" && id.Module != other.Module {
		return false
	}
	if id.Package != "" && id.Package != other.Package {
		return false
	}
	if id.Type != "" && id.Type != other.Type {
		return false
	}
	if id.Method != "" && id.Method != other.Method {
		return false
	}
	if id.Field != "" && id.Field != other.Field {
		return false
	}
	return true
}

func (id Identifier) GetPackage() Identifier {
	return Identifier{
		Module:   id.Module,
		Package:  id.Package,
		NodeType: NodeTypePackage,
	}
}

func (id Identifier) GetModule() Identifier {
	return Identifier{
		Module:   id.Module,
		NodeType: NodeTypeModule,
	}
}

func (id Identifier) GetTypeIdentifier() Identifier {
	return Identifier{
		Module:   id.Module,
		Package:  id.Package,
		Type:     id.Type,
		NodeType: NodeTypeType,
	}
}

func (id Identifier) IsChildOf(other Identifier) bool {
	return other.IsParentOf(id)
}

// String returns the full identifier as a string in the format:
//
//	module.package.type:method:field or module.package.type::field
//
// for endpoints it becomes: `module/package/type/method` providing
// flexibility for base urls, api versions, and paths
func (id Identifier) String() string {

	sep := "."

	if id.NodeType == NodeTypeEndpoint {
		sep = "/"
	}

	parts := []string{}
	if id.Module != "" {
		parts = append(parts, id.Module)
	}
	if id.Package != "" {
		parts = append(parts, id.Package)
	}
	if id.Type != "" {
		parts = append(parts, id.Type)
	}

	path := strings.Join(parts, sep)

	sep = ":"
	switch id.NodeType {
	case NodeTypeEndpoint:
		sep = "/"
	case NodeTypeField:
		sep = "."
	}
	if id.Method != "" {
		path = path + sep + id.Method
	}
	if id.Field != "" {
		if id.Method == "" {
			path += sep
		}
		path = path + sep + id.Field
	}
	if id.Signature != "" {
		path = path + "#" + string(id.Signature)
	}
	return path
}

func (id Identifier) GetIdentifier() Identifier {
	return id
}

func (id Identifier) GetName() string {
	if id.Field != "" {
		return id.Field
	}
	if id.Method != "" {
		return id.Method
	}
	if id.Type != "" {
		return id.Type
	}
	if id.Package != "" {
		return id.Package
	}
	if id.Module != "" {
		return id.Module
	}
	return ""
}

func (id Identifier) GetNodeType() NodeType {
	if id.NodeType != NodeTypeUnknown {
		return id.NodeType
	}
	if id.Field != "" {
		return NodeTypeField
	}
	if id.Method != "" {
		return NodeTypeMethod
	}
	if id.Type != "" {
		return NodeTypeType
	}
	if id.Package != "" {
		return NodeTypePackage
	}
	if id.Module != "" {
		return NodeTypeModule
	}
	return NodeTypeUnknown
}

func (id Identifier) Pretty() api.Text {
	s := clicky.Text("")
	style := id.GetNodeType().Color()

	if id.Module != "" {
		s = s.Append(id.Module, style).Append(".", "muted")
	}
	if id.Package != "" {
		s = s.Append(id.Package, style).Append(".", "muted")
	}
	if id.Type != "" {
		s = s.Append(id.Type, style)
	}
	if id.Method != "" {
		if id.Type != "" || id.Package != "" || id.Module != "" {
			s = s.Append(".", "muted")
		}
		s = s.Append(id.Method, style)
	}
	if id.Field != "" {
		if id.Type != "" || id.Package != "" || id.Module != "" || id.Method != "" {
			s = s.Append(".", "muted")
		}
		s = s.Append(id.Field, style)
	}
	return s
}

func ParseIdentifier(id string) Identifier {
	parts := strings.SplitN(id, ":", 3)
	mainParts := strings.SplitN(parts[0], ".", 3)
	identifier := Identifier{}
	if len(mainParts) > 0 {
		identifier.Type = mainParts[len(mainParts)-1]
	}
	if len(mainParts) > 1 {
		identifier.Package = mainParts[len(mainParts)-2]
	}
	if len(mainParts) > 2 {
		identifier.Module = mainParts[len(mainParts)-3]
	}
	if len(parts) > 1 {
		identifier.Method = parts[1]
	}
	if len(parts) > 2 {
		identifier.Field = parts[2]
	}
	return identifier
}
