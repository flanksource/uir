package graph_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
)

// memberOf places a column in its table and a field in its entity; a column
// stands for its table's site on the same line, a field does not.
func memberOf(node graph.Node) (graph.Member, bool) {
	kind, name, ok := graph.SplitDataID(node.ID)
	if !ok {
		return graph.Member{}, false
	}
	owner, member := graph.SplitMember(kind, name)
	switch kind {
	case graph.DataColumn:
		return graph.Member{Owner: graph.DataID(graph.DataTable, owner), Name: member, Property: "columns", Covers: true}, true
	case graph.DataField:
		return graph.Member{Owner: graph.DataID(graph.DataEntity, owner), Name: member, Property: "fields"}, true
	}
	return graph.Member{}, false
}

func ownerNode(id string) graph.Node {
	kind, name, _ := graph.SplitDataID(id)
	return graph.Node{ID: id, Kind: kind, Label: name}
}

func dataStep(id string, typ uir.RelationshipType, kind string, sites ...graph.Site) graph.Step {
	nodeKind, _, _ := graph.SplitDataID(id)
	return graph.Step{Node: graph.Node{ID: id, Kind: nodeKind}, Edge: graph.Edge{Type: typ, Kind: kind, Sites: sites}}
}

// foldedView is a folded edge as these specs compare it.
type foldedView struct {
	To, Type, Kind string
	Sites          []graph.Site
	Properties     map[string]string
	From           []int
}

func foldedViews(folded []graph.Folded) []foldedView {
	views := []foldedView{}
	for _, f := range folded {
		views = append(views, foldedView{
			To: f.Step.Node.ID, Type: string(f.Step.Edge.Type), Kind: f.Step.Edge.Kind,
			Sites: f.Step.Edge.Sites, Properties: f.Step.Edge.Properties, From: f.From,
		})
	}
	return views
}

var _ = Describe("FoldMembers", func() {
	const (
		path       = "procedure:Settle"
		readStatus = "SELECT STATUSCODE, POLICYGUID FROM AsPolicy"
		closed     = "SELECT 1 FROM AsPolicy WHERE STATUSCODE = '09'"
		lapsed     = "SELECT 1 FROM AsPolicy WHERE STATUSCODE = '08'"
	)
	var (
		read  = uir.RelationshipTypeRead
		write = uir.RelationshipTypeWrite
		at    = func(line int, text string) graph.Site { return graph.Site{Path: path, Line: line, Text: text} }
	)
	// A body that reads two columns of AsPolicy on line 3, one again on line 7
	// in two statements, the table itself on lines 3 (a site its columns stand
	// for) and 9, writes the table on line 3, and writes a field of Policy and
	// the entity itself on line 2.
	steps := func() []graph.Step {
		return []graph.Step{
			dataStep("column:AsPolicy.STATUSCODE", read, "sql", at(3, readStatus)),
			dataStep("column:AsPolicy.POLICYGUID", read, "sql", at(3, readStatus)),
			dataStep("column:AsPolicy.STATUSCODE", read, "sql", at(7, lapsed), at(7, closed)),
			dataStep("table:AsPolicy", read, "sql-fill", at(9, "Procedure")),
			dataStep("table:AsPolicy", read, "sql", at(3, readStatus)),
			dataStep("field:Policy.Name", write, "mathupdate", at(2, "MathUpdate")),
			dataStep("entity:Policy", write, "mathupdate", at(2, "MathUpdate")),
			dataStep("table:AsPolicy", write, "sql", at(3, "UPDATE AsPolicy")),
		}
	}
	fold := func(folding bool) []foldedView {
		return foldedViews(foldSteps(steps(), graph.Folding{MemberOf: memberOf, Owner: ownerNode, Fold: folding}))
	}

	It("folds each member into its owner, one edge per owner and type, naming the members and every site once", func() {
		Expect(fold(true)).To(Equal([]foldedView{
			{
				To: "table:AsPolicy", Type: "read", Kind: "sql, sql-fill",
				Sites:      []graph.Site{at(3, readStatus), at(7, lapsed), at(7, closed), at(9, "Procedure")},
				Properties: map[string]string{"columns": "POLICYGUID, STATUSCODE"},
				From:       []int{0, 1, 2, 3, 4},
			},
			{
				To: "entity:Policy", Type: "write", Kind: "mathupdate",
				Sites: []graph.Site{at(2, "MathUpdate")}, Properties: map[string]string{"fields": "Name"}, From: []int{5, 6},
			},
			{To: "table:AsPolicy", Type: "write", Kind: "sql", Sites: []graph.Site{at(3, "UPDATE AsPolicy")}, From: []int{7}},
		}))
	})

	It("draws each member apart, leaving out an owner's site a covering member of the same type stands for", func() {
		Expect(fold(false)).To(Equal([]foldedView{
			{
				To: "column:AsPolicy.STATUSCODE", Type: "read", Kind: "sql",
				Sites: []graph.Site{at(3, readStatus), at(7, lapsed), at(7, closed)}, From: []int{0, 2},
			},
			{To: "column:AsPolicy.POLICYGUID", Type: "read", Kind: "sql", Sites: []graph.Site{at(3, readStatus)}, From: []int{1}},
			{To: "table:AsPolicy", Type: "read", Kind: "sql-fill", Sites: []graph.Site{at(9, "Procedure")}, From: []int{3}},
			{To: "field:Policy.Name", Type: "write", Kind: "mathupdate", Sites: []graph.Site{at(2, "MathUpdate")}, From: []int{5}},
			{To: "entity:Policy", Type: "write", Kind: "mathupdate", Sites: []graph.Site{at(2, "MathUpdate")}, From: []int{6}},
			{To: "table:AsPolicy", Type: "write", Kind: "sql", Sites: []graph.Site{at(3, "UPDATE AsPolicy")}, From: []int{7}},
		}))
	})

	It("matches a covering member's owner to the owner node whatever its case", func() {
		folded := foldSteps([]graph.Step{
			dataStep("column:aspolicy.STATUSCODE", read, "sql", at(3, readStatus)),
			dataStep("table:AsPolicy", read, "sql", at(3, readStatus)),
		}, graph.Folding{MemberOf: memberOf, Owner: ownerNode})

		Expect(foldedViews(folded)).To(Equal([]foldedView{
			{To: "column:aspolicy.STATUSCODE", Type: "read", Kind: "sql", Sites: []graph.Site{at(3, readStatus)}, From: []int{0}},
		}))
	})

	It("names an edge several constructs draw by their names sorted, whichever is met first", func() {
		kinds := func(first, second string) string {
			folded := foldSteps([]graph.Step{
				dataStep("table:AsActivity", read, first), dataStep("table:AsActivity", read, second),
			}, graph.Folding{MemberOf: memberOf, Owner: ownerNode})
			return folded[0].Step.Edge.Kind
		}

		Expect([]string{kinds("sql-fill", "sql"), kinds("sql", "sql-fill, sql")}).To(Equal([]string{"sql, sql-fill", "sql, sql-fill"}))
	})

	It("passes a step to a node that is no member through, keeping its node and properties", func() {
		leaf := graph.Step{
			Node: graph.Node{ID: "unresolved:sql:parse", Kind: "sql-unparsed", Unresolved: true},
			Edge: graph.Edge{Type: read, Kind: "sql", Properties: map[string]string{"why": "parse"}, Sites: []graph.Site{at(4, "SELECT FROM")}},
		}
		folded := foldSteps([]graph.Step{leaf}, graph.Folding{MemberOf: memberOf, Owner: ownerNode, Fold: true})

		Expect(folded).To(Equal([]graph.Folded{{Step: leaf, From: []int{0}}}))
	})

	It("refuses members of one owner listed under different properties", func() {
		listedAs := func(node graph.Node) (graph.Member, bool) {
			kind, name, _ := graph.SplitDataID(node.ID)
			return graph.Member{Owner: "table:AsPolicy", Name: name, Property: kind + "s"}, kind == graph.DataColumn || kind == graph.DataField
		}
		_, err := graph.FoldMembers([]graph.Step{
			dataStep("column:AsPolicy.STATUSCODE", read, "sql", at(3, readStatus)),
			dataStep("field:Policy.StatusCode", read, "field-ref", at(5, "MathVariable")),
		}, graph.Folding{MemberOf: listedAs, Owner: ownerNode, Fold: true})

		Expect(err).To(MatchError("graph: table:AsPolicy folds members listed as columns and as fields; an owner lists its members under one property"))
	})

	Describe("Surviving", func() {
		It("names the steps drawn apart, leaving out those every site of which a covering member stands for", func() {
			Expect(graph.Surviving(steps(), memberOf)).To(Equal([]int{0, 1, 2, 3, 5, 6, 7}))
		})

		It("keeps a step with no sites, which no member can stand for", func() {
			Expect(graph.Surviving([]graph.Step{
				dataStep("column:AsPolicy.STATUSCODE", read, "sql", at(3, readStatus)), dataStep("table:AsPolicy", read, "sql"),
			}, memberOf)).To(Equal([]int{0, 1}))
		})
	})
})

// foldSteps is FoldMembers of steps that fold without error.
func foldSteps(steps []graph.Step, folding graph.Folding) []graph.Folded {
	GinkgoHelper()
	folded, err := graph.FoldMembers(steps, folding)
	Expect(err).NotTo(HaveOccurred())
	return folded
}
