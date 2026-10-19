package query

import (
	"context"
	"errors"
	"slices"

	"github.com/flanksource/uir/graph"
)

func (pipeline *Pipeline) previousGraph(ctx context.Context, request graphBuild) (*graphWalk, error) {
	options := request.options
	options.Since, options.Selector, options.Symbol = nil, "", request.root.ID
	scope, err := pipeline.moduleScopeForSnapshot(ctx, request.since.from)
	if err != nil {
		return nil, err
	}
	scopes := slices.Clone(request.scopes)
	for position := range scopes {
		if scopes[position].root.ID == scope.root.ID {
			scopes[position] = scope
		}
	}
	coverage, err := pipeline.scopeCoverage(ctx, scopes)
	if err != nil {
		return nil, err
	}
	base, err := newIndexContext(ctx, pipeline, scopes)
	if err != nil {
		return nil, err
	}
	index := newCompactIndex(base)
	roots, err := index.graphRoots(ctx, options, coverage)
	var missing *UnresolvedSymbolError
	if errors.As(err, &missing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	previous, err := buildIndexedGraph(ctx, index, graphBuild{root: roots[0], options: options, exclusions: request.exclusions})
	if err != nil {
		return nil, err
	}
	return &previous, nil
}

func (changes *GraphChanges) previousNodes(before *graph.Graph, symbols map[int64]GraphChangeReason, handles map[string]int64) {
	if before == nil {
		return
	}
	reported := make(map[string]bool, len(changes.Nodes))
	for _, node := range changes.Nodes {
		reported[node.ID] = true
	}
	for _, node := range before.Nodes {
		handle, indexed := handles[node.ID]
		if reason, changed := symbols[handle]; indexed && changed && !reported[node.ID] {
			changes.Nodes = append(changes.Nodes, GraphChangedNode{ID: node.ID, Reason: reason, SnapshotID: changes.FromSnapshotID})
		}
	}
}

func (sides *sinceSides) edgeChanges(before, after *graph.Graph) []GraphChangedEdge {
	changes := []GraphChangedEdge{}
	compare := func(current, previous *graph.Graph, reason GraphChangeReason, snapshot string) {
		if current == nil {
			return
		}
		known := map[string]bool{}
		if previous != nil {
			for _, edge := range previous.Edges {
				known[edge.ID] = true
			}
		}
		origins := make([]string, 0, len(current.Edges))
		for _, edge := range current.Edges {
			if !known[edge.ID] {
				origins = append(origins, edge.From)
			}
		}
		paths := current.Paths(origins)
		for _, edge := range current.Edges {
			if !known[edge.ID] {
				path := append(append([]string(nil), paths[edge.From]...), edge.To)
				changes = append(changes, GraphChangedEdge{ID: edge.ID, Reason: reason, SnapshotID: snapshot, Path: path})
			}
		}
	}
	compare(after, before, GraphChangeAdded, sides.to.ID.String())
	compare(before, after, GraphChangeRemoved, sides.from.ID.String())
	return changes
}
