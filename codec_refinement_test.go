package uir

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A statement's Type may be refined past the kind it is registered under — a call
// into another package is a call:package, and the markdown extractor marks a
// section body as doc. statement_type stays the registered kind, which is what an
// interface slot decodes by, and the refinement travels beside it.
var _ = Describe("refined statement types", func() {
	encodedKeys := func(data []byte) map[string]any {
		GinkgoHelper()
		fields := map[string]any{}
		Expect(json.Unmarshal(data, &fields)).To(Succeed())
		return fields
	}

	It("keeps a call refined to call:package, stamped under its registered kind", func() {
		call := NewMethodCall("Println", Identifier{Package: "fmt"}).Build()
		call.Type = ASTStatementTypeCallPackage

		data, err := MarshalStatement(call)
		Expect(err).NotTo(HaveOccurred())
		Expect(encodedKeys(data)).To(And(
			HaveKeyWithValue(statementTypeField, string(ASTStatementTypeCall)),
			HaveKeyWithValue(statementRefinementField, string(ASTStatementTypeCallPackage)),
		))

		got := roundTripStatement(BlockStmt{Children: []Statement{call}}).(*BlockStmt).Children[0].(*MethodCallStmt)
		Expect(got.Type).To(Equal(ASTStatementTypeCallPackage))
		Expect(got.GetStatementType()).To(Equal(ASTStatementTypeCall), "the refinement never changes the kind")
	})

	Describe("a block refined to doc, as the markdown extractor builds a section body", func() {
		section := func() MethodNode {
			heading := NewDoc("Overview").WithHeading(1)
			method := NewMethod("Overview").Build()
			method.Body = &BlockStmt{Children: []Statement{heading}}
			method.Body.Type = ASTStatementTypeDoc
			return method
		}

		It("survives as a method body", func() {
			method := section()
			data, err := json.Marshal(method)
			Expect(err).NotTo(HaveOccurred())

			var got MethodNode
			Expect(json.Unmarshal(data, &got)).To(Succeed(), "decoding %s", data)
			Expect(got.Body.Type).To(Equal(ASTStatementTypeDoc))
			Expect(got.Body.Children).To(Equal(method.Body.Children))
		})

		It("survives in an interface slot, where doc would otherwise name a DocStmt", func() {
			body := *section().Body
			got := roundTripStatement(body)

			Expect(got).To(BeAssignableToTypeOf(&BlockStmt{}))
			Expect(got.(*BlockStmt).Type).To(Equal(ASTStatementTypeDoc))
		})
	})

	It("refuses a refinement that names another statement's kind", func() {
		call := MethodCallStmt{}
		call.Type = ASTStatementTypeRecordRead
		_, err := MarshalStatement(call)
		Expect(err).To(MatchError(ContainSubstring(string(ASTStatementTypeRecordRead))))

		block := BlockStmt{}
		block.Type = ASTStatementTypeIf
		_, err = json.Marshal(block)
		Expect(err).To(MatchError(ContainSubstring(string(ASTStatementTypeIf))))
	})

	DescribeTable("refuses to decode a refinement of another kind",
		func(document string) {
			_, err := StatementMarshaler.UnmarshalByType([]byte(document))
			Expect(err).To(MatchError(ContainSubstring(statementRefinementField)))
		},
		Entry("a call refined to an if", `{"statement_type":"call","statement_refinement":"control:if"}`),
		Entry("a block refined to a call", `{"statement_type":"control:block","statement_refinement":"call"}`),
		Entry("a refinement that is not a string", `{"statement_type":"call","statement_refinement":7}`),
	)

	It("refuses a method body whose refinement names another kind", func() {
		var method MethodNode
		err := json.Unmarshal([]byte(`{"body":{"statement_type":"control:block","statement_refinement":"control:if"}}`), &method)
		Expect(err).To(MatchError(ContainSubstring(string(ASTStatementTypeIf))))
	})
})
