package uir_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
)

var _ = Describe("Stmt.Value", func() {
	It("returns the one populated statement, not the first field", func() {
		ret := &uir.ReturnStmt{}
		Expect(uir.Stmt{Return: ret}.Value()).To(BeIdenticalTo(ret))
	})

	It("returns nil when no statement is populated", func() {
		Expect(uir.Stmt{}.Value()).To(BeNil())
	})

	It("renders a populated statement instead of dereferencing an unset one", func() {
		value := "42"
		stmt := uir.Stmt{Literal: &uir.LiteralStmt{Value: &value}}
		Expect(stmt.Pretty().String()).To(ContainSubstring(value))
	})

	It("renders null for an empty statement", func() {
		Expect(uir.Stmt{}.Pretty().String()).To(Equal("null"))
	})
})

var _ = Describe("ExprStmt.Value", func() {
	It("returns the one populated expression, not the first field", func() {
		tuple := &uir.TupleStmt{}
		Expect(uir.ExprStmt{Tuple: tuple}.Value()).To(BeIdenticalTo(tuple))
	})

	It("returns nil when no expression is populated", func() {
		Expect(uir.ExprStmt{}.Value()).To(BeNil())
	})
})
