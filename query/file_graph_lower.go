package query

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/indexer/syntax"
	"github.com/flanksource/uir/storage"
)

// loweredFile is a source file as a graph draws it: the functions it declares and the calls, reads
// and writes their bodies make.
type loweredFile struct {
	path          string
	language      string
	functions     []fileFunction
	declared      map[string]int
	relationships []uir.UIRRelationship
	// resolvable are the identity keys of the undeclared targets a lookup by name in an index may
	// place; a Go call on a value, whose receiver type the syntax does not say, is not one.
	resolvable map[string]bool
	// labels are how an undeclared target is written at its first site.
	labels      map[string]string
	groups      map[string]bool
	diagnostics []FileDiagnostic
}

// fileFunction is one function or method the file declares.
type fileFunction struct {
	identifier uir.Identifier
	node       graph.Node
	// name is how a hint lists it.
	name string
}

func lowerGraphFile(file GraphFile, packagePath string) (*loweredFile, error) {
	language, err := file.language()
	if err != nil {
		return nil, err
	}
	if language == fileLanguageSQL {
		return lowerSQLFile(file)
	}
	return lowerGoFile(file, packagePath)
}

func newLoweredFile(path, language string) *loweredFile {
	return &loweredFile{
		path: path, language: language, declared: map[string]int{}, resolvable: map[string]bool{},
		labels: map[string]string{}, groups: map[string]bool{},
	}
}

func (lowered *loweredFile) declare(function fileFunction) {
	lowered.declared[function.identifier.IdentityKey()] = len(lowered.functions)
	lowered.functions = append(lowered.functions, function)
	lowered.groups[function.node.Group] = true
}

func (lowered *loweredFile) declares(id string) bool {
	_, declared := lowered.declared[id]
	return declared
}

func (lowered *loweredFile) nodes() []uir.Node {
	nodes := make([]uir.Node, 0, len(lowered.functions))
	for _, function := range lowered.functions {
		nodes = append(nodes, function.identifier.AsRef())
	}
	return nodes
}

// describe is a declared function's node, or an unresolved leaf for any other target.
func (lowered *loweredFile) describe(identifier uir.Identifier) graph.Node {
	key := identifier.IdentityKey()
	if position, declared := lowered.declared[key]; declared {
		return lowered.functions[position].node
	}
	return graph.Node{Kind: "unresolved", Label: lowered.labels[key], Group: identifier.Package, Unresolved: true}
}

// module is the module a group's exclusion is matched by: std for a Go package outside the file
// whose path has no dot in its first element, as the go command tells the standard library apart.
func (lowered *loweredFile) module(group string) string {
	first, _, _ := strings.Cut(group, "/")
	if lowered.language == fileLanguageGo && !lowered.groups[group] && group != builtinGroup && !strings.Contains(first, ".") {
		return "std"
	}
	return ""
}

// lowerGoFile reads a Go file's functions and calls from its syntax: the call occurrences
// its syntax document holds, each with the whole call and its guards as the source says. packagePath
// is the file's package path, or empty for its package clause name.
func lowerGoFile(file GraphFile, packagePath string) (*loweredFile, error) {
	guards, err := parseGuardFile(file.Path, file.Content)
	if err != nil {
		return nil, invalidFile("%v", err)
	}
	if packagePath == "" {
		packagePath = guards.syntax.Name.Name
	}
	document, err := syntax.Document(file.Path, packagePath, file.Content)
	if err != nil {
		return nil, fmt.Errorf("extract %s: %w", file.Path, err)
	}
	lowered := newLoweredFile(file.Path, fileLanguageGo)
	enclosing := map[string]uir.Identifier{}
	functions := map[string]uir.Identifier{}
	for _, symbol := range document.Symbols {
		if symbol.Kind != "func" && symbol.Kind != "method" {
			continue
		}
		lowered.declare(goFunction(file.Path, symbol))
		enclosing[symbol.Key] = symbol.Identifier
		if symbol.Identifier.Type == "" {
			functions[symbol.Identifier.Method] = symbol.Identifier
		}
	}
	for _, occurrence := range document.Occurrences {
		if occurrence.Role != "call" {
			continue
		}
		from, found := enclosing[occurrence.EnclosingKey]
		if !found {
			return nil, fmt.Errorf("call %s at %s:%d is in no function the file declares", occurrence.Text, file.Path, occurrence.Range[0])
		}
		site, err := guards.siteAt(occurrence.Bytes)
		if err != nil {
			return nil, err
		}
		target := lowered.goTarget(occurrence, functions)
		relationship := uir.NewRelationship(uir.RelationshipTypeCall, from.AsRef(), target.AsRef()).
			Source(file.Path, occurrence.Range[0], occurrence.Range[0]).Text(site.Text).Build()
		column := occurrence.Range[1]
		relationship.Column, relationship.Guards = &column, site.Guards
		lowered.relationships = append(lowered.relationships, relationship)
	}
	return lowered, nil
}

func goFunction(path string, symbol storage.DocumentSymbol) fileFunction {
	identifier := symbol.Identifier
	label := identifier.Method
	if identifier.Type != "" {
		label = identifier.Type + "." + identifier.Method
	}
	return fileFunction{identifier: identifier, name: label, node: graph.Node{
		Kind: symbol.Kind, Label: label, Group: identifier.Package,
		Location:   &graph.Location{Path: path, Line: symbol.Name[0], Column: symbol.Name[1]},
		Properties: map[string]string{GraphPropertySymbolKind: symbol.Kind},
	}}
}

// goTarget is the callee of a call occurrence: the file's own function a bare identifier names, Go's
// predeclared function or type it names otherwise, or the target the syntax reads, which a lookup in
// an index may place unless it is a method of a value.
func (lowered *loweredFile) goTarget(occurrence storage.DocumentOccurrence, functions map[string]uir.Identifier) uir.Identifier {
	target := *occurrence.Target
	bare := occurrence.Resolvable && occurrence.LocalRoot
	if declared, found := functions[target.Method]; bare && found {
		return declared
	}
	switch {
	case bare && types.Universe.Lookup(target.Method) != nil:
		target = uir.Identifier{Package: builtinGroup, Method: target.Method, NodeType: uir.NodeTypeMethod}
	case occurrence.Resolvable:
		lowered.resolvable[target.IdentityKey()] = true
	}
	if _, labelled := lowered.labels[target.IdentityKey()]; !labelled {
		lowered.labels[target.IdentityKey()] = occurrence.Text
	}
	return target
}

// root is the file function the selector names: by name, Type.Name, package or schema qualified
// name, or full query name, ignoring case in SQL. With no selector, the file must declare exactly one.
func (lowered *loweredFile) root(selector string) (fileFunction, error) {
	var matched []fileFunction
	for _, function := range lowered.functions {
		if selector == "" || lowered.names(function, selector) {
			matched = append(matched, function)
		}
	}
	if len(matched) == 1 {
		return matched[0], nil
	}
	hint := func(functions []fileFunction) string {
		names := make([]string, 0, len(functions))
		for _, function := range functions {
			names = append(names, function.name)
		}
		return "Give a selector naming one of: " + strings.Join(names, ", ") + "."
	}
	switch {
	case len(lowered.functions) == 0:
		return fileFunction{}, &InvalidQueryError{Message: fmt.Sprintf("%s declares no function or procedure to root a graph at", lowered.path), Hint: "Draw a file that declares a function, method or routine."}
	case selector == "":
		return fileFunction{}, &InvalidQueryError{Message: fmt.Sprintf("%s declares %d functions, and with no selector the graph names no root among them", lowered.path, len(lowered.functions)), Hint: hint(lowered.functions)}
	case len(matched) == 0:
		return fileFunction{}, &InvalidQueryError{Message: fmt.Sprintf("selector %q names no function of %s", selector, lowered.path), Hint: hint(lowered.functions)}
	}
	return fileFunction{}, &InvalidQueryError{Message: fmt.Sprintf("selector %q names %d functions of %s", selector, len(matched), lowered.path), Hint: hint(matched)}
}

func (lowered *loweredFile) names(function fileFunction, selector string) bool {
	identifier := function.identifier
	candidates := []string{identifier.Method, identifierQueryName(identifier), identifier.Package + "." + identifier.Method}
	if identifier.Type != "" {
		candidates = append(candidates, identifier.Type+"."+identifier.Method)
	}
	for _, candidate := range candidates {
		if candidate == selector || lowered.language == fileLanguageSQL && strings.EqualFold(candidate, selector) {
			return true
		}
	}
	return false
}

// identifierQueryName is the name `uir query` qualifies a symbol by: its package path or schema,
// type, method and field, dot separated.
func identifierQueryName(identifier uir.Identifier) string {
	var parts []string
	for _, part := range []string{identifier.Package, identifier.Type, identifier.Method, identifier.Field} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ".")
}
