package query

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
)

const (
	defaultLimit = 100
	maximumLimit = 1000
)

type ModuleScopeOptions struct {
	RootKey    string
	Location   string
	SnapshotID string
	Limit      int
}

// ModuleMatch is one result row. A declaration's position is its name's range; an occurrence's is
// the identifier's range. Role through Dispatch are set by the index-backed operations: the
// occurrence role, the symbol the row names, the declaration enclosing an occurrence, the coverage
// of the document the row was read from, and whether a caller reaches the target through an interface.
type ModuleMatch struct {
	Kind         string         `json:"kind"`
	RootKey      string         `json:"root_key"`
	Location     string         `json:"location"`
	SnapshotID   string         `json:"snapshot_id"`
	Path         string         `json:"path"`
	Line         *int           `json:"line,omitempty"`
	Column       *int           `json:"column,omitempty"`
	Identifier   uir.Identifier `json:"identifier"`
	EndLine      *int           `json:"end_line,omitempty"`
	EndColumn    *int           `json:"end_column,omitempty"`
	Role         string         `json:"role,omitempty"`
	SymbolID     string         `json:"symbol_id,omitempty"`
	EnclosingID  string         `json:"enclosing_id,omitempty"`
	EnclosingKey string         `json:"enclosing_key,omitempty"`
	Coverage     string         `json:"coverage,omitempty"`
	Dispatch     bool           `json:"dispatch,omitempty"`
}

// ModuleQueryResult holds the rows of one query. Total counts the rows before the limit; Symbols are
// the canonical symbols a selector resolved to; Declarations are the target's definitions, kept apart
// so occurrence rows are never multiplied by declaration locations; Coverage lists every package in
// scope that is not fully indexed, so an empty result is not mistaken for proven absence.
type ModuleQueryResult struct {
	Operation    Operation         `json:"operation"`
	Matches      []ModuleMatch     `json:"matches"`
	Stages       []ResolutionStage `json:"stages"`
	Total        int               `json:"total"`
	Symbols      []ModuleSymbol    `json:"symbols,omitempty"`
	Declarations []ModuleMatch     `json:"declarations,omitempty"`
	Coverage     []ModuleCoverage  `json:"coverage"`
}

type moduleScope struct {
	root     storage.ModuleRoot
	location storage.ModuleLocation
	snapshot storage.ModuleSnapshot
}

type moduleCall struct {
	toIdentifier uir.Identifier
	resolvable   bool
	match        ModuleMatch
}

type indexedModuleScope struct {
	nodes []ModuleMatch
	calls []moduleCall
}

type moduleTargetIndex struct {
	exact    map[string][]ModuleMatch
	callable map[string][]ModuleMatch
}

var (
	documentPredicates = []string{"node_type", "module", "package", "type", "method", "field", "signature", "language", "symbol_key", "identity_key"}
	symbolPredicates   = []string{"symbol_id", "module", "package", "type", "method", "field", "kind", "name", "owner"}
)

func (pipeline *Pipeline) RunModules(ctx context.Context, input string, options ModuleScopeOptions) (ModuleQueryResult, error) {
	if pipeline == nil || pipeline.database == nil {
		return ModuleQueryResult{}, errors.New("UIR query database is required")
	}
	parsed, err := Parse(input)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	rootKey, predicates, err := rootSelector(parsed.Predicates, options.RootKey)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	options.RootKey = rootKey
	if options.Limit == 0 {
		options.Limit = defaultLimit
	}
	if options.Limit < 1 || options.Limit > maximumLimit {
		return ModuleQueryResult{}, fmt.Errorf("UIR query limit must be between 1 and %d, got %d", maximumLimit, options.Limit)
	}
	if options.Location != "" && options.SnapshotID != "" {
		return ModuleQueryResult{}, errors.New("location and snapshot selectors are mutually exclusive")
	}
	if err := checkPredicates(parsed.Operation, predicates); err != nil {
		return ModuleQueryResult{}, err
	}
	scopes, err := pipeline.moduleScopes(ctx, options, parsed.Operation.indexed())
	if err != nil {
		return ModuleQueryResult{}, err
	}
	coverage, err := pipeline.scopeCoverage(ctx, scopes)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	result := ModuleQueryResult{Operation: parsed.Operation, Matches: []ModuleMatch{}, Coverage: coverage, Stages: []ResolutionStage{
		{Name: "parse", Value: string(parsed.Operation)}, {Name: "scope", Value: fmt.Sprintf("%d snapshots", len(scopes))}, coverageStage(coverage),
	}}
	if parsed.Operation.indexed() {
		err = pipeline.runIndexed(ctx, parsed, predicates, scopes, options.Limit, &result)
	} else {
		err = pipeline.runDocuments(ctx, parsed.Operation, predicates, scopes, &result)
	}
	if err != nil {
		return ModuleQueryResult{}, err
	}
	if len(result.Matches) > options.Limit {
		result.Matches = result.Matches[:options.Limit]
	}
	result.Stages = append(result.Stages, ResolutionStage{Name: "execute", Value: fmt.Sprintf("%d rows", len(result.Matches))})
	return result, nil
}

// checkPredicates rejects a predicate the operation cannot evaluate instead of letting it match nothing.
func checkPredicates(operation Operation, predicates []Predicate) error {
	supported := documentPredicates
	if operation.indexed() {
		supported = symbolPredicates
	}
	for _, predicate := range predicates {
		if !slices.Contains(supported, predicate.Field) {
			return fmt.Errorf("predicate %q is not supported by %s; use one of %v", predicate.Field, operation, supported)
		}
	}
	return nil
}

// runDocuments answers nodes and unresolved calls by scanning each primary head's active documents.
func (pipeline *Pipeline) runDocuments(ctx context.Context, operation Operation, predicates []Predicate, scopes []moduleScope, result *ModuleQueryResult) error {
	indexed := make([]indexedModuleScope, 0, len(scopes))
	for _, scope := range scopes {
		loaded, err := pipeline.loadModuleScope(ctx, scope)
		if err != nil {
			return err
		}
		indexed = append(indexed, loaded)
	}
	switch operation {
	case OperationNodes:
		for _, scope := range indexed {
			for _, node := range scope.nodes {
				if matchesPredicates(node, predicates) {
					result.Matches = append(result.Matches, node)
				}
			}
		}
	case OperationUnresolvedCalls:
		targets := newModuleTargetIndex(indexed)
		for _, scope := range indexed {
			for _, call := range scope.calls {
				if !call.resolvable || len(targets.resolve(call.toIdentifier)) == 0 {
					result.Matches = append(result.Matches, call.match)
				}
			}
		}
	default:
		return fmt.Errorf("unsupported UIR operation %q", operation)
	}
	sort.Slice(result.Matches, func(i, j int) bool {
		left, right := result.Matches[i], result.Matches[j]
		return left.RootKey+"\x00"+left.Location+"\x00"+left.Path+"\x00"+left.Identifier.IdentityKey() <
			right.RootKey+"\x00"+right.Location+"\x00"+right.Path+"\x00"+right.Identifier.IdentityKey()
	})
	result.Total = len(result.Matches)
	return nil
}

func (pipeline *Pipeline) loadModuleScope(ctx context.Context, scope moduleScope) (indexedModuleScope, error) {
	documents, err := pipeline.scopeDocuments(ctx, scope)
	if err != nil {
		return indexedModuleScope{}, err
	}
	var result indexedModuleScope
	for _, document := range documents {
		for _, symbol := range document.content.Symbols {
			if !symbol.Explorable() {
				continue
			}
			line, _, column := symbolPosition(symbol)
			result.nodes = append(result.nodes, ModuleMatch{
				Kind: "node", RootKey: scope.root.RootKey, Location: scope.location.CanonicalPath,
				SnapshotID: scope.snapshot.ID.String(), Path: document.path, Line: line, Column: column,
				Identifier: symbol.Identifier,
			})
		}
		for _, occurrence := range document.content.Occurrences {
			if !occurrence.IsCall() {
				continue
			}
			result.calls = append(result.calls, moduleCall{
				toIdentifier: *occurrence.Target, resolvable: occurrence.Resolvable,
				match: ModuleMatch{
					Kind: "unresolved_call", RootKey: scope.root.RootKey, Location: scope.location.CanonicalPath,
					SnapshotID: scope.snapshot.ID.String(), Path: document.path, Line: &occurrence.Range[0], Column: &occurrence.Range[1],
					Identifier: *occurrence.Target,
				},
			})
		}
	}
	return result, nil
}

func matchesPredicates(node ModuleMatch, predicates []Predicate) bool {
	for _, predicate := range predicates {
		value := ""
		switch predicate.Field {
		case "node_type":
			value = string(node.Identifier.GetNodeType())
		case "module":
			value = node.Identifier.Module
		case "package":
			value = node.Identifier.Package
		case "type":
			value = node.Identifier.Type
		case "method":
			value = node.Identifier.Method
		case "field":
			value = node.Identifier.Field
		case "signature":
			value = node.Identifier.Signature
		case "language":
			value = "go"
		case "symbol_key":
			value = node.Identifier.SymbolKey()
		case "identity_key":
			value = node.Identifier.IdentityKey()
		}
		if value != predicate.Value {
			return false
		}
	}
	return true
}

func newModuleTargetIndex(scopes []indexedModuleScope) moduleTargetIndex {
	index := moduleTargetIndex{exact: map[string][]ModuleMatch{}, callable: map[string][]ModuleMatch{}}
	for _, scope := range scopes {
		for _, node := range scope.nodes {
			identifier := node.Identifier
			index.exact[identifier.IdentityKey()] = append(index.exact[identifier.IdentityKey()], node)
			identifier.Signature = ""
			index.callable[identifier.IdentityKey()] = append(index.callable[identifier.IdentityKey()], node)
		}
	}
	return index
}

func (index moduleTargetIndex) resolve(identifier uir.Identifier) []ModuleMatch {
	if identifier.Signature != "" {
		return index.exact[identifier.IdentityKey()]
	}
	return index.callable[identifier.IdentityKey()]
}

func canonicalLocation(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve location %q: %w", path, err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("canonicalize location %q: %w", path, err)
	}
	return canonical, nil
}
