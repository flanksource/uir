package graph

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/uir"
)

// FromRelationships serves call and dispatch relationships as a Source; every
// other relationship type is not a call-graph edge and is ignored.
//
// A node's id is the IdentityKey of the relationship end it stands for. describe
// supplies the rest of each node once per id: FromRelationships sets ID and
// Identifier, and fills Kind (the identifier's node type) and Label (the
// identifier as written) when describe leaves them empty. describe reports an
// identifier it cannot place by returning a Node with Unresolved set.
//
// A call or dispatch relationship missing either end is an error: an edge needs
// both, and uir.CollectRelationships always sets them.
func FromRelationships(rels []uir.UIRRelationship, describe func(uir.Identifier) Node) (Source, error) {
	if describe == nil {
		return nil, errors.New("graph: FromRelationships: describe is required")
	}
	src := &relationshipSource{describe: describe, nodes: map[string]Node{}, out: map[string][]Step{}, in: map[string][]Step{}}
	for i, rel := range rels {
		if rel.RelationshipType != uir.RelationshipTypeCall && rel.RelationshipType != uir.RelationshipTypeDispatch {
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
		edge := Edge{Type: rel.RelationshipType, Sites: []Site{siteOf(rel)}}
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
		return Node{}, fmt.Errorf("graph: no call or dispatch relationship names %q", id)
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
