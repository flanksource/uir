package uir

// NodesOf widens a slice of concrete node values into a slice of Node, so that
// hashing, diffing and traversal can treat every container field uniformly.
func NodesOf[T Node](in []T) []Node {
	out := make([]Node, len(in))
	for i := range in {
		out[i] = in[i]
	}
	return out
}
