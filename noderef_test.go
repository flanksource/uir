package uir_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
)

var _ = Describe("NodeRef", func() {
	const (
		schema = "dbo"
		table  = "AsPolicy"
	)
	ref := uir.NewRef(uir.Identifier{Package: schema, Type: table, NodeType: uir.NodeTypeRecord})

	It("is a leaf with no location or language of its own", func() {
		Expect(ref.GetChildren()).To(BeEmpty())
		Expect(ref.GetLocation()).To(Equal(uir.Location{}))
		Expect(ref.GetLanguage()).To(BeEmpty())
	})

	It("ends a walk that reaches it, in both the built and the decoded form", func() {
		data, err := uir.MarshalNode(ref)
		Expect(err).NotTo(HaveOccurred())
		decoded, err := uir.UnmarshalNode(data)
		Expect(err).NotTo(HaveOccurred())

		tree := uir.NewTree(servicePackage(), ref, decoded)
		var visited []string
		tree.Walk(func(node uir.Node) bool {
			visited = append(visited, fmt.Sprintf("%T:%s", node, node.GetIdentifier().Type))
			return true
		}, uir.WalkOptions{IncludeRoot: true})

		Expect(visited).To(ContainElements("uir.NodeRef:"+table, "*uir.NodeRef:"+table))
		Expect(tree.GetFiles()).To(BeEmpty(), "a reference names no file, so it adds none")
	})
})
