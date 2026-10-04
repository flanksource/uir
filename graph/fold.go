package graph

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/uir"
)

// Member places a node inside another, its owner: a column in its table, a
// field in its entity.
type Member struct {
	// Owner is the id of the node the member folds into.
	Owner string
	// Name is the member as the folded edge lists it, e.g. STATUSCODE.
	Name string
	// Property is the edge property listing the folded members, e.g. columns.
	Property string
	// Covers says a site of the member also stands for its owner's site of the
	// same edge type on the same line, as a column read stands for the read of
	// its table: drawn apart, the owner's site there is left out.
	Covers bool
}

// Folding says how FoldMembers draws member nodes.
type Folding struct {
	// MemberOf places a member node; false for any other node.
	MemberOf func(Node) (Member, bool)
	// Owner is the node a member is drawn as when folded, by the id its Member
	// names. It is asked only when Fold is set.
	Owner func(id string) Node
	// Fold draws each member as its owner, the edge naming the members in its
	// Member.Property; otherwise each member is a node of its own.
	Fold bool
}

// Folded is one edge FoldMembers draws, and the steps it stands for.
type Folded struct {
	Step Step
	// From are the indexes of the steps folded into Step, in the order given.
	From []int
}

// FoldMembers folds the steps of one body into one per node and edge type, in
// the order first met. Folding, a member is drawn as its owner. Drawn apart, an
// owner's site on a line where a covering member has a site of the same type
// is left out, and a step left with no site is not drawn. A folded edge has
// each site once, ordered by path, line and column; its Kind is the sorted
// union of the comma-separated kinds of the steps it folds, its Properties the
// first non-empty ones, and Member.Property lists the members it folds,
// sorted. Members of one owner listed under different properties are an
// error: the edge has one list to name them in.
func FoldMembers(steps []Step, folding Folding) ([]Folded, error) {
	covered := map[coveredSite]bool{}
	if !folding.Fold {
		covered = coveredSites(steps, folding.MemberOf)
	}
	held := &foldedSteps{byKey: map[string]*foldedStep{}}
	for i, step := range steps {
		member, isMember := Member{}, false
		if folding.Fold {
			if member, isMember = folding.MemberOf(step.Node); isMember {
				step.Node = folding.Owner(member.Owner)
			}
		} else {
			sites, drawn := uncoveredSites(step, covered)
			if !drawn {
				continue
			}
			step.Edge.Sites = sites
		}
		if err := held.add(i, step.Node, step.Edge, member); err != nil {
			return nil, err
		}
	}
	return held.folded(), nil
}

// Surviving are the indexes of the steps FoldMembers draws when it draws
// members apart, in order: every step but those a covering member stands for
// at each of their sites. A caller that names a node only once it is known to
// be drawn (spelling it, say, as first met) names those alone.
func Surviving(steps []Step, memberOf func(Node) (Member, bool)) []int {
	covered := coveredSites(steps, memberOf)
	var kept []int
	for i, step := range steps {
		if _, drawn := uncoveredSites(step, covered); drawn {
			kept = append(kept, i)
		}
	}
	return kept
}

// uncoveredSites are step's sites that no covering member stands for; false
// when it had sites and none is left.
func uncoveredSites(step Step, covered map[coveredSite]bool) ([]Site, bool) {
	if len(covered) == 0 {
		return step.Edge.Sites, true
	}
	sites := slices.DeleteFunc(slices.Clone(step.Edge.Sites), func(site Site) bool {
		return covered[coveredSite{owner: strings.ToLower(step.Node.ID), typ: step.Edge.Type, line: site.Line}]
	})
	return sites, len(sites) > 0 || len(step.Edge.Sites) == 0
}

// same reports whether two sites are one: the same text at the same position,
// under the same guards.
func (s Site) same(other Site) bool {
	return s.Path == other.Path && s.Line == other.Line && s.Column == other.Column && s.Text == other.Text &&
		slices.Equal(s.Guards, other.Guards)
}

// coveredSite is a line a covering member has a site on, by its owner and the
// edge type.
type coveredSite struct {
	owner string
	typ   uir.RelationshipType
	line  int
}

func coveredSites(steps []Step, memberOf func(Node) (Member, bool)) map[coveredSite]bool {
	covered := map[coveredSite]bool{}
	for _, step := range steps {
		member, ok := memberOf(step.Node)
		if !ok || !member.Covers {
			continue
		}
		for _, site := range step.Edge.Sites {
			covered[coveredSite{owner: strings.ToLower(member.Owner), typ: step.Edge.Type, line: site.Line}] = true
		}
	}
	return covered
}

type foldedStep struct {
	step            Step
	from            []int
	kinds, members  []string
	membersProperty string
}

type foldedSteps struct {
	order []string
	byKey map[string]*foldedStep
}

func (f *foldedSteps) add(index int, node Node, edge Edge, member Member) error {
	key := node.ID + "|" + string(edge.Type)
	held, ok := f.byKey[key]
	if !ok {
		held = &foldedStep{step: Step{Node: node, Edge: Edge{Type: edge.Type}}}
		f.byKey[key] = held
		f.order = append(f.order, key)
	}
	held.from = append(held.from, index)
	for _, site := range edge.Sites {
		if !slices.ContainsFunc(held.step.Edge.Sites, site.same) {
			held.step.Edge.Sites = append(held.step.Edge.Sites, site)
		}
	}
	for kind := range strings.SplitSeq(edge.Kind, ", ") {
		if kind != "" && !slices.Contains(held.kinds, kind) {
			held.kinds = append(held.kinds, kind)
		}
	}
	if len(held.step.Edge.Properties) == 0 && len(edge.Properties) > 0 {
		held.step.Edge.Properties = maps.Clone(edge.Properties)
	}
	if member.Name == "" {
		return nil
	}
	if held.membersProperty != "" && held.membersProperty != member.Property {
		return fmt.Errorf("graph: %s folds members listed as %s and as %s; an owner lists its members under one property",
			node.ID, held.membersProperty, member.Property)
	}
	held.membersProperty = member.Property
	if !slices.Contains(held.members, member.Name) {
		held.members = append(held.members, member.Name)
	}
	return nil
}

func (f *foldedSteps) folded() []Folded {
	out := make([]Folded, 0, len(f.order))
	for _, key := range f.order {
		held := f.byKey[key]
		step := held.step
		step.Edge.Kind = strings.Join(slices.Sorted(slices.Values(held.kinds)), ", ")
		slices.SortStableFunc(step.Edge.Sites, compareSites)
		if len(held.members) > 0 {
			slices.SortFunc(held.members, func(a, b string) int {
				return cmp.Or(cmp.Compare(strings.ToLower(a), strings.ToLower(b)), cmp.Compare(a, b))
			})
			if step.Edge.Properties == nil {
				step.Edge.Properties = map[string]string{}
			}
			step.Edge.Properties[held.membersProperty] = strings.Join(held.members, ", ")
		}
		out = append(out, Folded{Step: step, From: held.from})
	}
	return out
}
