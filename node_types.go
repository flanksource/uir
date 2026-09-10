package uir

import (
	"encoding/json"
	"fmt"
)

func (n ModuleNode) GetType() NodeType {
	return NodeTypeModule
}

type ModuleNode struct {
	nodeBase
	Packages []PackageNode `json:"packages,omitempty" gorm:"serializer:json"`
}

type PackageNode struct {
	nodeBase
	Types         []TypedNode   `json:"types,omitempty" gorm:"serializer:json"`
	Records       []ASTRecord   `json:"records,omitempty" gorm:"serializer:json"`
	Tables        []RecordTable `json:"tables,omitempty" gorm:"serializer:json"`
	Endpoints     []ASTEndpoint `json:"endpoints,omitempty" gorm:"serializer:json"`
	Functions     []MethodNode  `json:"functions,omitempty" gorm:"serializer:json"`
	Variables     ParamsDef     `json:"variables,omitempty" gorm:"type:text"`
	InitFunctions []MethodNode  `json:"initFunctions,omitempty" gorm:"serializer:json"`
}

func (p *PackageNode) Add(child Node) error {
	switch n := child.(type) {
	case NodeTree:
		return p.Add(n.Node)
	case *UIRNode:
		return p.Add(n.Value())
	case UIRNode:
		return p.Add(n.Value())
	case TypedNode:
		p.Types = append(p.Types, n)
	case *TypedNode:
		p.Types = append(p.Types, *n)
	case ASTRecord:
		p.Records = append(p.Records, n)
	case *ASTRecord:
		p.Records = append(p.Records, *n)
	case RecordTable:
		p.Tables = append(p.Tables, n)
	case *RecordTable:
		p.Tables = append(p.Tables, *n)
	case ASTEndpoint:
		p.Endpoints = append(p.Endpoints, n)
	case *ASTEndpoint:
		p.Endpoints = append(p.Endpoints, *n)
	case MethodNode:
		p.Functions = append(p.Functions, n)
	case *MethodNode:
		p.Functions = append(p.Functions, *n)
	case *PackageNode:
		return fmt.Errorf("cannot nest PackageNode inside PackageNode")
	default:
		return fmt.Errorf("unsupported child node type %T for PackageNode", child)
	}
	return nil
}

func (t TypedNode) GetType() NodeType {
	return NodeTypeType
}

type TypedNode struct {
	nodeBase
	Visibility Visibility   `json:"visibility,omitempty"`
	Variables  ParamsDef    `json:"variables,omitempty" gorm:"type:text"`
	Methods    []MethodNode `json:"methods,omitempty" gorm:"serializer:json"`
	// Defines any inner types or classes
	Types       []TypedNode     `json:"types,omitempty" gorm:"serializer:json"`
	Constructor *MethodNode     `json:"constructor,omitempty" gorm:"serializer:json"`
	Destructor  *MethodNode     `json:"destructor,omitempty" gorm:"serializer:json"`
	TypeParams  []TypeParam     `json:"typeParams,omitempty" gorm:"serializer:json"`
	Implements  []TypeReference `json:"implements,omitempty" gorm:"serializer:json"`
	Extends     *TypeReference  `json:"extends,omitempty" gorm:"serializer:json"`
	IsAbstract  bool            `json:"isAbstract,omitempty"`
}

func (t TypedNode) GetChildren() []Node {
	var children []Node
	if t.Constructor != nil {
		children = append(children, t.Constructor)
	}
	if t.Destructor != nil {
		children = append(children, t.Destructor)
	}
	for _, method := range t.Methods {
		children = append(children, method)
	}
	for _, innerType := range t.Types {
		children = append(children, innerType)
	}
	return children
}

type nodeBase struct {
	Metadata   `json:",inline"`
	Identifier `json:",inline"`
	SourceCode `json:"sourceCode,omitempty"`
}

func (n nodeBase) GetLanguage() string {
	if n.Language != nil {
		return *n.Language
	}
	return ""
}

func (n nodeBase) GetLocation() Location {
	return n.Location
}

type PersistentBodyMixin interface {
	UnmarshalJSON(data []byte) error
}

var PersistentBodies = []PersistentBodyMixin{
	&MethodNode{},
}

type MethodNode struct {
	nodeBase
	Visibility  Visibility     `json:"visibility,omitempty"`
	Params      ParamsDef      `json:"params,omitempty" gorm:"type:text"`
	Returns     ParamsDef      `json:"returns,omitempty" gorm:"type:text"`
	Errors      []ASTError     `json:"errors,omitempty" gorm:"serializer:json"`
	Body        *BlockStmt     `json:"body,omitempty" gorm:"serializer:json"`
	IsAsync     bool           `json:"isAsync,omitempty"`
	IsGenerator bool           `json:"isGenerator,omitempty"`
	TypeParams  []TypeParam    `json:"typeParams,omitempty" gorm:"serializer:json"`
	ReturnType  *TypeReference `json:"returnType,omitempty" gorm:"serializer:json"`
}

func (m MethodNode) IsPrivate() bool {
	return m.Visibility == VisibilityPrivate
}

func (m MethodNode) IsPublic() bool {
	return m.Visibility == VisibilityPublic
}

// PersistentBodyMixin implementation for MethodNode
func (m MethodNode) Marshal() json.RawMessage {
	if m.Body == nil {
		return nil
	}
	data, err := json.Marshal(m.Body)
	if err != nil {
		return nil
	}
	return data
}

func (m *MethodNode) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	// First unmarshal everything except Body using an alias to avoid recursion
	type Alias MethodNode
	aux := (*Alias)(m)

	// Parse into a map to separate body from other fields
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Extract and remove body from raw data
	bodyData := raw["body"]
	delete(raw, "body")

	// Marshal back without body and unmarshal into aux
	dataWithoutBody, err := json.Marshal(raw)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(dataWithoutBody, aux); err != nil {
		return err
	}

	// Unmarshal the body field separately using StatementMarshaler
	if len(bodyData) > 0 {
		if body, err := StatementMarshaler.UnmarshalByType(bodyData); err != nil {
			return err
		} else if bodyPtr, ok := body.(*BlockStmt); ok {
			m.Body = bodyPtr
		} else if bodyVal, ok := body.(BlockStmt); ok {
			m.Body = &bodyVal
		} else {
			m.Body = &BlockStmt{
				Children: []Statement{body},
			}
		}
	}

	return nil
}

func (m MethodNode) LineCount() int {
	// FIXME: implement line count based on Body location
	return 0
}

func (m *MethodNode) NewCall(method string) *MethodCallBuilder {
	b := NewMethodCall(method, m.GetIdentifier())
	if m.Body == nil {
		m.Body = &BlockStmt{}
	}
	b.Path = m.Path

	return b
}

func (t MethodNode) GetLocations() []Location {
	l := []Location{}
	if !t.IsEmpty() {
		l = append(l, t.Location)
	}
	return l
}

func (t MethodNode) GetType() NodeType {
	return NodeTypeMethod
}

func (m MethodNode) GetChildren() []Node {
	children := []Node{}
	//FIXME: Uncomment when Body is implemented
	// if m.Body != nil {
	// 	children = append(children, m.Body)
	// }
	return children
}

func (p PackageNode) GetChildren() []Node {
	children := []Node{}

	for i := range p.Types {
		children = append(children, p.Types[i])
	}
	for i := range p.Records {
		children = append(children, p.Records[i])
	}
	for i := range p.Tables {
		children = append(children, p.Tables[i])
	}
	for i := range p.Endpoints {
		children = append(children, p.Endpoints[i])
	}
	for i := range p.Functions {
		children = append(children, p.Functions[i])
	}
	for i := range p.InitFunctions {
		children = append(children, p.InitFunctions[i])
	}
	return children
}

// PersistentBodyMixin implementation for PackageNode
func (p PackageNode) GetPersistentBody() json.RawMessage {
	if len(p.Functions) == 0 {
		return nil
	}
	data, err := json.Marshal(p.Functions)
	if err != nil {
		return nil
	}
	return data
}

func (p *PackageNode) LoadPersistentBody(data json.RawMessage) error {
	if len(data) == 0 {
		p.Functions = nil
		return nil
	}
	var functions []MethodNode
	if err := json.Unmarshal(data, &functions); err != nil {
		return fmt.Errorf("failed to unmarshal PackageNode functions: %w", err)
	}
	p.Functions = functions
	return nil
}

func (m ModuleNode) GetChildren() []Node {
	children := []Node{}
	for i := range m.Packages {
		children = append(children, m.Packages[i])
	}
	return children
}
