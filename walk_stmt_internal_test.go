package uir

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("emptyCondition: no expression variant and no source text",
	func(condition ConditionStmt, want bool) {
		Expect(emptyCondition(condition)).To(Equal(want))
	},
	Entry("the zero condition", ConditionStmt{}, true),
	Entry("a condition over an expression that carries only its kind",
		NewCondition(ExprStmt{statementBase: statementBase{Type: ASTStatementTypeExpression}}), true),
	Entry("a condition over an expression with empty source text",
		NewCondition(ExprStmt{statementBase: statementBase{SourceCode: SourceCode{Content: new("")}}}), true),
	Entry("a condition over a variable", NewCondition(VarExpr("ready")), false),
	Entry("a condition over an expression kept as source text",
		NewCondition(ExprStmt{statementBase: statementBase{SourceCode: SourceCode{Content: new("<-done")}}}), false),
)
