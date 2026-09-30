package uir_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
)

type collected struct {
	Type   uir.RelationshipType
	From   string
	To     string
	Guards []string
	Path   string
	Line   int
	Text   string
}

func summariseCollected(rels []uir.UIRRelationship) []collected {
	var out []collected
	for _, rel := range rels {
		entry := collected{
			Type:   rel.RelationshipType,
			From:   rel.GetFrom().GetIdentifier().String(),
			To:     rel.To.GetIdentifier().String(),
			Guards: guardText(rel.Guards),
			Path:   rel.Path,
		}
		if rel.StartLine != nil {
			entry.Line = *rel.StartLine
		}
		if rel.Content != nil {
			entry.Text = *rel.Content
		}
		out = append(out, entry)
	}
	return out
}

var _ = Describe("CollectRelationships", func() {
	const (
		sourcePath = "orders/process.go"
		caller     = "Orders:Process"
	)
	valid := uir.VarExpr("valid")
	process := func(stmts ...uir.Statement) uir.MethodNode {
		return uir.NewMethod("Process").WithType("Orders").WithBody(blockOf(stmts...)).Build()
	}

	It("returns nil for a method without a body", func() {
		Expect(uir.CollectRelationships(uir.NewMethod("Process").Build())).To(BeNil())
	})

	It("reports each call from the method, with the statement's source and the guards in scope", func() {
		validate := uir.NewMethodCall("Validate", uir.Identifier{Type: "Orders"}).
			WithSource(sourcePath, 12, 12).
			WithSourceCode("o.Validate(order)").
			Build()
		method := process(
			validate,
			ifThen(valid, uir.NewReturn(callExprTo("save"))).WithElse(blockOf(callTo("reject"))).Build(),
		)

		Expect(summariseCollected(uir.CollectRelationships(method))).To(Equal([]collected{
			{Type: uir.RelationshipTypeCall, From: caller, To: "Orders:Validate", Guards: []string{}, Path: sourcePath, Line: 12, Text: "o.Validate(order)"},
			{Type: uir.RelationshipTypeCall, From: caller, To: ":save", Guards: []string{"valid"}},
			{Type: uir.RelationshipTypeCall, From: caller, To: ":reject", Guards: []string{"!valid"}},
		}))
	})

	It("reports a dispatch call as one call plus one dispatch per candidate, each under the scope's guards", func() {
		dispatch := uir.NewDispatchCall("Handle", uir.Identifier{Type: "Handler"}).
			WithCandidate(uir.NewRef(uir.Identifier{Type: "Email", Method: "Handle"})).
			WithCandidate(uir.NewRef(uir.Identifier{Type: "Sms", Method: "Handle"})).
			Build()
		dispatch.Path = sourcePath
		dispatch.StartLine = new(20)

		Expect(summariseCollected(uir.CollectRelationships(process(ifThen(valid, dispatch).Build())))).To(Equal([]collected{
			{Type: uir.RelationshipTypeCall, From: caller, To: "Handler:Handle", Guards: []string{"valid"}, Path: sourcePath, Line: 20},
			{Type: uir.RelationshipTypeDispatch, From: caller, To: "Email:Handle", Guards: []string{"valid"}, Path: sourcePath, Line: 20},
			{Type: uir.RelationshipTypeDispatch, From: caller, To: "Sms:Handle", Guards: []string{"valid"}, Path: sourcePath, Line: 20},
		}))
	})

	It("reports an endpoint call as a call, a record read as a read and a record write as a write", func() {
		endpoint := uir.NewEndpoint("charge").Build()
		policies := uir.NewRef(uir.Identifier{Type: "AsPolicy", NodeType: uir.NodeTypeRecord})
		method := process(
			uir.NewEndpointCall(uir.EndpointTypeHTTP, endpoint).Build(),
			uir.NewRecordRead(uir.RecordTypeTable, policies).Build(),
			uir.NewRecordWrite(uir.RecordTypeTable, policies).Build(),
		)

		Expect(summariseCollected(uir.CollectRelationships(method))).To(Equal([]collected{
			{Type: uir.RelationshipTypeCall, From: caller, To: "/charge", Guards: []string{}},
			{Type: uir.RelationshipTypeRead, From: caller, To: "AsPolicy", Guards: []string{}},
			{Type: uir.RelationshipTypeWrite, From: caller, To: "AsPolicy", Guards: []string{}},
		}))
	})

	It("takes a record statement's own location, not its expression's", func() {
		read := uir.NewRecordRead(uir.RecordTypeTable, uir.NewRef(uir.Identifier{Type: "AsPolicy"})).
			WithExpression(uir.ExpressionTypeSQL, "SELECT 1").Build()
		read.Path = "inline.sql"

		rels := uir.CollectRelationships(process(read))
		Expect(rels).To(HaveLen(1))
		Expect(rels[0].Path).To(BeEmpty(), "inline.sql is where the expression lives, which the statement does not carry")
	})

	It("reports no relationship for a statement that names no target", func() {
		method := process(
			uir.MethodCallStmt{},
			uir.DispatchCallStmt{},
			uir.EndpointCallStmt{},
			uir.RecordReadStmt{},
			uir.RecordWriteStmt{},
			callTo("named"),
		)
		Expect(summariseCollected(uir.CollectRelationships(method))).To(Equal([]collected{
			{Type: uir.RelationshipTypeCall, From: caller, To: ":named", Guards: []string{}},
		}))
	})

	It("still reports the candidates of a dispatch call that names no declared target", func() {
		dispatch := uir.DispatchCallStmt{Candidates: []uir.Node{uir.NewRef(uir.Identifier{Type: "Email", Method: "Handle"})}}
		Expect(summariseCollected(uir.CollectRelationships(process(dispatch)))).To(Equal([]collected{
			{Type: uir.RelationshipTypeDispatch, From: caller, To: "Email:Handle", Guards: []string{}},
		}))
	})

	It("panics on a nil dispatch candidate, which names nothing to dispatch to", func() {
		dispatch := uir.DispatchCallStmt{Candidates: []uir.Node{nil}}
		Expect(func() { uir.CollectRelationships(process(dispatch)) }).To(PanicWith(ContainSubstring("nil candidate")))
	})
})
