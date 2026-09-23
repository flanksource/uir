package uir_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
)

// unknownNode is a Node that UIR.Add has no arm for.
type unknownNode struct{ uir.MethodNode }

var _ = Describe("UIR.Add", func() {
	It("records a warning for a node it cannot place instead of dropping it silently", func() {
		node := unknownNode{uir.NewMethod("orphan").Build()}
		doc := (&uir.UIR{}).Add(node)

		Expect(doc.Functions).To(BeEmpty())
		Expect(doc.Warnings).To(HaveLen(1))
		Expect(doc.Warnings[0].Node).To(Equal(uir.Node(node)))
		Expect(doc.Warnings[0].String()).To(ContainSubstring("uir_test.unknownNode"))
	})

	It("records no warning for a node it places", func() {
		doc := (&uir.UIR{}).Add(uir.NewMethod("init").Build())

		Expect(doc.Functions).To(HaveLen(1))
		Expect(doc.Warnings).To(BeEmpty())
	})
})
