package graph_test

import (
	"github.com/flanksource/uir/graph"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Graph paths", func() {
	It("walks the shortest route from a root to each target it reaches, preferring the first edge", func() {
		built := build(calls("a>b", "a>c", "b>d", "c>d", "d>e", "e>b", "x>e"), graph.DirectionCallees, 4, "a")

		Expect(built.Paths([]string{"a", "d", "e", "x", "missing"})).To(Equal(map[string][]string{
			"a": {"a"},
			"d": {"a", "b", "d"},
			"e": {"a", "b", "d", "e"},
		}), "a root is its own path, a diamond takes its first branch, and nodes the walk never drew have none")
	})
})
