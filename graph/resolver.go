package graph

import (
	"context"
	"fmt"
	"maps"

	"github.com/flanksource/uir"
)

// ResolutionStatus is how a reference made by name resolved.
type ResolutionStatus string

const (
	// ResolutionStatic is a name every execution resolves to one target.
	ResolutionStatic ResolutionStatus = "static"
	// ResolutionDynamic is a name each execution may resolve differently: each
	// target is one implementation, its Properties naming what selects it.
	ResolutionDynamic ResolutionStatus = "dynamic"
	// ResolutionAmbiguous is a name several candidates rank equally for, none
	// of them chosen.
	ResolutionAmbiguous ResolutionStatus = "ambiguous"
	// ResolutionUnresolved is a name nothing resolves.
	ResolutionUnresolved ResolutionStatus = "unresolved"
)

// Target is one node a reference may reach, and what the edge to it carries.
type Target struct {
	Node       Node
	Properties map[string]string
}

// Resolution is what a reference made by name may reach.
type Resolution struct {
	Status ResolutionStatus
	// Targets are the one target of a static reference, or every
	// implementation of a dynamic one.
	Targets []Target
	// Candidates are the equally ranked targets of an ambiguous reference.
	Candidates []Target
}

// Reference is one name a body refers to, at its sites, and how it resolved.
type Reference struct {
	// Kind is the kind of reference, which every edge it draws carries.
	Kind  string
	Name  string
	Sites []Site
	Resolution
}

// Referrer is a reference another node's body makes.
type Referrer struct {
	Node      Node
	Reference Reference
}

// Resolver is a Source whose edges are references made by name and resolved
// the way the language dispatches them; FromResolver draws them.
type Resolver interface {
	Describe(ctx context.Context, id string) (Node, error)
	// References are the references the body of id makes, each resolved as it
	// executes there.
	References(ctx context.Context, id string) ([]Reference, error)
	// Referrers are the references other bodies make that may reach id, each
	// with the node making it. One that does not reach id is not drawn.
	Referrers(ctx context.Context, id string) ([]Referrer, error)
}

// FromResolver serves a Resolver as a Source. A node's callees are the steps
// of its references (Reference.Steps), and its callers each referrer whose
// reference reaches it, with the edge its callees draw (Reference.EdgeTo).
func FromResolver(resolver Resolver) Source {
	return resolverSource{resolver}
}

type resolverSource struct {
	resolver Resolver
}

func (s resolverSource) Describe(ctx context.Context, id string) (Node, error) {
	return s.resolver.Describe(ctx, id)
}

func (s resolverSource) Out(ctx context.Context, id string) ([]Step, error) {
	refs, err := s.resolver.References(ctx, id)
	if err != nil {
		return nil, err
	}
	var steps []Step
	for _, ref := range refs {
		drawn, err := ref.Steps()
		if err != nil {
			return nil, fmt.Errorf("references of %q: %w", id, err)
		}
		steps = append(steps, drawn...)
	}
	return steps, nil
}

func (s resolverSource) In(ctx context.Context, id string) ([]Step, error) {
	referrers, err := s.resolver.Referrers(ctx, id)
	if err != nil {
		return nil, err
	}
	var steps []Step
	for _, referrer := range referrers {
		edge, reaches, err := referrer.Reference.EdgeTo(id)
		if err != nil {
			return nil, fmt.Errorf("references of %q: %w", referrer.Node.ID, err)
		}
		if reaches {
			steps = append(steps, Step{Node: referrer.Node, Edge: edge})
		}
	}
	return steps, nil
}

// Steps are the edges a reference draws from the body that makes it: a call
// to the target of a static reference, a dispatch to each implementation of a
// dynamic one and to each candidate of an ambiguous one (whose properties
// carry status=ambiguous), and a call to the UnresolvedID(Kind, Name) leaf of
// an unresolved one.
func (r Reference) Steps() ([]Step, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if r.Status == ResolutionUnresolved {
		leaf := Node{ID: UnresolvedID(r.Kind, r.Name), Kind: r.Kind, Label: r.Name, Unresolved: true}
		return []Step{{Node: leaf, Edge: Edge{Type: uir.RelationshipTypeCall, Kind: r.Kind, Sites: r.Sites}}}, nil
	}
	targets := r.Targets
	if r.Status == ResolutionAmbiguous {
		targets = r.Candidates
	}
	steps := make([]Step, 0, len(targets))
	for _, target := range targets {
		steps = append(steps, Step{Node: target.Node, Edge: r.edge(target)})
	}
	return steps, nil
}

// EdgeTo is the edge the reference draws to the node id, as Steps draws it;
// false when no target or candidate of it is id. An unresolved reference
// reaches nothing.
func (r Reference) EdgeTo(id string) (Edge, bool, error) {
	if err := r.validate(); err != nil {
		return Edge{}, false, err
	}
	targets := r.Targets
	if r.Status == ResolutionAmbiguous {
		targets = r.Candidates
	}
	for _, target := range targets {
		if target.Node.ID == id {
			return r.edge(target), true, nil
		}
	}
	return Edge{}, false, nil
}

// edge is the edge to one target of a resolved reference.
func (r Reference) edge(target Target) Edge {
	edge := Edge{Type: uir.RelationshipTypeCall, Kind: r.Kind, Sites: r.Sites, Properties: target.Properties}
	switch r.Status {
	case ResolutionDynamic:
		edge.Type = uir.RelationshipTypeDispatch
	case ResolutionAmbiguous:
		edge.Type = uir.RelationshipTypeDispatch
		edge.Properties = maps.Clone(target.Properties)
		if edge.Properties == nil {
			edge.Properties = map[string]string{}
		}
		edge.Properties["status"] = string(ResolutionAmbiguous)
	}
	return edge
}

func (r Reference) validate() error {
	switch r.Status {
	case ResolutionStatic:
		if len(r.Targets) != 1 {
			return fmt.Errorf("graph: %s %q is static with %d targets, not one", r.Kind, r.Name, len(r.Targets))
		}
	case ResolutionDynamic, ResolutionAmbiguous, ResolutionUnresolved:
	default:
		return fmt.Errorf("graph: %s %q has resolution status %q, not static, dynamic, ambiguous or unresolved", r.Kind, r.Name, r.Status)
	}
	return nil
}
