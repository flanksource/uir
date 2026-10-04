package graph_test

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
)

var _ = Describe("FillGuards", func() {
	// guarded is a graph whose run>save edge has a site in a.go on lines 3 and
	// 5, and a site with no position; save>flush a site in b.go and one in c.go.
	guarded := func() *graph.Graph {
		return &graph.Graph{Edges: []graph.Edge{
			{From: "run", To: "save", Type: uir.RelationshipTypeCall, Sites: []graph.Site{
				{Path: "a.go", Line: 3}, {Path: "a.go", Line: 5}, {Text: "save()"},
			}},
			{From: "save", To: "flush", Type: uir.RelationshipTypeCall, Sites: []graph.Site{{Path: "b.go", Line: 1}, {Path: "c.go", Line: 2}}},
		}}
	}
	guardsBy := func(byLine map[string][]string) func(graph.Edge, graph.Site) ([]string, error) {
		return func(edge graph.Edge, site graph.Site) ([]string, error) {
			guards, ok := byLine[fmt.Sprintf("%s>%s %s:%d", edge.From, edge.To, site.Path, site.Line)]
			if !ok {
				return nil, fmt.Errorf("%w: %s holds no call on line %d", graph.ErrUnreadableSource, site.Path, site.Line)
			}
			return guards, nil
		}
	}

	It("sets each positioned site's guards from the edge and site", func() {
		g := guarded()
		Expect(graph.FillGuards(g, guardsBy(map[string][]string{
			"run>save a.go:3": {"ok"}, "run>save a.go:5": nil, "save>flush b.go:1": {"!done"}, "save>flush c.go:2": {"x > 0", "y"},
		}))).To(Succeed())

		Expect(g.Edges[0].Sites).To(Equal([]graph.Site{{Path: "a.go", Line: 3, Guards: []string{"ok"}}, {Path: "a.go", Line: 5}, {Text: "save()"}}))
		Expect(g.Edges[1].Sites).To(Equal([]graph.Site{{Path: "b.go", Line: 1, Guards: []string{"!done"}}, {Path: "c.go", Line: 2, Guards: []string{"x > 0", "y"}}}))
		Expect(g.Omitted.UnreadableSource).To(BeEmpty())
	})

	It("lists each source whose guards are unreadable once, in the order met, and fills the rest", func() {
		g := guarded()
		Expect(graph.FillGuards(g, guardsBy(map[string][]string{"run>save a.go:5": {"ok"}, "save>flush b.go:1": {"!done"}}))).To(Succeed())

		Expect(g.Omitted.UnreadableSource).To(Equal([]string{"a.go", "c.go"}))
		Expect(g.Edges[0].Sites[1].Guards).To(Equal([]string{"ok"}))
		Expect(g.Edges[1].Sites[0].Guards).To(Equal([]string{"!done"}))
	})

	It("keeps the guards the source gave a site whose guards are unreadable", func() {
		g := guarded()
		g.Edges[1].Sites[1].Guards = []string{"from the index"}
		Expect(graph.FillGuards(g, guardsBy(map[string][]string{"save>flush b.go:1": {"!done"}}))).To(Succeed())

		Expect(g.Edges[1].Sites).To(Equal([]graph.Site{
			{Path: "b.go", Line: 1, Guards: []string{"!done"}}, {Path: "c.go", Line: 2, Guards: []string{"from the index"}},
		}))
	})

	It("fails on any other error, naming the site", func() {
		broken := errors.New("lowering failed")
		err := graph.FillGuards(guarded(), func(graph.Edge, graph.Site) ([]string, error) { return nil, broken })

		Expect(err).To(MatchError(broken))
		Expect(err).To(MatchError(ContainSubstring("graph: guards of the run>save site at a.go:3")))
	})
})
