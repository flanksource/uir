package graph_test

import (
	"strings"

	"github.com/flanksource/uir/graph"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Graph direction overlap", func() {
	It("continues both independent walks when a cycle reaches the same node from both sides", func() {
		result := shapeOf(build(calls("a>b", "b>a", "b>c", "d>b", "x>c"), graph.DirectionBoth, 2, "a"))
		Expect(result.Edges).To(ConsistOf("a|b|call", "b|a|call", "b|c|call", "d|b|call"))
		Expect(result.Nodes).To(ContainElement("d@-2 in=0 out=1"))
		Expect(result.Nodes).NotTo(ContainElement(ContainSubstring("x@")))
	})

	It("renders both directional subtrees when a cycle shares a node between them", func() {
		result := build(calls("a>b", "b>a", "b>c", "d>b"), graph.DirectionBoth, 2, "a")
		text := strings.Join(treeLines(result.Pretty()), "\n")
		Expect(text).To(ContainSubstring("ƒ c"))
		Expect(text).To(ContainSubstring("ƒ d"))
	})
})
