package storage_test

import (
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func classShape() string {
	balance := uir.Field("Balance", uir.RecordFieldTypeInt)
	balance.Visibility = uir.VisibilityPublic
	account := uir.NewType("Account").WithVisibility(uir.VisibilityPublic).WithVariables(balance).Build()
	return uir.Shape(account, uir.LayerPrivate)
}

var _ = Describe("TypeForm", func() {
	DescribeTable("checks a declared type form against the form its shape classifies as",
		func(shape, declared, expected string) {
			id := digest("account type")
			form, err := storage.TypeForm(storage.DocumentFormatVersion, storage.DocumentSymbol{ID: &id, Kind: "type", Key: "account", Shape: shape, TypeForm: declared})
			if expected == "" {
				Expect(err).To(HaveOccurred())
				return
			}
			Expect(err).ToNot(HaveOccurred())
			Expect(form).To(Equal(expected))
		},
		Entry("a Go struct declaration", "type Account struct {\n\tBalance int\n}", "struct", "struct"),
		Entry("a UIR class shape is struct-like", classShape(), "struct", "struct"),
		Entry("a UIR class shape is not an interface", classShape(), "interface", ""),
		Entry("a shape that is neither a Go declaration nor a UIR class", "record Account", "struct", ""),
	)

	It("classifies a UIR class shape without parsing it as Go", func() {
		form, err := storage.ClassifyTypeShape(storage.DocumentSymbol{Key: "account", Shape: classShape()})
		Expect(err).ToNot(HaveOccurred())
		Expect(form).To(Equal("struct"))
	})
})
