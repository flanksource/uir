package query

import (
	"context"
	"fmt"
	"strconv"

	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/storage"
)

type nativeGraphSource struct {
	index   *compactIndex
	base    *indexGraphSource
	catalog storage.DomainCatalog
	via     []string
}

func (source *nativeGraphSource) Describe(ctx context.Context, id string) (graph.Node, error) {
	node, err := source.base.Describe(ctx, id)
	if err != nil {
		return node, err
	}
	declarations, err := source.index.declarations(ctx, []string{id}, "definition")
	if err != nil {
		return node, err
	}
	if len(declarations) > 0 {
		node.Props = cloneProperties(declarations[0].Props)
	}
	return node, nil
}

func (source *nativeGraphSource) Out(ctx context.Context, id string) ([]graph.Step, error) {
	return source.steps(ctx, id, false)
}
func (source *nativeGraphSource) In(ctx context.Context, id string) ([]graph.Step, error) {
	return source.steps(ctx, id, true)
}

func (source *nativeGraphSource) steps(ctx context.Context, id string, incoming bool) ([]graph.Step, error) {
	symbols, err := source.index.symbolsByID(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	matches, err := source.index.nativeMatches(ctx, symbols, incoming, source.via)
	if err != nil {
		return nil, err
	}
	steps := make([]graph.Step, 0, len(matches))
	for _, match := range matches {
		relationship := match.Relationship
		target := match.SymbolID
		if incoming {
			target = match.EnclosingID
		}
		var node graph.Node
		if target == "" {
			node = graph.Node{ID: "unresolved:" + relationship.Source + ":" + relationship.Kind + ":" + relationship.Reference, Kind: "unresolved", Label: relationship.Reference, Unresolved: true, Group: symbols[0].PackagePath}
		} else {
			node, err = source.Describe(ctx, target)
			if err != nil {
				return nil, err
			}
		}
		kind, ok := source.catalog.Relationships[relationship.Kind]
		if !ok {
			return nil, fmt.Errorf("document uses unregistered relationship kind %s", relationship.Kind)
		}
		site := graph.Site{Path: match.Path, Line: *match.Line, Column: *match.Column, Text: relationship.Evidence.Text, Resolution: relationship.Resolution, Reference: relationship.Reference, Targets: relationship.Targets, Props: cloneProperties(relationship.Props)}
		_, active, err := source.base.sources.located(match.SnapshotID, match.Path)
		if err != nil {
			return nil, err
		}
		site.Location = &graph.Location{RootKey: match.RootKey, CheckoutPath: match.Location, SnapshotID: match.SnapshotID, SourceID: active.Source.ID.String(), Path: match.Path, Line: *match.Line, Column: *match.Column}
		edge := graph.Edge{Type: kind.Type, Kind: kind.Name, Sites: []graph.Site{site}, Props: cloneProperties(relationship.Props), Properties: map[string]string{"resolution": relationship.Resolution}}
		if relationship.Reference != "" {
			edge.Properties["reference"] = relationship.Reference
		}
		if len(relationship.Targets) > 1 {
			edge.Properties["candidates"] = strconv.Itoa(len(relationship.Targets))
		}
		steps = append(steps, graph.Step{Node: node, Edge: edge})
	}
	return steps, nil
}

func (pipeline *Pipeline) sqlNativeGraph(ctx context.Context, index *compactIndex, value compactValue, source *sqlSource, direction graph.Direction, result ModuleQueryResult) (ModuleQueryResult, error) {
	if len(value.symbols) == 0 {
		return ModuleQueryResult{}, sqlError(source.pos, "graph source matched no symbols")
	}
	catalog, err := storage.LoadDomainCatalog(ctx, pipeline.database)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	sources := newScopeSources(index.indexContext)
	base := newIndexGraphSource(index, sources, newGuardReader(pipeline.database, sources), nil)
	native := &nativeGraphSource{index: index, base: base, catalog: catalog, via: source.options.via}
	exclude := source.options.exclude
	if len(exclude) == 0 {
		exclude = DefaultGraphExclusions
	}
	exclusions, err := parseGraphExclusions(exclude)
	if err != nil {
		return ModuleQueryResult{}, err
	}
	ids := make([]string, 0, len(value.symbols))
	for _, symbol := range value.symbols {
		ids = append(ids, symbol.ID)
	}
	built, err := graph.Build(ctx, native, ids, graph.Options{Direction: direction, Depth: source.options.depth, Limit: source.options.nodes, Exclude: func(node graph.Node) bool {
		if node.Unresolved {
			return false
		}
		return exclusions.excludes(base.packageFacts(node.Group))
	}})
	if err != nil {
		return ModuleQueryResult{}, err
	}
	result.Graph = &GraphResult{Graph: *built, Exclude: exclude, Packages: graphPackages(built, base.packages, exclusions), Stages: result.Stages, Candidates: []ModuleSymbol{}}
	result.Symbols = value.symbols
	result.Total = len(built.Nodes)
	return result, nil
}
