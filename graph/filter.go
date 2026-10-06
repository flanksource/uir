package graph

import "cmp"

// Filter is the part of a call-graph route's request every frontend spells
// alike, as flags and query parameters: the direction, depth, limit, access
// and columns clicky-ui's CallGraph sends (CallGraphFetchParams). Embed it in
// a route's options beside the root and the frontend's own scope flags.
//
// The exclusions (exclude-kind, exclude-group) and guards flags name the
// frontend's own kinds, groups and conditions, so a route declares them with
// its own help and passes them to ExcludeKindsGroups and FillGuards.
type Filter struct {
	Direction string   `flag:"direction" default:"callees" help:"callees (what the root calls), callers (what calls it) or both"`
	Depth     int      `flag:"depth" help:"How many edges away from the root the graph reaches, 1..8 (0 takes 2)"`
	Limit     int      `flag:"limit" help:"Most nodes the graph holds, 1..1000 (0 takes 150)"`
	Access    []string `flag:"access" help:"Edge types to follow: call (calls and dispatches), read, write (repeatable or comma separated; default all)"`
	Columns   bool     `flag:"columns" help:"Draw each column and entity field as a node of its own instead of folding it into its table or entity"`
}

// Options are the Build options f asks for: zero Depth and Limit take
// DefaultDepth and DefaultLimit, and Access is read by ParseAccess, so no
// access follows every type. Direction is taken as written, for Build to
// validate. Exclude is the route's to set; Columns is the route's to apply
// (FoldMembers).
func (f Filter) Options() (Options, error) {
	access, err := ParseAccess(f.Access)
	if err != nil {
		return Options{}, err
	}
	return Options{
		Direction: Direction(f.Direction),
		Depth:     cmp.Or(f.Depth, DefaultDepth),
		Limit:     cmp.Or(f.Limit, DefaultLimit),
		Access:    access,
	}, nil
}
