package graph_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir/graph"
)

var _ = Describe("ExcludeKindsGroups", func() {
	It("excludes nothing, as a nil Exclude, when no kind or group is named", func() {
		Expect(graph.ExcludeKindsGroups(nil, []string{})).To(BeNil())
	})

	DescribeTable("leaves out a node whose kind or group matches, case-insensitively and whole, with * wildcards",
		func(kinds, groups []string, node graph.Node, want bool) {
			exclude := graph.ExcludeKindsGroups(kinds, groups)
			Expect(exclude).NotTo(BeNil())
			Expect(exclude(node)).To(Equal(want))
		},
		Entry("a kind in another case", []string{"CopyBook"}, nil, graph.Node{Kind: "copybook"}, true),
		Entry("a kind matches only itself", []string{"function"}, nil, graph.Node{Kind: "sqlfunction"}, false),
		Entry("a kind by a prefix wildcard", []string{"*function"}, nil, graph.Node{Kind: "sqlfunction"}, true),
		Entry("a group", nil, []string{"Global"}, graph.Node{Kind: "copybook", Group: "Global"}, true),
		Entry("a group by a suffix wildcard", nil, []string{"Plan · *"}, graph.Node{Group: "Plan · PlanA"}, true),
		Entry("a kind named, a group not", []string{"table"}, nil, graph.Node{Kind: "column", Group: "table"}, false),
		Entry("a group named, a kind not", nil, []string{"Global"}, graph.Node{Kind: "Global", Group: "Plan · PlanA"}, false),
		Entry("a kind in a comma list", []string{"table,column"}, nil, graph.Node{Kind: "column"}, true),
		Entry("a kind outside a comma list", []string{"table,column"}, nil, graph.Node{Kind: "entity"}, false),
		Entry("a kind a lone negation does not name: every other kind", []string{"!table"}, nil, graph.Node{Kind: "column"}, true),
		Entry("the kind a lone negation names", []string{"!table"}, nil, graph.Node{Kind: "table"}, false),
		Entry("a negation over a wildcard", []string{"*", "!Global"}, nil, graph.Node{Kind: "global"}, false),
		Entry("a group a negated group list does not name", nil, []string{"!Global,!Plan · *"}, graph.Node{Group: "Product · P"}, true),
		Entry("a group a negated group list names", nil, []string{"!Global,!Plan · *"}, graph.Node{Group: "Plan · PlanA"}, false),
	)
})
