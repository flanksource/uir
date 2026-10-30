package indexer

import (
	"errors"
	"fmt"

	"github.com/flanksource/uir"
)

// NodePublication is a root a producer holds as UIR nodes, such as a database catalogue lowered into
// RecordTable and MethodNode values. PublicationFromNodes turns it into the Publication that Publish
// writes; every field but Documents is copied to it unchanged.
type NodePublication struct {
	Options   PublishOptions
	RootKey   string
	Name      string
	Location  ExternalLocation
	Revision  string
	Documents []NodeDocument
}

// NodeDocument is one document of the root: its root-relative slash path, the package it belongs to,
// and the top-level nodes it declares, in the order they are written to its generated source.
type NodeDocument struct {
	PathKey     string
	PackagePath string
	Nodes       []uir.Node
}

// nodeKinds is the symbol kind of each node type PublicationFromNodes publishes. A class is the builtin
// type kind, whose type_form storage.ClassifyTypeShape derives from its UIR shape.
var nodeKinds = map[uir.NodeType]string{
	uir.NodeTypeTable: "table", uir.NodeTypeColumn: "column", uir.NodeTypeIndex: "index", uir.NodeTypeForeignKey: "foreign_key",
	uir.NodeTypeRecord: "record", uir.NodeTypeField: "field", uir.NodeTypeInterface: "interface", uir.NodeTypeEndpoint: "endpoint",
	uir.NodeTypeMethod: "method", uir.NodeTypeFunction: "func", uir.NodeTypeConstructor: "constructor", uir.NodeTypeType: "type",
}

// topLevelSlots is the explorer child slot of a top-level symbol, by kind.
var topLevelSlots = map[string]string{
	"table": "tables", "record": "records", "interface": "types", "type": "types", "endpoint": "endpoints", "func": "functions", "constructor": "functions",
}

// PublicationFromNodes projects UIR nodes onto a publication. Every node and member becomes a declared
// symbol whose identity is its owner chain (a column is owned by its table), whose kind comes from its
// node type, and whose layers, shapes, payload, and semantic hash come from uir.Fingerprint, uir.Shape,
// uir.MarshalNode, and Hash. Each document's source is generated from the nodes' private shapes and
// their lowered bodies, so its spans always agree with its symbols and a body edit changes lines inside
// that symbol's extent alone. A node type with no symbol kind, a node without a name, and a
// symbol or identifier declared twice are errors, as is a publication Publish would refuse.
func PublicationFromNodes(input NodePublication) (Publication, error) {
	if input.RootKey == "" {
		return Publication{}, errors.New("root_key is required")
	}
	publication := Publication{
		Options: input.Options, RootKey: input.RootKey, Name: input.Name, Location: input.Location, Revision: input.Revision,
		Symbols: []PublishedSymbol{}, Documents: []PublishedDocument{},
	}
	declared := declaredNodes{ids: map[string]bool{}, keys: map[string]bool{}}
	for _, document := range input.Documents {
		published, symbols, err := nodeDocument(input.RootKey, document, declared)
		if err != nil {
			return Publication{}, fmt.Errorf("document %q: %w", document.PathKey, err)
		}
		publication.Documents = append(publication.Documents, published)
		publication.Symbols = append(publication.Symbols, symbols...)
	}
	if err := publication.validate(); err != nil {
		return Publication{}, err
	}
	return publication, nil
}

// nodeSymbol is a node as a declared symbol, with its members.
type nodeSymbol struct {
	node       uir.Node
	identity   Identity
	visibility string
	parentKey  string
	slot       string
	ordinal    int
	members    []nodeSymbol
}

// subject is how errors name the symbol: its kind and its qualified name.
func (symbol nodeSymbol) subject(owner *nodeSymbol) string {
	name := symbol.identity.Name
	if owner != nil {
		name = owner.identity.Name + "." + name
	}
	return symbol.identity.Kind + " " + symbol.identity.PackagePath + "." + name
}

// declaredNodes are the ids and identifier keys already declared in the publication.
type declaredNodes struct {
	ids, keys map[string]bool
}

// memberSlot is one group of a node's members: its child slot, its nodes, and whether they are private
// members (indexes, foreign keys, triggers), which are internal and never in their owner's public layer.
type memberSlot struct {
	name    string
	nodes   []uir.Node
	private bool
}

func memberSlots(node uir.Node) []memberSlot {
	switch n := node.(type) {
	case uir.RecordTable:
		return []memberSlot{
			{"columns", uir.NodesOf(n.Columns), false}, {"indexes", uir.NodesOf(n.Indexes), true},
			{"foreignKeys", uir.NodesOf(n.ForeignKeys), true}, {"triggers", uir.NodesOf(n.Triggers), true},
		}
	case uir.ASTRecord:
		return []memberSlot{{"fields", uir.NodesOf(n.Fields), false}}
	case uir.TypedNode:
		return []memberSlot{
			{"variables", uir.NodesOf(n.Variables), false}, {"constructor", optionalMethod(n.Constructor), false},
			{"destructor", optionalMethod(n.Destructor), false}, {"methods", uir.NodesOf(n.Methods), false}, {"types", uir.NodesOf(n.Types), false},
		}
	}
	return nil
}

func optionalMethod(method *uir.MethodNode) []uir.Node {
	if method == nil {
		return nil
	}
	return []uir.Node{*method}
}

// nodeValue is the node a pointer refers to, so every switch below handles values only.
func nodeValue(node uir.Node) uir.Node {
	switch n := node.(type) {
	case *uir.RecordTable:
		return *n
	case *uir.RecordColumn:
		return *n
	case *uir.RecordIndex:
		return *n
	case *uir.RecordForeignKey:
		return *n
	case *uir.ASTRecord:
		return *n
	case *uir.RecordField:
		return *n
	case *uir.TypedNode:
		return *n
	case *uir.MethodNode:
		return *n
	case *uir.ASTEndpoint:
		return *n
	}
	return node
}

// nodeBody is the node's lowered body, which its generated source renders inside its extent: a
// callable's body (a trigger's included) and a view's definition.
func nodeBody(node uir.Node) *uir.BlockStmt {
	switch n := node.(type) {
	case uir.MethodNode:
		return n.Body
	case uir.RecordTable:
		return n.Definition
	}
	return nil
}

// symbolKind is the node's symbol kind; a callable is a method when it has an owner and a func otherwise.
func symbolKind(node uir.Node, owned bool) (string, error) {
	kind, found := nodeKinds[uir.ShapeKind(node)]
	switch {
	case !found:
		return "", fmt.Errorf("node type %q has no symbol kind PublicationFromNodes publishes", uir.ShapeKind(node))
	case kind == "func" && owned:
		return "method", nil
	case kind == "method" && !owned:
		return "func", nil
	}
	return kind, nil
}

// nodeCallableTypes are a callable's parameter and result types, which tell its overloads apart: each
// one's SQL type, else its field type, else its type reference.
func nodeCallableTypes(node uir.Node) (parameters, results []string, err error) {
	method, ok := node.(uir.MethodNode)
	if !ok {
		return nil, nil, nil
	}
	if parameters, err = fieldTypes(method.Params, "parameter", method.Method); err != nil {
		return nil, nil, err
	}
	if results, err = fieldTypes(method.Returns, "result", method.Method); err != nil {
		return nil, nil, err
	}
	return parameters, results, nil
}

func fieldTypes(fields uir.ParamsDef, role, method string) ([]string, error) {
	types := make([]string, len(fields))
	for i, field := range fields {
		switch {
		case field.SQLType != "":
			types[i] = field.SQLType
		case field.FieldType != "":
			types[i] = string(field.FieldType)
		case field.TypeRef != nil && field.TypeRef.Name != "":
			types[i] = field.TypeRef.Name
		default:
			return nil, fmt.Errorf("%s %d %q of %s has no type", role, i, field.Field, method)
		}
	}
	return types, nil
}
