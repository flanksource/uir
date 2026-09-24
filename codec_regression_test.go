package uir

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// roundTripStatement encodes stmt as a block child would be and decodes it back.
func roundTripStatement(stmt Statement) Statement {
	GinkgoHelper()
	data, err := MarshalStatement(stmt)
	Expect(err).NotTo(HaveOccurred())
	decoded, err := StatementMarshaler.UnmarshalByType(data)
	Expect(err).NotTo(HaveOccurred(), "decoding %s", data)
	return decoded
}

var _ = Describe("UIR JSON codec regressions", func() {
	const (
		sourcePath = "rules/Transaction.xml"
		comment    = "spawned by the plan"
	)
	startLine := 12

	Describe("MethodCallStmt", func() {
		It("keeps a constructor call a constructor, with its location and metadata", func() {
			call := NewMethodCall("AddRider", Identifier{Type: "Transactions"}).AsConstructor().Build()
			call.Path = sourcePath
			call.StartLine = &startLine
			call.Comments = []Comment{{Text: comment}}

			decoded := roundTripStatement(BlockStmt{Children: []Statement{call}}).(*BlockStmt)

			got := decoded.Children[0].(*MethodCallStmt)
			Expect(got.IsConstructor).To(BeTrue(), "a constructor rendered as a plain call emits X() instead of new X()")
			Expect(got.Path).To(Equal(sourcePath))
			Expect(got.StartLine).To(HaveValue(Equal(startLine)))
			Expect(got.Comments).To(ConsistOf(HaveField("Text", comment)))
			Expect(got.Method).To(HaveValue(Equal(call.Method)), "a reference decodes as a *NodeRef")
		})

		It("keeps an absent method absent", func() {
			decoded := roundTripStatement(MethodCallStmt{}).(*MethodCallStmt)
			Expect(decoded.Method).To(BeNil())
		})

		It("reports a method it cannot decode instead of dropping it", func() {
			_, err := StatementMarshaler.UnmarshalByType([]byte(`{"statement_type":"call","Method":{"node_kind":"no-such-kind"}}`))
			Expect(err).To(MatchError(ContainSubstring("no-such-kind")))
		})
	})

	Describe("record reads and writes", func() {
		// The arch-unit proto converter and the correspondence parser both name the
		// record by a bare reference that carries no node type of its own.
		It("round-trips a record named by a reference without a node type", func() {
			ref := NewRef(Identifier{Package: "dbo", Type: "AsPolicy"})
			read := NewRecordRead(RecordTypeTable, ref).WithExpression(ExpressionTypeSQL, "SELECT 1").Build()

			got := roundTripStatement(read).(*RecordReadStmt)
			Expect(got.Record).To(HaveValue(Equal(ref)))
			Expect(got.RecordType).To(Equal(RecordTypeTable), "NewRecordRead's record type must not be discarded")
		})

		It("keeps the statement location apart from the expression's", func() {
			write := NewRecordWrite(RecordTypeTable, NewRef(Identifier{Type: "AsPolicy"})).
				WithExpression(ExpressionTypeSQL, "UPDATE AsPolicy SET x = 1").Build()
			write.statementBase.Path = sourcePath
			write.Path = "inline.sql" // the Expression's, which is the shallower Path

			got := roundTripStatement(write).(*RecordWriteStmt)
			Expect(got.recordBase.statementBase.Path).To(Equal(sourcePath))
			Expect(got.Expression.Path).To(Equal("inline.sql"))
			Expect(got.RecordType).To(Equal(RecordTypeTable))
		})
	})

	It("decodes an endpoint call's endpoint", func() {
		endpoint := NewEndpoint("getPolicy").Build()
		call := NewEndpointCall(EndpointTypeHTTP, endpoint).Build()

		got := roundTripStatement(call).(*EndpointCallStmt)
		Expect(got.Endpoint).To(Equal(&endpoint))
	})

	It("keeps a function declaration's statement fields and its method's metadata", func() {
		method := NewMethod("compute").Build()
		method.Comments = []Comment{{Text: comment}}
		decl := FunctionDeclStmt{MethodNode: method}
		decl.Path = sourcePath

		got := roundTripStatement(decl).(*FunctionDeclStmt)
		Expect(got.statementBase.Path).To(Equal(sourcePath))
		Expect(got.MethodNode.Comments).To(ConsistOf(HaveField("Text", comment)))
		Expect(got.MethodNode.Method).To(Equal("compute"))
	})

	It("decodes a scoped variable reference held as a block child", func() {
		ref := &ScopedVariableRef{VariableRef: VariableRef{Name: "PolicyNumber"}}

		got := roundTripStatement(BlockStmt{Children: []Statement{ref}}).(*BlockStmt)
		Expect(got.Children).To(ConsistOf(ref))
	})

	It("keeps an endpoint's name apart from its HTTP method", func() {
		endpoint := NewEndpoint("getPolicy").Build()
		endpoint.Method = EndpointMethodGet

		data, err := MarshalNode(endpoint)
		Expect(err).NotTo(HaveOccurred())
		got, err := UnmarshalNode(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.GetIdentifier()).To(Equal(endpoint.GetIdentifier()), "the endpoint's identity must survive")
		Expect(got.(*ASTEndpoint).Method).To(Equal(EndpointMethodGet))
	})

	It("keeps both ends of a relationship", func() {
		from := NewRef(Identifier{Type: "Caller", Method: "run"})
		to := NewRecord("AsPolicy").Build()
		rel := NewRelationship(RelationshipTypeRead, from, to).Build()

		data, err := json.Marshal(rel)
		Expect(err).NotTo(HaveOccurred())
		var got UIRRelationship
		Expect(json.Unmarshal(data, &got)).To(Succeed(), "decoding %s", data)
		Expect(got.GetFrom()).To(HaveValue(Equal(from)))
		Expect(got.GetTo()).To(Equal(&to))
	})

	It("refuses a node slot holding a typed nil", func() {
		var record *ASTRecord
		_, err := MarshalStatement(NewRecordRead(RecordTypeTable, record).Build())
		Expect(err).To(MatchError(ContainSubstring("nil")))
	})

	It("treats a null method body and a null branch as absent", func() {
		var method MethodNode
		Expect(json.Unmarshal([]byte(`{"method":"compute","body":null}`), &method)).To(Succeed())
		Expect(method.Body).To(BeNil())

		stmt, err := StatementMarshaler.UnmarshalByType([]byte(`{"statement_type":"control:if","then":null}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(stmt.(*IfStmt).Then.Children).To(BeEmpty())
	})

	It("refuses a node without a node_kind", func() {
		_, err := UnmarshalNode([]byte(`{"type":"AsPolicy","node_type":"record"}`))
		Expect(err).To(MatchError(ContainSubstring("node_kind")))
	})
})
