package lower_test

import (
	"bytes"
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/lower"
)

// relView is a relationship as these specs compare it.
type relView struct {
	Type             uir.RelationshipType
	Kind, Via, Name  string
	From, Path, Text string
	Line             int
	Guards           []string
}

func relViews(rels []uir.UIRRelationship) []relView {
	GinkgoHelper()
	views := make([]relView, 0, len(rels))
	for _, rel := range rels {
		Expect(rel.StartLine).To(Equal(rel.EndLine), "a reference sits on one line")
		view := relView{Type: rel.RelationshipType, Kind: rel.Kind, Via: rel.Via, Name: rel.To.GetIdentifier().Type, Path: rel.Path}
		if from := rel.GetFrom(); from != nil {
			view.From = from.GetIdentifier().String()
		}
		if rel.Content != nil {
			view.Text = *rel.Content
		}
		if rel.StartLine != nil {
			view.Line = *rel.StartLine
		}
		for _, guard := range rel.Guards {
			view.Guards = append(view.Guards, guard.Pretty().String())
		}
		views = append(views, view)
	}
	return views
}

// offsetAfter is the offset just past the n-th (0-based) occurrence of start in body.
func offsetAfter(body, start string, n int) int64 {
	at := 0
	for i := 0; ; i++ {
		found := bytes.Index([]byte(body[at:]), []byte(start))
		Expect(found).To(BeNumerically(">=", 0), "%q has no occurrence %d of %q", body, n, start)
		if i == n {
			return int64(at + found + len(start))
		}
		at += found + len(start)
	}
}

const ruleBody = "<Rule>\n  <Include>Rates</Include>\n  <Math>\n    <Call FUNCTION=\"Fee\"/>\n  </Math>\n</Rule>"

var _ = Describe("Recorder.Relationships", func() {
	caller := uir.NewRef(uir.Identifier{Package: "rules", Type: "Charge"})
	source := lower.Source{From: caller, Path: "rules/Charge.xml", Body: []byte(ruleBody)}

	It("places each site on the line of its element, in document order, with the source as its origin", func() {
		r := lower.NewRecorder(lower.RecorderOptions{Guards: true})
		pop := r.Guard(uir.VarExpr("ok"))
		r.Record(lower.Site{Type: uir.RelationshipTypeCall, Kind: "function", Name: "Fee", Elements: []string{"Call"},
			Offset: offsetAfter(ruleBody, `<Call FUNCTION="Fee"/>`, 0), Text: "Fee()"})
		pop()
		r.Record(lower.Site{Type: uir.RelationshipTypeIncludes, Kind: "include", Via: "CopyBook", Name: "Rates",
			Elements: []string{"Include"}, Offset: offsetAfter(ruleBody, "<Include>", 0)})

		rels, err := r.Relationships(source)
		Expect(err).NotTo(HaveOccurred())
		Expect(relViews(rels)).To(Equal([]relView{
			{Type: uir.RelationshipTypeIncludes, Kind: "include", Via: "CopyBook", Name: "Rates", From: "rules.Charge", Path: "rules/Charge.xml", Line: 2},
			{Type: uir.RelationshipTypeCall, Kind: "function", Via: "Call", Name: "Fee", From: "rules.Charge", Path: "rules/Charge.xml", Line: 4, Text: "Fee()", Guards: []string{"ok"}},
		}))
	})

	It("fails with a PositionError when the offset follows an element the site may not carry", func() {
		afterMath := offsetAfter(ruleBody, "<Math>", 0)
		r := lower.NewRecorder(lower.RecorderOptions{})
		r.Record(lower.Site{Kind: "include", Name: "Rates", Elements: []string{"Include", "CopyBook"}, Offset: afterMath})

		_, err := r.Relationships(source)
		var position *lower.PositionError
		Expect(errors.As(err, &position)).To(BeTrue(), "got %v", err)
		Expect(*position).To(Equal(lower.PositionError{
			Kind: "include", Name: "Rates", Path: "rules/Charge.xml", Offset: afterMath,
			Expected: []string{"Include", "CopyBook"}, Found: "Math",
		}))
		Expect(err).To(MatchError(fmt.Sprintf(`include reference "Rates" in rules/Charge.xml has no position: decode offset `+
			`%d should follow <Include|CopyBook> but follows "Math"`, afterMath)))
	})

	It("fails with a PositionError naming no element when the offset is outside the body", func() {
		r := lower.NewRecorder(lower.RecorderOptions{})
		r.Record(lower.Site{Kind: "include", Name: "Rates", Elements: []string{"Include"}, Offset: int64(len(ruleBody) + 5)})

		_, err := r.Relationships(source)
		var position *lower.PositionError
		Expect(errors.As(err, &position)).To(BeTrue(), "got %v", err)
		Expect(position.Found).To(BeEmpty())
	})

	It("places a site recorded in a fragment on the line of the body the fragment was cut from", func() {
		fragment := `<Math><Call FUNCTION="Fee"/></Math>`
		at := offsetAfter(fragment, `<Call FUNCTION="Fee"/>`, 0)
		r := lower.NewRecorder(lower.RecorderOptions{})
		r.Record(lower.Site{Type: uir.RelationshipTypeCall, Kind: "function", Name: "Fee", Elements: []string{"Call"}, Offset: at})

		rels, err := r.Relationships(lower.Source{From: caller, Body: []byte(fragment), Fragment: &lower.Fragment{
			Body: []byte(ruleBody), Starts: map[int64]int64{at: offsetAfter(ruleBody, `<Call FUNCTION="Fee"/>`, 0)},
		}})
		Expect(err).NotTo(HaveOccurred())
		Expect(relViews(rels)).To(Equal([]relView{{Type: uir.RelationshipTypeCall, Kind: "function", Via: "Call", Name: "Fee", From: "rules.Charge", Line: 4}}))
	})
})

var _ = Describe("Source.Attribute", func() {
	rels := func() []uir.UIRRelationship {
		return []uir.UIRRelationship{
			uir.NewRelationship(uir.RelationshipTypeCall, uir.NewRef(uir.Identifier{Type: "Parsed"}), uir.NewRef(uir.Identifier{Type: "Fee"})).Source("parsed.xml", 3, 3).Build(),
		}
	}

	It("re-homes every relationship onto the source's entity and path", func() {
		attributed := lower.Source{From: uir.NewRef(uir.Identifier{Package: "rules", Type: "Charge"}), Path: "rules/Charge.xml"}.Attribute(rels())

		Expect(relViews(attributed)).To(Equal([]relView{{Type: uir.RelationshipTypeCall, Name: "Fee", From: "rules.Charge", Path: "rules/Charge.xml", Line: 3}}))
	})

	It("keeps each relationship's path when the source names none, and clears the origin when it names no entity", func() {
		Expect(relViews(lower.Source{}.Attribute(rels()))).To(Equal([]relView{{Type: uir.RelationshipTypeCall, Name: "Fee", Path: "parsed.xml", Line: 3}}))
	})
})

var _ = Describe("ShiftLines and InDocumentOrder", func() {
	at := func(name string, line int) uir.UIRRelationship {
		return uir.NewRelationship(uir.RelationshipTypeCall, nil, uir.NewRef(uir.Identifier{Type: name})).Source("", line, line).Build()
	}
	lines := func(rels []uir.UIRRelationship) []string {
		var out []string
		for _, view := range relViews(rels) {
			out = append(out, fmt.Sprintf("%s@%d", view.Name, view.Line))
		}
		return out
	}

	It("moves every relationship delta lines down", func() {
		Expect(lines(lower.ShiftLines([]uir.UIRRelationship{at("A", 1), at("B", 4)}, 2))).To(Equal([]string{"A@3", "B@6"}))
	})

	It("merges parts already in document order into one ordered by line, keeping a tie in part order", func() {
		merged := lower.InDocumentOrder([]uir.UIRRelationship{at("A", 1), at("C", 5)}, []uir.UIRRelationship{at("B", 3), at("D", 5)})
		Expect(lines(merged)).To(Equal([]string{"A@1", "B@3", "C@5", "D@5"}))
	})
})
