package uir_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
	"github.com/flanksource/uir"
)

var _ = Describe("LiteralStmt.Pretty", func() {
	DescribeTable("prints the value alone, in the style of its literal family",
		func(fieldType uir.RecordFieldType, value, text, style string) {
			rendered := uir.Literal(value, fieldType).Pretty()
			Expect(rendered.String()).To(Equal(text))
			Expect(rendered).To(Equal(api.Text{Content: text, Style: style}))
			Expect(uir.LiteralStyle(fieldType)).To(Equal(style))
		},
		Entry("a string, quoted", uir.RecordFieldTypeString, "ACTIVE", `"ACTIVE"`, uir.StyleLiteralString),
		Entry("a number", uir.RecordFieldTypeNumber, "10", "10", uir.StyleLiteralNumber),
		Entry("an int", uir.RecordFieldTypeInt, "0", "0", uir.StyleLiteralNumber),
		Entry("a float", uir.RecordFieldTypeFloat, "1.5", "1.5", uir.StyleLiteralNumber),
		Entry("a boolean", uir.RecordFieldTypeBoolean, "true", "true", uir.StyleLiteralBoolean),
		Entry("a date, quoted", uir.RecordFieldTypeDate, "2026-01-02", `"2026-01-02"`, uir.StyleLiteralOther),
		Entry("an enum, quoted", uir.RecordFieldTypeEnum, "PENDING", `"PENDING"`, uir.StyleLiteralOther),
		Entry("an untyped literal, quoted", uir.RecordFieldType(""), "null", `"null"`, uir.StyleLiteralOther),
	)

	It("gives each of the string, number, boolean and other families its own style", func() {
		styles := []string{uir.StyleLiteralString, uir.StyleLiteralNumber, uir.StyleLiteralBoolean, uir.StyleLiteralOther}
		seen := map[string]bool{}
		for _, style := range styles {
			seen[style] = true
		}
		Expect(seen).To(HaveLen(len(styles)))
	})

	It("prints a literal without a value as the empty value of its family", func() {
		Expect(uir.LiteralStmt{FieldType: uir.RecordFieldTypeString}.Pretty().String()).To(Equal(`""`))
		Expect(uir.LiteralStmt{FieldType: uir.RecordFieldTypeInt}.Pretty().String()).To(BeEmpty())
	})

	DescribeTable("reads as the source would inside an expression",
		func(expr uir.ExprStmt, text string) {
			Expect(expr.Pretty().String()).To(Equal(text))
		},
		Entry("a number comparison", uir.BinaryExpr(uir.VarExpr("limit"), uir.BinaryOpGreater, uir.LitExpr("0", uir.RecordFieldTypeInt)), "limit > 0"),
		Entry("a string comparison", uir.BinaryExpr(uir.VarExpr("status"), uir.BinaryOpEqual, uir.LitExpr("ACTIVE", uir.RecordFieldTypeString)), `status == "ACTIVE"`),
		Entry("a boolean comparison", uir.BinaryExpr(uir.VarExpr("ok"), uir.BinaryOpEqual, uir.LitExpr("true", uir.RecordFieldTypeBoolean)), "ok == true"),
	)
})

var _ = Describe("the rendering theme", func() {
	DescribeTable("styles a type name as it styles a literal of that type",
		func(fieldType uir.RecordFieldType) {
			Expect(fieldType.Pretty().Style).To(Equal(uir.LiteralStyle(fieldType)))
		},
		Entry("string", uir.RecordFieldTypeString),
		Entry("number", uir.RecordFieldTypeNumber),
		Entry("int", uir.RecordFieldTypeInt),
		Entry("float", uir.RecordFieldTypeFloat),
		Entry("boolean", uir.RecordFieldTypeBoolean),
		Entry("a type with no style of its own", uir.RecordFieldTypeUUID),
	)

	It("keeps a distinct hue for the type names that have one", func() {
		Expect(uir.RecordFieldTypeDate.Pretty()).To(Equal(api.Text{Content: "date", Style: "text-indigo-600"}))
		Expect(uir.RecordFieldTypeMap.Pretty()).To(Equal(api.Text{Content: "object", Style: "text-cyan-600"}))
		Expect(uir.FieldTypeFloat.Pretty()).To(Equal(api.Text{Content: "number", Style: "text-blue-600"}))
		Expect(uir.FieldType("custom").Pretty()).To(Equal(api.Text{Content: "custom", Style: "text-yellow-600"}))
	})

	It("looks up a node type's colour and icon, and leaves an unlisted one bare", func() {
		Expect(uir.NodeTypeMethod.Color()).To(Equal("text-purple-500"))
		Expect(uir.NodeTypeMethod.Icon()).To(Equal(icons.Method))
		Expect(uir.NodeTypeUnknown.Color()).To(Equal("text-gray-400"))
		Expect(uir.NodeTypeRef.Color()).To(BeEmpty())
		Expect(uir.NodeTypeRef.Icon()).To(Equal(api.Text{Content: ""}))
	})

	DescribeTable("names a relationship with its arrow and colour",
		func(relationship uir.RelationshipType, text string, style string) {
			rendered := relationship.Pretty()
			Expect(rendered.String()).To(HaveSuffix(text))
			Expect(rendered.Children).To(ContainElement(api.Text{Content: text, Style: style}))
		},
		Entry("a call", uir.RelationshipTypeCall, " call", "text-green-600"),
		Entry("a dispatch", uir.RelationshipTypeDispatch, " dispatch", "text-green-600"),
		Entry("an import", uir.RelationshipTypeImport, " import", "text-blue-600"),
		Entry("one with no entry of its own", uir.RelationshipTypeRead, " reference", "text-yellow-600"),
	)
})
