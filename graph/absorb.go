package graph

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// Absorb serves src with the nodes absorbedInto names a parent for folded into
// another node, never drawn themselves: a structural part whose node says
// nothing its parent's does not. absorbedInto returns the id of the node an
// absorbed node is drawn as among the callers, and false for a node that is
// drawn as itself, as a root always should be. An absorbed node with no parent
// is absorbed among callees only: met as a caller, it fails the graph.
//
// Among a node's callees, a step to an absorbed node is replaced, in place, by
// that node's own callees, drawn from the node that listed it; their sites keep
// the absorbed node's path. A site such a callee shares with one already
// absorbed into the same node, because two of its steps reach the absorbed
// node, is drawn once. Among a node's callers, an absorbed caller is drawn as
// the node its parent id describes.
func Absorb(src Source, absorbedInto func(Node) (parent string, absorbed bool, err error)) Source {
	return &absorbingSource{src: src, absorbedInto: absorbedInto}
}

type absorbingSource struct {
	src          Source
	absorbedInto func(Node) (string, bool, error)
}

func (s *absorbingSource) Describe(ctx context.Context, id string) (Node, error) {
	return s.src.Describe(ctx, id)
}

// Out is id's callees with absorbed nodes replaced by their own. An absorbed
// node whose callees reach, through absorbed nodes only, a node being expanded
// is a cycle no drawing can end, and fails the graph.
func (s *absorbingSource) Out(ctx context.Context, id string) ([]Step, error) {
	return s.out(ctx, id, []string{id})
}

// out lists id's callees, expanding is the chain of nodes whose callees are
// being listed, id last.
func (s *absorbingSource) out(ctx context.Context, id string, expanding []string) ([]Step, error) {
	steps, err := s.src.Out(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]Step, 0, len(steps))
	drawn := map[drawnSite]bool{}
	for _, step := range steps {
		_, absorbed, err := s.absorbedInto(step.Node)
		if err != nil {
			return nil, fmt.Errorf("graph: absorbing %q into %q: %w", step.Node.ID, id, err)
		}
		if !absorbed {
			out = append(out, step)
			continue
		}
		if slices.Contains(expanding, step.Node.ID) {
			return nil, fmt.Errorf("graph: absorbing %q into %q: it is absorbed into itself through %s",
				step.Node.ID, id, strings.Join(append(slices.Clone(expanding), step.Node.ID), " > "))
		}
		inner, err := s.out(ctx, step.Node.ID, append(slices.Clone(expanding), step.Node.ID))
		if err != nil {
			return nil, fmt.Errorf("graph: callees of %q, absorbed into %q: %w", step.Node.ID, id, err)
		}
		for _, absorbedStep := range inner {
			if absorbedStep, ok := withoutDrawnSites(absorbedStep, drawn); ok {
				out = append(out, absorbedStep)
			}
		}
	}
	return out, nil
}

func (s *absorbingSource) In(ctx context.Context, id string) ([]Step, error) {
	listed, err := s.src.In(ctx, id)
	if err != nil {
		return nil, err
	}
	steps := slices.Clone(listed)
	for i, step := range steps {
		parent, absorbed, err := s.absorbedInto(step.Node)
		if err != nil {
			return nil, fmt.Errorf("graph: absorbing %q, a caller of %q: %w", step.Node.ID, id, err)
		}
		if !absorbed {
			continue
		}
		if parent == "" {
			return nil, fmt.Errorf("graph: %q, a caller of %q, is absorbed into no node it can be drawn as", step.Node.ID, id)
		}
		if steps[i].Node, err = s.src.Describe(ctx, parent); err != nil {
			return nil, fmt.Errorf("graph: %q, which %q is absorbed into: %w", parent, step.Node.ID, err)
		}
	}
	return steps, nil
}

// drawnSite is one site of an edge to a node, as Absorb tells a node absorbed
// twice apart.
type drawnSite struct {
	to, edgeType, path, text string
	line, column             int
}

// withoutDrawnSites is step without the sites drawn already, noting its own;
// false when every site it had was drawn. A step with no sites is kept.
func withoutDrawnSites(step Step, drawn map[drawnSite]bool) (Step, bool) {
	if len(step.Edge.Sites) == 0 {
		return step, true
	}
	sites := make([]Site, 0, len(step.Edge.Sites))
	for _, site := range step.Edge.Sites {
		key := drawnSite{to: step.Node.ID, edgeType: string(step.Edge.Type), path: site.Path, text: site.Text, line: site.Line, column: site.Column}
		if !drawn[key] {
			drawn[key] = true
			sites = append(sites, site)
		}
	}
	if len(sites) == 0 {
		return Step{}, false
	}
	step.Edge.Sites = sites
	return step, true
}
