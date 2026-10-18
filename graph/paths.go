package graph

// Paths are, for each target the graph drew, the node ids of one shortest walk to it from a root, root
// first. Edges are walked from From to To, as a callees graph draws them, in the graph's edge order, so
// of two equally short walks the one through the earlier edge is kept. A target the walk does not
// reach has no entry.
func (g Graph) Paths(targets []string) map[string][]string {
	next := map[string][]string{}
	for _, edge := range g.Edges {
		next[edge.From] = append(next[edge.From], edge.To)
	}
	parent := map[string]string{}
	queue := make([]string, 0, len(g.Roots))
	for _, root := range g.Roots {
		if _, seen := parent[root]; !seen {
			parent[root] = ""
			queue = append(queue, root)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, to := range next[id] {
			if _, seen := parent[to]; !seen {
				parent[to] = id
				queue = append(queue, to)
			}
		}
	}
	paths := map[string][]string{}
	for _, target := range targets {
		if _, reached := parent[target]; !reached {
			continue
		}
		var path []string
		for id := target; id != ""; id = parent[id] {
			path = append([]string{id}, path...)
		}
		paths[target] = path
	}
	return paths
}
