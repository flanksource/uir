package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
)

type ModuleScopeOptions struct {
	RootKey    string
	Location   string
	SnapshotID string
	Limit      int
}

type ModuleMatch struct {
	Kind       string         `json:"kind"`
	RootKey    string         `json:"root_key"`
	Location   string         `json:"location"`
	SnapshotID string         `json:"snapshot_id"`
	Path       string         `json:"path"`
	Line       *int           `json:"line,omitempty"`
	Column     *int           `json:"column,omitempty"`
	Identifier uir.Identifier `json:"identifier"`
}

type ModuleQueryResult struct {
	Operation Operation         `json:"operation"`
	Matches   []ModuleMatch     `json:"matches"`
	Stages    []ResolutionStage `json:"stages"`
}

type moduleScope struct {
	root     storage.ModuleRoot
	location storage.ModuleLocation
	snapshot storage.ModuleSnapshot
}

type projectedNode struct {
	Identifier uir.Identifier
	StartLine  *int
	Column     *int
}

type projectedCall struct {
	FromIdentity string
	ToIdentifier uir.Identifier
	Resolvable   bool
	StartLine    *int
	Column       *int
}

type sourceProjection struct {
	PackagePath string
	Nodes       []projectedNode
	Calls       []projectedCall
}

type moduleCall struct {
	fromIdentity string
	toIdentifier uir.Identifier
	resolvable   bool
	match        ModuleMatch
}

type indexedModuleScope struct {
	nodes      []ModuleMatch
	calls      []moduleCall
	byIdentity map[string][]ModuleMatch
}

type moduleTargetIndex struct {
	exact    map[string][]ModuleMatch
	callable map[string][]ModuleMatch
}

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
	allHeads := parsed.Operation == OperationCallers || parsed.Operation == OperationCallees
	scopes, err := pipeline.moduleScopes(ctx, options, allHeads)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	result := ModuleQueryResult{Operation: parsed.Operation, Matches: []ModuleMatch{}, Stages: []ResolutionStage{{Name: "parse", Value: string(parsed.Operation)}, {Name: "scope", Value: fmt.Sprintf("%d snapshots", len(scopes))}}}
	indexed := make([]indexedModuleScope, 0, len(scopes))
	for _, scope := range scopes {
		loaded, err := pipeline.loadModuleScope(ctx, scope)
		if err != nil {
			return ModuleQueryResult{}, err
		}
		indexed = append(indexed, loaded)
	}
	targets := newModuleTargetIndex(indexed)
	switch parsed.Operation {
	case OperationNodes:
		for _, scope := range indexed {
			for _, node := range scope.nodes {
				if matchesPredicates(node, predicates) {
					result.Matches = append(result.Matches, node)
				}
			}
		}
	case OperationCallers, OperationCallees:
		if len(predicates) == 0 {
			return ModuleQueryResult{}, errors.New("call queries require at least one symbol predicate in addition to root")
		}
		candidates := make([]ModuleMatch, 0)
		for _, scope := range indexed {
			for _, node := range scope.nodes {
				if matchesPredicates(node, predicates) {
					candidates = append(candidates, node)
				}
			}
		}
		if len(candidates) == 0 {
			return ModuleQueryResult{}, errors.New("target selector matched no nodes")
		}
		if len(candidates) > 1 {
			for _, candidate := range candidates {
				candidate.Kind = "candidate"
				result.Matches = append(result.Matches, candidate)
			}
			break
		}
		result.Matches = graphMatches(parsed.Operation, candidates[0], indexed, targets)
	case OperationUnresolvedCalls:
		for _, scope := range indexed {
			for _, call := range scope.calls {
				if !call.resolvable || len(targets.resolve(call.toIdentifier)) == 0 {
					result.Matches = append(result.Matches, call.match)
				}
			}
		}
	default:
		return ModuleQueryResult{}, fmt.Errorf("unsupported UIR operation %q", parsed.Operation)
	}
	sort.Slice(result.Matches, func(i, j int) bool {
		left, right := result.Matches[i], result.Matches[j]
		return left.RootKey+"\x00"+left.Location+"\x00"+left.Path+"\x00"+left.Identifier.IdentityKey() <
			right.RootKey+"\x00"+right.Location+"\x00"+right.Path+"\x00"+right.Identifier.IdentityKey()
	})
	if len(result.Matches) > options.Limit {
		result.Matches = result.Matches[:options.Limit]
	}
	result.Stages = append(result.Stages, ResolutionStage{Name: "execute", Value: fmt.Sprintf("%d rows", len(result.Matches))})
	return result, nil
}

func (pipeline *Pipeline) loadModuleScope(ctx context.Context, scope moduleScope) (indexedModuleScope, error) {
	sources, err := storage.EffectiveSources(ctx, pipeline.database, scope.snapshot.ID)
	if err != nil {
		return indexedModuleScope{}, err
	}
	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := indexedModuleScope{byIdentity: map[string][]ModuleMatch{}}
	for _, path := range paths {
		revision := sources[path]
		var projection sourceProjection
		if err := json.Unmarshal(revision.Projection, &projection); err != nil {
			return indexedModuleScope{}, fmt.Errorf("decode projection for %s at %s: %w", path, scope.snapshot.ID, err)
		}
		if projection.PackagePath != revision.PackagePath {
			return indexedModuleScope{}, fmt.Errorf("projection for %s has package %q, expected %q", path, projection.PackagePath, revision.PackagePath)
		}
		for _, node := range projection.Nodes {
			match := ModuleMatch{
				Kind: "node", RootKey: scope.root.RootKey, Location: scope.location.CanonicalPath,
				SnapshotID: scope.snapshot.ID.String(), Path: path, Line: node.StartLine, Column: node.Column,
				Identifier: node.Identifier,
			}
			result.nodes = append(result.nodes, match)
			key := match.Identifier.IdentityKey()
			result.byIdentity[key] = append(result.byIdentity[key], match)
		}
		for _, call := range projection.Calls {
			result.calls = append(result.calls, moduleCall{
				fromIdentity: call.FromIdentity, toIdentifier: call.ToIdentifier, resolvable: call.Resolvable,
				match: ModuleMatch{
					Kind: "unresolved_call", RootKey: scope.root.RootKey, Location: scope.location.CanonicalPath,
					SnapshotID: scope.snapshot.ID.String(), Path: path, Line: call.StartLine, Column: call.Column,
					Identifier: call.ToIdentifier,
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

func graphMatches(operation Operation, target ModuleMatch, scopes []indexedModuleScope, targets moduleTargetIndex) []ModuleMatch {
	matches := []ModuleMatch{}
	for _, scope := range scopes {
		for _, call := range scope.calls {
			if operation == OperationCallers {
				if !callMatches(call.toIdentifier, target.Identifier) {
					continue
				}
				for _, node := range scope.byIdentity[call.fromIdentity] {
					node.Kind = "caller"
					matches = append(matches, node)
				}
				continue
			}
			if call.fromIdentity != target.Identifier.IdentityKey() {
				continue
			}
			for _, node := range targets.resolve(call.toIdentifier) {
				node.Kind = "callee"
				matches = append(matches, node)
			}
		}
	}
	return matches
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

func callMatches(call, candidate uir.Identifier) bool {
	if call.IdentityKey() == candidate.IdentityKey() {
		return true
	}
	if call.Signature != "" {
		return false
	}
	candidate.Signature = ""
	return call.IdentityKey() == candidate.IdentityKey()
}
