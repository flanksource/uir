package uir

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DispatchCallStmt", func() {
	var (
		declared   = Identifier{Type: "Handler"}
		candidateA = Identifier{Type: "A", Method: "Run"}
		candidateB = Identifier{Type: "B", Method: "Run"}
	)
	build := func() DispatchCallStmt {
		return NewDispatchCall("Run", declared).
			WithCandidate(NewRef(candidateA)).
			WithCandidate(NewRef(candidateB)).
			WithPositionalArg(VarExpr("order")).
			Build()
	}

	It("round-trips through the statement codec with its target and candidates as nodes", func() {
		call := build()
		data, err := MarshalStatement(call)
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(MatchJSON(`{
			"statement_type": "dispatch_call",
			"Method": {"node_kind": "ref", "type": "Handler", "method": "Run"},
			"arguments": [{"value": {"statement_type": "assignment:expression", "variable": {"name": "order"}}}],
			"candidates": [
				{"node_kind": "ref", "type": "A", "method": "Run"},
				{"node_kind": "ref", "type": "B", "method": "Run"}
			]
		}`))

		want := build()
		want.Method = &NodeRef{Identifier: Identifier{Type: "Handler", Method: "Run"}}
		want.Candidates = []Node{&NodeRef{Identifier: candidateA}, &NodeRef{Identifier: candidateB}}
		Expect(roundTripStatement(call)).To(Equal(&want))
	})

	It("round-trips as an expression variant", func() {
		call := build()
		decoded := roundTripStatement(ExprStmt{DispatchCall: &call}).(*ExprStmt)
		Expect(decoded.Value()).To(BeIdenticalTo(decoded.DispatchCall))
		Expect(decoded.DispatchCall.Candidates).To(HaveLen(2))
	})

	It("refuses a nil candidate instead of writing a hole", func() {
		call := build()
		call.Candidates = append(call.Candidates, nil)
		_, err := MarshalStatement(call)
		Expect(err).To(MatchError(ContainSubstring("DispatchCallStmt.Candidates")))
	})

	It("refuses a null candidate instead of reading a hole", func() {
		_, err := StatementMarshaler.UnmarshalByType([]byte(`{"statement_type":"dispatch_call","candidates":[null]}`))
		Expect(err).To(MatchError(ContainSubstring("candidates[0] is null")))
	})

	It("keeps absent candidates absent", func() {
		decoded := roundTripStatement(DispatchCallStmt{}).(*DispatchCallStmt)
		Expect(decoded.Candidates).To(BeNil())
		Expect(decoded.Method).To(BeNil())
	})

	It("hashes by target, arguments and candidates, and not by location", func() {
		base := build()
		moved := build()
		moved.Path = "handlers.go"
		moved.StartLine = new(12)
		fewer := build()
		fewer.Candidates = fewer.Candidates[:1]
		reordered := build()
		reordered.Candidates = []Node{NewRef(candidateB), NewRef(candidateA)}
		plain := NewMethodCall("Run", declared).WithPositionalArg(VarExpr("order")).Build()

		Expect(map[string]bool{
			"equal statements":     HashStatement(base) == HashStatement(build()),
			"value and pointer":    HashStatement(base) == HashStatement(&base),
			"moved":                HashStatement(base) == HashStatement(moved),
			"fewer candidates":     HashStatement(base) == HashStatement(fewer),
			"reordered candidates": HashStatement(base) == HashStatement(reordered),
			"a plain method call":  HashStatement(base) == HashStatement(plain),
			"as an expression":     HashStatement(ExprStmt{DispatchCall: &base}) == HashStatement(ExprStmt{DispatchCall: &fewer}),
		}).To(Equal(map[string]bool{
			"equal statements":     true,
			"value and pointer":    true,
			"moved":                true,
			"fewer candidates":     false,
			"reordered candidates": false,
			"a plain method call":  false,
			"as an expression":     false,
		}))
	})

	It("renders the declared target, its arguments and the candidates", func() {
		call := build()
		Expect(call.Pretty().String()).To(Equal("Handler.Run(order) → A.Run | B.Run"))
		Expect(ExprStmt{DispatchCall: &call}.Pretty().String()).To(Equal("Handler.Run(order) → A.Run | B.Run"))
	})

	It("renders like a plain call when it has no candidates", func() {
		Expect(NewDispatchCall("Run", declared).Build().Pretty().String()).To(Equal("Handler.Run()"))
	})

	It("takes a receiver and named arguments like a method call", func() {
		call := NewDispatchCall("Run").WithReceiver(VarExpr("handler")).WithArgument("order", VarExpr("o")).Build()
		Expect(call.Receiver).To(HaveValue(Equal(VarExpr("handler"))))
		Expect(call.Arguments).To(HaveLen(1))
		Expect(call.Arguments[0].Name).To(HaveValue(Equal("order")))
		Expect(call.Arguments[0].Value).To(Equal(VarExpr("o")))
		Expect(call.GetStatementType()).To(Equal(ASTStatementTypeDispatchCall))
	})

	It("relates to its declared target by call and to each candidate by dispatch", func() {
		call := build()
		type relation struct {
			Type RelationshipType
			To   string
		}
		summarise := func(rels []Relationship) []relation {
			var out []relation
			for _, rel := range rels {
				out = append(out, relation{rel.GetRelationshipType(), rel.GetTo().GetIdentifier().String()})
			}
			return out
		}
		want := []relation{
			{RelationshipTypeCall, "Handler:Run"},
			{RelationshipTypeDispatch, "A:Run"},
			{RelationshipTypeDispatch, "B:Run"},
		}
		Expect(summarise(call.GetRelationships())).To(Equal(want))
		Expect(summarise(ExprStmt{DispatchCall: &call}.GetRelationships())).To(Equal(want))
		Expect(ExprStmt{DispatchCall: &call}.GetChildren()).To(Equal([]Node{call.Method, call.Candidates[0], call.Candidates[1]}))
	})

	It("is the value of a statement one-of that holds it", func() {
		call := build()
		Expect(Stmt{DispatchCall: &call}.Value()).To(BeIdenticalTo(&call))
	})

})

var _ = Describe("UIRRelationship.Guards", func() {
	It("survives json.Marshal on a dispatch relationship", func() {
		rel := NewRelationship(RelationshipTypeDispatch, NewRef(Identifier{Type: "Handler"}), NewRef(Identifier{Type: "A"})).Build()
		rel.Guards = []ConditionStmt{NewCondition(VarExpr("enabled"))}

		data, err := json.Marshal(rel)
		Expect(err).NotTo(HaveOccurred())
		var decoded UIRRelationship
		Expect(json.Unmarshal(data, &decoded)).To(Succeed(), "decoding %s", data)
		Expect(decoded.RelationshipType).To(Equal(RelationshipTypeDispatch))
		Expect(decoded.Guards).To(Equal(rel.Guards))
	})
})

var _ = Describe("ExprStmt.Pretty", func() {
	It("prints its source text when no variant is set", func() {
		expr := ExprStmt{statementBase: statementBase{SourceCode: SourceCode{Content: new("<-done")}}}
		Expect(expr.Pretty()).To(HaveField("Content", "<-done"))
		Expect(expr.Pretty().Style).To(BeEmpty())
	})

	It("keeps the placeholder when it has neither a variant nor source text", func() {
		Expect(ExprStmt{}.Pretty().String()).To(Equal("expr"))
		Expect(ExprStmt{statementBase: statementBase{SourceCode: SourceCode{Content: new("")}}}.Pretty().String()).To(Equal("expr"))
	})

	It("prefers a variant over the source text", func() {
		expr := VarExpr("ready")
		expr.Content = new("ready /* flag */")
		Expect(expr.Pretty().String()).To(Equal("ready"))
	})
})

var _ = Describe("UnaryStmt.Pretty", func() {
	It("parenthesises a binary operand so the operator reads as applying to all of it", func() {
		negated := UnaryExpr(UnaryOpNot, BinaryExpr(VarExpr("a"), BinaryOpEqual, VarExpr("b")))
		Expect(negated.Pretty().String()).To(Equal("!(a == b)"))
	})

	It("leaves any other operand bare", func() {
		Expect(UnaryExpr(UnaryOpNot, VarExpr("a")).Pretty().String()).To(Equal("!a"))
	})
})
