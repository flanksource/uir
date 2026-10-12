package uir

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// An interface-typed slot — a block child, a Node field — holds its value under
// its kind, {"$<kind>": <value>}, so a decoder knows the concrete type before the
// value streams and never buffers or re-scans a subtree to find it. A slot in the
// legacy inline layout — what the Python and Java producers and every artifact
// written before the tagged layout carry — still decodes.
var _ = Describe("tagged interface slots", func() {
	nested := func() BlockStmt {
		return BlockStmt{Children: []Statement{
			RawStmt{Source: "a()", Language: "typescript"},
			IfStmt{Then: BlockStmt{Children: []Statement{
				RawStmt{Source: "b()", Language: "typescript"},
			}}},
		}}
	}

	It("writes each block child under its kind", func() {
		data, err := MarshalStatement(BlockStmt{Children: []Statement{RawStmt{Source: "a()", Language: "typescript"}}})
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(MatchJSON(`{
			"statement_type": "control:block",
			"children": [{"$raw": {"statement_type": "raw", "source": "a()", "source_language": "typescript"}}]
		}`))
	})

	It("writes a Node field under its kind", func() {
		data, err := MarshalStatement(NewMethodCall("Run", Identifier{Type: "Handler"}).Build())
		Expect(err).NotTo(HaveOccurred())
		var fields map[string]json.RawMessage
		Expect(json.Unmarshal(data, &fields)).To(Succeed())
		Expect(fields["Method"]).To(MatchJSON(`{"$ref": {"node_kind": "ref", "type": "Handler", "method": "Run"}}`))
	})

	It("keeps a child's refined Type in its slot, under its registered kind", func() {
		call := NewMethodCall("Println", Identifier{Package: "fmt"}).Build()
		call.Type = ASTStatementTypeCallPackage
		data, err := json.Marshal(BlockStmt{Children: []Statement{call}})
		Expect(err).NotTo(HaveOccurred())
		var block struct {
			Children []map[string]map[string]any `json:"children"`
		}
		Expect(json.Unmarshal(data, &block)).To(Succeed())
		Expect(block.Children[0]).To(HaveKeyWithValue("$call", HaveKeyWithValue(statementTypeField, string(ASTStatementTypeCallPackage))))
	})

	It("unwraps slots for map walkers, stamped as the legacy layout stamps them", func() {
		call := NewMethodCall("Println", Identifier{Package: "fmt"}).Build()
		call.Type = ASTStatementTypeCallPackage
		data, err := json.Marshal(BlockStmt{Children: []Statement{call}})
		Expect(err).NotTo(HaveOccurred())
		var doc map[string]any
		Expect(json.Unmarshal(data, &doc)).To(Succeed())

		child := UnwrapSlots(doc).(map[string]any)["children"].([]any)[0].(map[string]any)
		Expect(child).To(HaveKeyWithValue(statementTypeField, string(ASTStatementTypeCall)))
		Expect(child).To(HaveKeyWithValue(statementRefinementField, string(ASTStatementTypeCallPackage)))
		Expect(child["Method"]).To(HaveKeyWithValue("node_kind", "ref"))
	})

	It("encodes the same bytes through Marshal and encoding/json, and decodes them back", func() {
		call := NewMethodCall("Println", Identifier{Package: "fmt"}).Build()
		call.Type = ASTStatementTypeCallPackage
		block := nested()
		block.Children = append(block.Children, call, DispatchCallStmt{Candidates: []Node{NewRef(Identifier{Type: "A"})}})

		viaUIR, err := Marshal(block)
		Expect(err).NotTo(HaveOccurred())
		viaStd, err := json.Marshal(block)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(viaUIR)).To(Equal(string(viaStd)))

		var decoded BlockStmt
		Expect(Unmarshal(viaUIR, &decoded)).To(Succeed())
		Expect(decoded.Children[2].(*MethodCallStmt).Type).To(Equal(ASTStatementTypeCallPackage))
		Expect(decoded.Children[3].(*DispatchCallStmt).Candidates).To(HaveLen(1))
	})

	It("writes a slot's node_kind ahead of the node's own fields", func() {
		data, err := Marshal(NewMethodCall("Run", Identifier{Type: "Handler"}).Build())
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(ContainSubstring(`"Method":{"$ref":{"node_kind":"ref",`))
	})

	It("decodes nested blocks the same through Unmarshal and encoding/json", func() {
		data, err := json.Marshal(nested())
		Expect(err).NotTo(HaveOccurred())

		var viaUIR, viaStd BlockStmt
		Expect(Unmarshal(data, &viaUIR)).To(Succeed())
		Expect(json.Unmarshal(data, &viaStd)).To(Succeed())
		Expect(viaUIR).To(Equal(viaStd))
		Expect(viaUIR.Children).To(HaveLen(2))
		Expect(viaUIR.Children[1].(*IfStmt).Then.Children[0].(*RawStmt).Source).To(Equal("b()"))
	})

	It("reads the legacy inline layout", func() {
		legacy := `{"statement_type":"control:block","children":[
			{"source":"a()","statement_type":"raw"},
			{"Method":{"method":"Run","node_kind":"ref","type":"Handler"},"statement_type":"call"}
		]}`
		var block BlockStmt
		Expect(Unmarshal([]byte(legacy), &block)).To(Succeed())
		Expect(block.Children).To(HaveLen(2))
		Expect(block.Children[0].(*RawStmt).Source).To(Equal("a()"))
		Expect(block.Children[1].(*MethodCallStmt).Method).To(Equal(&NodeRef{Identifier: Identifier{Type: "Handler", Method: "Run"}}))
	})

	It("refuses a tagged slot holding more than one kind", func() {
		var block BlockStmt
		err := Unmarshal([]byte(`{"statement_type":"control:block","children":[{"$raw":{},"$call":{}}]}`), &block)
		Expect(err).To(MatchError(ContainSubstring("exactly one kind")))
	})

	It("refuses an unknown kind instead of guessing", func() {
		var block BlockStmt
		err := Unmarshal([]byte(`{"statement_type":"control:block","children":[{"$no-such-kind":{}}]}`), &block)
		Expect(err).To(MatchError(ContainSubstring(`unknown statement_type "no-such-kind"`)))
	})

	It("refuses a tagged Type that does not refine the slot's kind", func() {
		var block BlockStmt
		err := Unmarshal([]byte(`{"statement_type":"control:block","children":[{"$call":{"statement_type":"control:if"}}]}`), &block)
		Expect(err).To(HaveOccurred())
	})
})
