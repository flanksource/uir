package uir_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
)

var _ = Describe("BinaryStmt.Pretty", func() {
	a, b, c, d := uir.VarExpr("a"), uir.VarExpr("b"), uir.VarExpr("c"), uir.VarExpr("d")
	binary := uir.BinaryExpr

	DescribeTable("parenthesises an operand only where the text would otherwise read as another expression",
		func(expr uir.ExprStmt, text string) {
			Expect(expr.Pretty().String()).To(Equal(text))
		},
		Entry("an or under an and, on the right", binary(a, uir.BinaryOpAnd, binary(b, uir.BinaryOpOr, c)), "a && (b || c)"),
		Entry("an or under an and, on the left", binary(binary(a, uir.BinaryOpOr, b), uir.BinaryOpAnd, c), "(a || b) && c"),
		Entry("an and under an or, on the right", binary(a, uir.BinaryOpOr, binary(b, uir.BinaryOpAnd, c)), "a || b && c"),
		Entry("an and under an or, on the left", binary(binary(a, uir.BinaryOpAnd, b), uir.BinaryOpOr, c), "a && b || c"),
		Entry("comparisons under an and", binary(binary(a, uir.BinaryOpEqual, b), uir.BinaryOpAnd, binary(c, uir.BinaryOpNotEqual, d)), "a == b && c != d"),
		Entry("a product under a sum", binary(a, uir.BinaryOpAdd, binary(b, uir.BinaryOpMultiply, c)), "a + b * c"),
		Entry("a sum under a product", binary(binary(a, uir.BinaryOpAdd, b), uir.BinaryOpMultiply, c), "(a + b) * c"),
		Entry("a sum under a comparison", binary(binary(a, uir.BinaryOpAdd, b), uir.BinaryOpLess, c), "a + b < c"),
		Entry("a left-nested difference", binary(binary(a, uir.BinaryOpSubtract, b), uir.BinaryOpSubtract, c), "a - b - c"),
		Entry("a right-nested difference", binary(a, uir.BinaryOpSubtract, binary(b, uir.BinaryOpSubtract, c)), "a - (b - c)"),
		Entry("a left-nested or chain", binary(binary(a, uir.BinaryOpOr, b), uir.BinaryOpOr, c), "a || b || c"),
		Entry("a right-nested or chain", binary(a, uir.BinaryOpOr, binary(b, uir.BinaryOpOr, c)), "a || b || c"),
		Entry("a right-nested and chain", binary(a, uir.BinaryOpAnd, binary(b, uir.BinaryOpAnd, c)), "a && b && c"),
		Entry("a right-nested comparison", binary(a, uir.BinaryOpEqual, binary(b, uir.BinaryOpEqual, c)), "a == (b == c)"),
		Entry("a left-nested power", binary(binary(a, uir.BinaryOpExponent, b), uir.BinaryOpExponent, c), "(a ** b) ** c"),
		Entry("a right-nested power", binary(a, uir.BinaryOpExponent, binary(b, uir.BinaryOpExponent, c)), "a ** b ** c"),
		Entry("a negated comparison under an and", binary(uir.UnaryExpr(uir.UnaryOpNot, binary(a, uir.BinaryOpEqual, b)), uir.BinaryOpAnd, c), "!(a == b) && c"),
		Entry("a negation, which binds tighter than any binary operator", binary(uir.UnaryExpr(uir.UnaryOpNot, a), uir.BinaryOpAnd, b), "!a && b"),
		Entry("literals", binary(binary(uir.VarExpr("limit"), uir.BinaryOpGreater, uir.LitExpr("0", uir.RecordFieldTypeInt)), uir.BinaryOpAnd,
			binary(uir.VarExpr("status"), uir.BinaryOpEqual, uir.LitExpr("ACTIVE", uir.RecordFieldTypeString))), `limit > 0 && status == "ACTIVE"`),
	)

	DescribeTable("always parenthesises around an operator whose precedence languages disagree on",
		func(expr uir.ExprStmt, text string) {
			Expect(expr.Pretty().String()).To(Equal(text))
		},
		Entry("a bitwise and under a comparison", binary(binary(a, uir.BinaryOpBitwiseAnd, b), uir.BinaryOpEqual, c), "(a & b) == c"),
		Entry("a comparison under a bitwise and", binary(a, uir.BinaryOpBitwiseAnd, binary(b, uir.BinaryOpEqual, c)), "a & (b == c)"),
		Entry("a shift under a sum", binary(a, uir.BinaryOpAdd, binary(b, uir.BinaryOpLeftShift, c)), "a + (b << c)"),
		Entry("a left-nested chain of one bitwise operator", binary(binary(a, uir.BinaryOpBitwiseOr, b), uir.BinaryOpBitwiseOr, c), "a | b | c"),
		Entry("a right-nested chain of one bitwise operator", binary(a, uir.BinaryOpBitwiseOr, binary(b, uir.BinaryOpBitwiseOr, c)), "a | (b | c)"),
		Entry("a nullish default beside an or", binary(binary(a, uir.BinaryOpOr, b), uir.BinaryOpNullishCoalescing, c), "(a || b) ?? c"),
	)

	It("styles the parentheses as punctuation and the operator as a binary operator", func() {
		rendered := binary(a, uir.BinaryOpAnd, binary(b, uir.BinaryOpOr, c)).Pretty().HTML()
		Expect(rendered).To(ContainSubstring(`<span class="` + uir.StylePunctuation + `"`))
		Expect(rendered).To(ContainSubstring(`<span class="` + uir.StyleBinaryOperator + `"`))
	})
})
