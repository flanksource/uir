package lower_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/lower"
)

// guardTexts renders each site's guards as they print.
func guardTexts(sites []lower.Site) [][]string {
	out := make([][]string, 0, len(sites))
	for _, site := range sites {
		var texts []string
		for _, guard := range site.Guards {
			texts = append(texts, guard.Pretty().String())
		}
		out = append(out, texts)
	}
	return out
}

var _ = Describe("Recorder", func() {
	include := func(name string, offset int64) lower.Site {
		return lower.Site{Type: uir.RelationshipTypeIncludes, Kind: "include", Name: name, Elements: []string{"Include"}, Offset: offset}
	}

	It("drops a second visit of one element, which records the same kind and name at the same offset", func() {
		r := lower.NewRecorder(lower.RecorderOptions{})
		r.Record(include("Rates", 40))
		r.Record(include("Rates", 40))
		r.Record(include("Rates", 90))
		r.Record(lower.Site{Kind: "function", Name: "Rates", Offset: 40})

		Expect(r.Sites()).To(Equal([]lower.Site{
			include("Rates", 40), include("Rates", 90), {Kind: "function", Name: "Rates", Offset: 40},
		}))
	})

	It("never deduplicates a site without an offset, which no element identifies", func() {
		r := lower.NewRecorder(lower.RecorderOptions{})
		r.Record(include("Rates", 0))
		r.Record(include("Rates", 0))

		Expect(r.Sites()).To(HaveLen(2))
	})

	It("records the name trimmed, and nothing for a site that names nothing", func() {
		r := lower.NewRecorder(lower.RecorderOptions{})
		r.Record(include("  Rates\n", 40))
		r.Record(include(" \t", 50))

		Expect(r.Names("include")).To(Equal([]string{"Rates"}))
	})

	It("lists the names recorded for one kind in recording order", func() {
		r := lower.NewRecorder(lower.RecorderOptions{})
		r.Record(include("Zeta", 90))
		r.Record(lower.Site{Kind: "function", Name: "Fee", Offset: 10})
		r.Record(include("Alpha", 20))

		Expect(r.Names("include")).To(Equal([]string{"Zeta", "Alpha"}))
	})

	It("works as a zero value, deduplicating without NewRecorder", func() {
		var r lower.Recorder
		r.Record(include("Rates", 40))
		r.Record(include("Rates", 40))

		Expect(r.Sites()).To(Equal([]lower.Site{include("Rates", 40)}))
	})

	It("is a no-op recorder when nil", func() {
		var r *lower.Recorder
		r.Record(include("Rates", 40))
		pop := r.Guard(uir.VarExpr("ok"))
		pop()
		rels, err := r.Relationships(lower.Source{Body: []byte("<Include>Rates</Include>")})

		Expect([]any{r.Sites(), r.Names("include"), r.Guarding(), rels, err}).To(Equal([]any{[]lower.Site(nil), []string(nil), false, []uir.UIRRelationship(nil), nil}))
	})

	Describe("the guard stack", func() {
		It("stamps each site with the conditions in force, outermost first, until each is popped", func() {
			r := lower.NewRecorder(lower.RecorderOptions{Guards: true})
			popOuter := r.Guard(uir.VarExpr("a"))
			r.Record(include("One", 10))
			popInner := r.Guard(uir.VarExpr("b"), uir.UnaryExpr(uir.UnaryOpNot, uir.VarExpr("c")))
			r.Record(include("Two", 20))
			popInner()
			r.Record(include("Three", 30))
			popOuter()
			r.Record(include("Four", 40))

			Expect(guardTexts(r.Sites())).To(Equal([][]string{{"a"}, {"a", "b", "!c"}, {"a"}, nil}))
		})

		It("keeps a site's guards when a later push grows the stack", func() {
			r := lower.NewRecorder(lower.RecorderOptions{Guards: true})
			pop := r.Guard(uir.VarExpr("a"))
			r.Record(include("One", 10))
			pop()
			pop = r.Guard(uir.VarExpr("z"))
			pop()

			Expect(guardTexts(r.Sites())).To(Equal([][]string{{"a"}}))
		})

		It("panics when an outer guard is popped while an inner one is still in force", func() {
			r := lower.NewRecorder(lower.RecorderOptions{Guards: true})
			popA := r.Guard(uir.VarExpr("a"))
			popB := r.Guard(uir.VarExpr("b"))

			Expect(popA).To(PanicWith(ContainSubstring("guard popped out of order")))
			_ = popB
		})

		It("panics when one guard is popped twice", func() {
			r := lower.NewRecorder(lower.RecorderOptions{Guards: true})
			pop := r.Guard(uir.VarExpr("a"))
			pop()

			Expect(pop).To(PanicWith(ContainSubstring("guard popped out of order")))
		})

		It("stamps no guard unless the recorder records guards", func() {
			r := lower.NewRecorder(lower.RecorderOptions{})
			pop := r.Guard(uir.VarExpr("a"))
			r.Record(include("One", 10))
			pop()

			Expect([]any{r.Guarding(), guardTexts(r.Sites())}).To(Equal([]any{false, [][]string{nil}}))
		})
	})
})
