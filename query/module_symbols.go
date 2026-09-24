package query

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
)

// ModuleSymbol is a canonical symbol a query resolved; Owner is the owner symbol's name.
type ModuleSymbol struct {
	ID             string       `json:"id"`
	ModuleKey      string       `json:"module_key"`
	PackagePath    string       `json:"package_path"`
	Kind           string       `json:"kind"`
	OwnerID        string       `json:"owner_id,omitempty"`
	Owner          string       `json:"owner,omitempty"`
	Name           string       `json:"name"`
	Visibility     string       `json:"visibility"`
	ParameterTypes storage.JSON `json:"parameter_types"`
}

// identifier spells the symbol as a structured identifier for a candidate with no definition in scope.
func (symbol ModuleSymbol) identifier() uir.Identifier {
	identifier := uir.Identifier{Module: symbol.ModuleKey, Package: symbol.PackagePath}
	switch symbol.Kind {
	case "type":
		identifier.Type, identifier.NodeType = symbol.Name, uir.NodeTypeType
	case "func":
		identifier.Method, identifier.NodeType = symbol.Name, uir.NodeTypeMethod
	case "method":
		identifier.Type, identifier.Method, identifier.NodeType = symbol.Owner, symbol.Name, uir.NodeTypeMethod
	case "field", "var", "const":
		identifier.Type, identifier.Field, identifier.NodeType = symbol.Owner, symbol.Name, uir.NodeTypeField
	default:
		identifier.NodeType = uir.NodeTypePackage
	}
	return identifier
}

var symbolKinds = []string{"package", "type", "func", "method", "field", "var", "const", "builtin"}

// symbolSelector is a set of exact conditions on the symbols table, derived from query predicates.
type symbolSelector struct {
	id, module, pkg, owner, name string
	kinds                        []string
}

// newSymbolSelector maps predicates onto symbol columns: `type` alone names a type and otherwise the
// owner of `method` or `field`; `method` without an owner matches functions and methods.
func newSymbolSelector(predicates []Predicate) (symbolSelector, error) {
	values := map[string]string{}
	for _, predicate := range predicates {
		if previous, found := values[predicate.Field]; found && previous != predicate.Value {
			return symbolSelector{}, fmt.Errorf("%s %q conflicts with %q", predicate.Field, predicate.Value, previous)
		}
		values[predicate.Field] = predicate.Value
	}
	selector := symbolSelector{id: values["symbol_id"], module: values["module"], pkg: values["package"], owner: values["owner"]}
	method, field, typeName := values["method"], values["field"], values["type"]
	member := cmp.Or(method, field)
	switch {
	case method != "" && field != "":
		return symbolSelector{}, fmt.Errorf("method %q and field %q cannot both be selected", method, field)
	case member == "" && typeName != "":
		selector.kinds = []string{"type"}
	case typeName != "" && selector.owner != "" && selector.owner != typeName:
		return symbolSelector{}, fmt.Errorf("type %q conflicts with owner %q", typeName, selector.owner)
	case typeName != "":
		selector.owner = typeName
	}
	switch {
	case method != "" && selector.owner != "":
		selector.kinds = []string{"method"}
	case method != "":
		selector.kinds = []string{"func", "method"}
	case field != "":
		selector.kinds = []string{"field"}
	}
	for _, name := range []string{cmp.Or(member, typeName), values["name"]} {
		if name != "" && selector.name != "" && selector.name != name {
			return symbolSelector{}, fmt.Errorf("name %q conflicts with %q", name, selector.name)
		}
		selector.name = cmp.Or(name, selector.name)
	}
	if err := selector.restrictKind(values["kind"]); err != nil {
		return symbolSelector{}, err
	}
	if selector.id == "" && selector.name == "" {
		return symbolSelector{}, errors.New("a symbol selector requires symbol_id, name, type, method, or field")
	}
	return selector, nil
}

// restrictKind narrows the selector to an explicit kind, which must agree with the derived kinds.
func (selector *symbolSelector) restrictKind(kind string) error {
	if kind == "" {
		return nil
	}
	allowed := symbolKinds
	if len(selector.kinds) > 0 {
		allowed = selector.kinds
	}
	if !slices.Contains(allowed, kind) {
		return fmt.Errorf("kind %q is not one of %v", kind, allowed)
	}
	selector.kinds = []string{kind}
	return nil
}

// lookup reads the symbols matching the selector; a name is looked up through the search_name index.
func (index *indexContext) lookup(ctx context.Context, selector symbolSelector) ([]storage.Symbol, error) {
	query := index.database.WithContext(ctx).Model(&storage.Symbol{})
	if selector.id != "" {
		query = query.Where("id = ?", selector.id)
	}
	if selector.name != "" {
		query = query.Where("search_name = ? AND name = ?", storage.SearchName(selector.name), selector.name)
	}
	if selector.module != "" {
		query = query.Where("module_key = ?", selector.module)
	}
	if selector.pkg != "" {
		query = query.Where("package_path = ?", selector.pkg)
	}
	if len(selector.kinds) > 0 {
		query = query.Where("kind IN ?", selector.kinds)
	}
	if selector.owner != "" {
		owners := index.database.WithContext(ctx).Model(&storage.Symbol{}).Select("id").Where("search_name = ? AND name = ?", storage.SearchName(selector.owner), selector.owner)
		if selector.pkg != "" {
			owners = owners.Where("package_path = ?", selector.pkg)
		}
		query = query.Where("owner_id IN (?)", owners)
	}
	var rows []storage.Symbol
	if err := query.Order("id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("look up symbols: %w", err)
	}
	return rows, nil
}

// resolve returns the symbols the selector names. When it names several, only those with a posting
// in an active document of the scope remain, so retained identities of other versions drop out.
func (index *indexContext) resolve(ctx context.Context, selector symbolSelector) ([]ModuleSymbol, error) {
	rows, err := index.lookup(ctx, selector)
	if err != nil {
		return nil, err
	}
	if len(rows) > 1 {
		postings, err := index.postings(ctx, symbolIDs(rows), "definition", "reference", "implements")
		if err != nil {
			return nil, err
		}
		used := map[string]bool{}
		for _, posting := range postings {
			used[posting.symbol] = true
		}
		rows = slices.DeleteFunc(rows, func(row storage.Symbol) bool { return !used[row.ID] })
	}
	return index.moduleSymbols(ctx, rows)
}

// moduleSymbols converts symbol rows, naming each one's owner.
func (index *indexContext) moduleSymbols(ctx context.Context, rows []storage.Symbol) ([]ModuleSymbol, error) {
	var ownerIDs []string
	for _, row := range rows {
		if row.OwnerID != nil {
			ownerIDs = append(ownerIDs, *row.OwnerID)
		}
	}
	owners := map[string]string{}
	for start := 0; start < len(ownerIDs); start += lookupBatch {
		var found []storage.Symbol
		if err := index.database.WithContext(ctx).Select("id", "name").Where("id IN ?", ownerIDs[start:min(start+lookupBatch, len(ownerIDs))]).Find(&found).Error; err != nil {
			return nil, fmt.Errorf("load symbol owners: %w", err)
		}
		for _, owner := range found {
			owners[owner.ID] = owner.Name
		}
	}
	symbols := make([]ModuleSymbol, 0, len(rows))
	for _, row := range rows {
		symbol := ModuleSymbol{
			ID: row.ID, ModuleKey: row.ModuleKey, PackagePath: row.PackagePath, Kind: row.Kind, Name: row.Name,
			Visibility: row.Visibility, ParameterTypes: row.ParameterTypes,
		}
		if row.OwnerID != nil {
			name, found := owners[*row.OwnerID]
			if !found {
				return nil, fmt.Errorf("symbol %s names owner %s, which is not a symbol", row.ID, *row.OwnerID)
			}
			symbol.OwnerID, symbol.Owner = *row.OwnerID, name
		}
		symbols = append(symbols, symbol)
	}
	return symbols, nil
}

func symbolIDs(rows []storage.Symbol) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func sameParameters(left, right storage.JSON) (bool, error) {
	var leftTypes, rightTypes []string
	if err := json.Unmarshal(left, &leftTypes); err != nil {
		return false, fmt.Errorf("decode parameter types %s: %w", left, err)
	}
	if err := json.Unmarshal(right, &rightTypes); err != nil {
		return false, fmt.Errorf("decode parameter types %s: %w", right, err)
	}
	return slices.Equal(leftTypes, rightTypes), nil
}
