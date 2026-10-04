package graph

import (
	"errors"
	"fmt"
	"slices"
)

// ErrUnreadableSource marks the guards of a site that cannot be read from the
// source that holds it: the file is gone, no longer parses, or no longer holds
// the call where the graph drew it.
var ErrUnreadableSource = errors.New("source unreadable")

// FillGuards sets the guards of every site of g that names a path and a line
// to what guards returns for it and its edge, outermost first. A site whose
// guards are unreadable (an error wrapping ErrUnreadableSource) keeps the
// guards its Source gave it, and its path is listed once in
// Omitted.UnreadableSource, in the order met; any other error fails.
func FillGuards(g *Graph, guards func(Edge, Site) ([]string, error)) error {
	for i := range g.Edges {
		edge := &g.Edges[i]
		for j := range edge.Sites {
			site := &edge.Sites[j]
			if site.Path == "" || site.Line == 0 {
				continue
			}
			read, err := guards(*edge, *site)
			switch {
			case errors.Is(err, ErrUnreadableSource):
				if !slices.Contains(g.Omitted.UnreadableSource, site.Path) {
					g.Omitted.UnreadableSource = append(g.Omitted.UnreadableSource, site.Path)
				}
			case err != nil:
				return fmt.Errorf("graph: guards of the %s>%s site at %s:%d: %w", edge.From, edge.To, site.Path, site.Line, err)
			default:
				site.Guards = read
			}
		}
	}
	return nil
}
