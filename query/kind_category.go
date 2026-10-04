package query

import (
	"context"
	"fmt"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

// register admits a stored symbol's kind into the query: a kind the index's registry lacks was
// registered after the index loaded it, so the registry is read again once, and a kind still missing
// is an error. Every ModuleSymbol the index builds passes through it, so category never misses.
func (index *indexContext) register(ctx context.Context, kind string) error {
	if _, err := index.kinds.Lookup(kind); err == nil {
		return nil
	}
	kinds, err := storage.LoadSymbolKinds(ctx, index.database)
	if err != nil {
		return err
	}
	index.kinds = kinds
	_, err = kinds.Lookup(kind)
	return err
}

// category is the category of a kind the index admitted; module and package, the tree selector
// kinds, are builtin containers.
func (index *indexContext) category(kind string) symbolhandle.Category {
	spec, err := index.kinds.Lookup(kind)
	if err != nil {
		panic(fmt.Sprintf("query: symbol kind %q was never admitted: %v", kind, err))
	}
	return spec.Category
}

// callable reports whether `<` and `>` read the symbol's call occurrences.
func (index *indexContext) callable(symbol ModuleSymbol) bool {
	return index.category(symbol.Kind) == symbolhandle.CategoryCallable
}

// relationAccepts reports whether a relation can start from a symbol of kind: `>` from a callable,
// the Go type relations from a Go type, and any other relation from anything but a container.
func (index *indexContext) relationAccepts(relation, kind string) bool {
	switch relation {
	case ">":
		return index.category(kind) == symbolhandle.CategoryCallable
	case ":impl", ":inherits", ":methods":
		return kind == symbolhandle.KindType.String()
	default:
		return index.category(kind) != symbolhandle.CategoryContainer
	}
}

// identifier is the node a symbol names: a module or package node for a container, a type node for a
// type, a method node for a callable (owned by its owner's type when it has one), and a field node of
// its owner for a member. A builtin, which has neither package nor owner, is named by its package node.
func (index *indexContext) identifier(symbol ModuleSymbol) uir.Identifier {
	identifier := uir.Identifier{Module: symbol.ModuleKey, Package: symbol.PackagePath}
	switch {
	case symbol.Kind == symbolhandle.KindModule.String():
		identifier.NodeType, identifier.Package = uir.NodeTypeModule, ""
		return identifier
	case symbol.Kind == symbolhandle.KindBuiltin.String():
		identifier.NodeType = uir.NodeTypePackage
		return identifier
	}
	switch index.category(symbol.Kind) {
	case symbolhandle.CategoryType:
		identifier.Type, identifier.NodeType = symbol.Name, uir.NodeTypeType
	case symbolhandle.CategoryCallable:
		identifier.Type, identifier.Method, identifier.NodeType = symbol.Owner, symbol.Name, uir.NodeTypeMethod
	case symbolhandle.CategoryMember:
		identifier.Type, identifier.Field, identifier.NodeType = symbol.Owner, symbol.Name, uir.NodeTypeField
	default:
		identifier.NodeType = uir.NodeTypePackage
	}
	return identifier
}

// symbolSelectors are the selectors that select symbols by kind and match their names.
var symbolSelectors = map[string]bool{"func": true, "method": true, "field": true, "struct": true, "type": true, "var": true, "kind": true}

// selectorKinds is the symbols.kind values a selector selects: every callable for func:, every type
// for type: and struct: (whose type-form check keeps Go struct types only), exactly its kind for
// method:, field:, var:, and kind:, and nil, every kind, for a scope selector.
func selectorKinds(selector Selector, kinds symbolhandle.Kinds) ([]string, error) {
	switch selector.Kind {
	case "func":
		return kinds.InCategory(symbolhandle.CategoryCallable), nil
	case "type", "struct":
		return kinds.InCategory(symbolhandle.CategoryType), nil
	case "method", "field", "var":
		return []string{selector.Kind}, nil
	case "kind":
		spec, err := kinds.Lookup(selector.SymbolKind)
		if err != nil {
			return nil, err
		}
		return []string{spec.Name}, nil
	case "pkg", "mod", "all", "path":
		return nil, nil
	}
	return nil, fmt.Errorf("unknown selector kind %q", selector.Kind)
}
