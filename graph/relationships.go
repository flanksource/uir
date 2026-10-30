package graph

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/flanksource/uir"
)

// graphRelationships are the relationship types FromRelationships draws.
var graphRelationships = []uir.RelationshipType{
	uir.RelationshipTypeCall, uir.RelationshipTypeDispatch, uir.RelationshipTypeRead, uir.RelationshipTypeWrite,
}

// RelationshipOptions are what FromRelationships serves.
type RelationshipOptions struct {
	// Nodes are declared nodes: Describe answers for each even when no
	// relationship mentions it, so one can be a root with no edges.
	Nodes         []uir.Node
	Relationships []uir.UIRRelationship
	// Describe is required.
	Describe func(uir.Identifier) Node
}

// FromRelationships serves call, dispatch, read and write relationships as a
// Source; every other relationship type is not a call-graph edge and is
// ignored. Options.Access chooses which of them a Build follows.
//
// Each relationship is one step: its Kind is the edge's Kind, and its location,
// Content and guards are the step's one Site.
//
// A node's id is the IdentityKey of the declared node or relationship end it
// stands for. Describe supplies the rest of each node once per id:
// FromRelationships sets ID and Identifier, and fills Kind (the identifier's
// node type) and Label (the identifier as written) when Describe leaves them
// empty. Describe reports an identifier it cannot place by returning a Node with
// Unresolved set.
//
// A nil declared node, and a drawn relationship missing either end, are errors:
// an edge needs both ends, and uir.CollectRelationships always sets them.
func FromRelationships(opts RelationshipOptions) (Source, error) {
	if opts.Describe == nil {
		return nil, errors.New("graph: FromRelationships: describe is required")
	}
	src := &relationshipSource{describe: opts.Describe, nodes: map[string]Node{}, out: map[string][]Step{}, in: map[string][]Step{}}
	for i, node := range opts.Nodes {
		if node == nil {
			return nil, fmt.Errorf("graph: FromRelationships: nodes[%d] is nil", i)
		}
		src.node(node.GetIdentifier())
	}
	for i, rel := range opts.Relationships {
		if !slices.Contains(graphRelationships, rel.RelationshipType) {
			continue
		}
		from, to := rel.GetFrom(), rel.To
		if from == nil {
			return nil, fmt.Errorf("graph: relationships[%d]: %s to %s at %s has no source", i, rel.RelationshipType, endName(to), rel.Location)
		}
		if to == nil {
			return nil, fmt.Errorf("graph: relationships[%d]: %s from %s at %s has no target", i, rel.RelationshipType, endName(from), rel.Location)
		}
		caller, callee := src.node(from.GetIdentifier()), src.node(to.GetIdentifier())
		edge := Edge{Type: rel.RelationshipType, Kind: rel.Kind, Sites: []Site{siteOf(rel)}}
		src.out[caller.ID] = append(src.out[caller.ID], Step{Node: callee, Edge: edge})
		src.in[callee.ID] = append(src.in[callee.ID], Step{Node: caller, Edge: edge})
	}
	return src, nil
}

func endName(node uir.Node) string {
	if node == nil {
		return "<nil>"
	}
	return node.GetIdentifier().String()
}

// siteOf renders each guard to plain text: api.Text.String applies no styling.
func siteOf(rel uir.UIRRelationship) Site {
	site := Site{Path: rel.Path}
	if rel.StartLine != nil {
		site.Line = *rel.StartLine
	}
	if rel.Column != nil {
		site.Column = *rel.Column
	}
	if rel.Content != nil {
		site.Text = *rel.Content
	}
	for _, guard := range rel.Guards {
		site.Guards = append(site.Guards, guard.Pretty().String())
	}
	return site
}

type relationshipSource struct {
	describe func(uir.Identifier) Node
	nodes    map[string]Node
	out      map[string][]Step
	in       map[string][]Step
}

func (s *relationshipSource) node(id uir.Identifier) Node {
	key := id.IdentityKey()
	if node, ok := s.nodes[key]; ok {
		return node
	}
	node := s.describe(id)
	node.ID, node.Identifier = key, id
	if node.Kind == "" {
		node.Kind = string(id.GetNodeType())
	}
	if node.Label == "" {
		node.Label = id.Pretty().String()
	}
	s.nodes[key] = node
	return node
}

func (s *relationshipSource) Describe(_ context.Context, id string) (Node, error) {
	node, ok := s.nodes[id]
	if !ok {
		return Node{}, fmt.Errorf("graph: %q is neither a declared node nor named by a call, dispatch, read or write relationship", id)
	}
	return node, nil
}

func (s *relationshipSource) Out(ctx context.Context, id string) ([]Step, error) {
	if _, err := s.Describe(ctx, id); err != nil {
		return nil, err
	}
	return s.out[id], nil
}

func (s *relationshipSource) In(ctx context.Context, id string) ([]Step, error) {
	if _, err := s.Describe(ctx, id); err != nil {
		return nil, err
	}
	return s.in[id], nil
}
