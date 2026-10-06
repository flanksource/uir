package graph

import "github.com/flanksource/commons/collections"

// ExcludeKindsGroups is an Options.Exclude that leaves out a node whose Kind
// matches one of kinds or whose Group matches one of groups. Both compare
// case-insensitively and whole: a pattern matches only the value it spells,
// or what its * wildcards stand for, so function never matches sqlfunction.
// A pattern may be a comma-separated list, and a ! pattern negates, as
// collections.MatchItems reads them. It is nil, excluding nothing, when kinds
// and groups are both empty.
func ExcludeKindsGroups(kinds, groups []string) func(Node) bool {
	if len(kinds) == 0 && len(groups) == 0 {
		return nil
	}
	return func(node Node) bool {
		return (len(kinds) > 0 && collections.MatchItems(node.Kind, kinds...)) ||
			(len(groups) > 0 && collections.MatchItems(node.Group, groups...))
	}
}
