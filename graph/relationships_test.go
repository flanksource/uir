package graph_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
)

func methodID(typeName, method string) uir.Identifier {
	return uir.Identifier{Type: typeName, Method: method}
}

func methodRef(typeName, method string) uir.Node {
	return uir.NewRef(methodID(typeName, method))
}

// describeByType places every identifier that names a type, grouped by it, and
// cannot place one that does not.
func describeByType(id uir.Identifier) graph.Node {
	if id.Type == "" {
		return graph.Node{Unresolved: true}
	}
	return graph.Node{Label: id.Method, Group: id.Type}
}

// named is a graph whose ids are replaced by the identifiers they stand for.
type named struct {
	Nodes []string
	Edges []string
}

func namedShape(g *graph.Graph) named {
	GinkgoHelper()
	names := map[string]string{}
	out := named{}
	for _, node := range g.Nodes {
		names[node.ID] = node.Identifier.String()
		out.Nodes = append(out.Nodes, fmt.Sprintf("%s@%d", node.Identifier.String(), node.Depth))
	}
	for _, edge := range g.Edges {
		out.Edges = append(out.Edges, fmt.Sprintf("%s>%s %s", names[edge.From], names[edge.To], edge.Type))
	}
	return out
}

var _ = Describe("FromRelationships", func() {
	var (
		run   = methodID("A", "run")
		save  = methodID("B", "save")
		flush = methodID("C", "flush")
	)
	relationships := func() []uir.UIRRelationship {
		guarded := uir.NewRelationship(uir.RelationshipTypeCall, run.AsRef(), save.AsRef()).
			Source("a.go", 7, 7).Text("b.save(order)").Kind("method-call").Via("CallExpr").Build()
		guarded.Column = new(3)
		guarded.Guards = []uir.ConditionStmt{
			uir.NewCondition(uir.VarExpr("ok")),
			uir.NewCondition(uir.UnaryExpr(uir.UnaryOpNot, uir.VarExpr("done"))),
		}
		return []uir.UIRRelationship{
			guarded,
			uir.NewRelationship(uir.RelationshipTypeCall, save.AsRef(), flush.AsRef()).Build(),
			uir.NewRelationship(uir.RelationshipTypeDispatch, run.AsRef(), flush.AsRef()).Build(),
		}
	}
	source := func(rels []uir.UIRRelationship) graph.Source {
		GinkgoHelper()
		src, err := graph.FromRelationships(rels, describeByType)
		Expect(err).NotTo(HaveOccurred())
		return src
	}

	It("serves callees and callers from the same relationships", func() {
		src := source(relationships())
		edges := []string{"A:run>B:save call", "A:run>C:flush dispatch", "B:save>C:flush call"}

		Expect(namedShape(build(src, graph.DirectionCallees, 2, run.IdentityKey()))).To(Equal(named{
			Nodes: []string{"A:run@0", "B:save@1", "C:flush@1"},
			Edges: edges,
		}))
		Expect(namedShape(build(src, graph.DirectionCallers, 2, flush.IdentityKey()))).To(Equal(named{
			Nodes: []string{"A:run@-1", "B:save@-1", "C:flush@0"},
			Edges: edges,
		}))
	})

	It("describes each node by its identity key, filling in what describe leaves out", func() {
		Expect(source(relationships()).Describe(context.Background(), save.IdentityKey())).To(Equal(graph.Node{
			ID:         save.IdentityKey(),
			Identifier: save,
			Kind:       string(uir.NodeTypeMethod),
			Label:      "save",
			Group:      "B",
		}))
	})

	It("carries each call site's kind, location, text and guard text", func() {
		g := build(source(relationships()), graph.DirectionCallees, 1, run.IdentityKey())

		Expect(g.Edges[0]).To(Equal(graph.Edge{
			ID:   run.IdentityKey() + "|" + save.IdentityKey() + "|call",
			From: run.IdentityKey(),
			To:   save.IdentityKey(),
			Type: uir.RelationshipTypeCall,
			Kind: "method-call",
			Sites: []graph.Site{{
				Path: "a.go", Line: 7, Column: 3, Text: "b.save(order)", Guards: []string{"ok", "!done"},
			}},
		}))
		Expect(g.Edges[1].Sites).To(Equal([]graph.Site{{}}), "a relationship with no source is still one call site")
	})

	It("reports a target describe cannot place as unresolved, named by its identifier", func() {
		unplaced := uir.Identifier{Method: "callback"}
		src := source([]uir.UIRRelationship{uir.NewRelationship(uir.RelationshipTypeCall, run.AsRef(), unplaced.AsRef()).Build()})
		g := build(src, graph.DirectionCallees, 2, run.IdentityKey())

		Expect(g.Nodes[1]).To(Equal(graph.Node{
			ID:         unplaced.IdentityKey(),
			Identifier: unplaced,
			Kind:       string(uir.NodeTypeMethod),
			Label:      "callback",
			Depth:      1,
			In:         1,
			Unresolved: true,
		}))
		Expect(g.Omitted).To(Equal(graph.Omitted{Unresolved: 1}))
	})

	It("ignores relationships that are not calls or data access, even ones with no source", func() {
		rels := append(relationships(),
			uir.NewRelationship(uir.RelationshipTypeImport, run.AsRef(), save.AsRef()).Build(),
			uir.NewRelationship(uir.RelationshipTypeImplements, nil, save.AsRef()).Build(),
		)

		Expect(build(source(rels), graph.DirectionBoth, 2, run.IdentityKey())).To(Equal(build(source(relationships()), graph.DirectionBoth, 2, run.IdentityKey())))
	})

	Describe("data access", func() {
		policy := uir.Identifier{Type: "AsPolicy", NodeType: uir.NodeTypeRecord}
		withData := func() []uir.UIRRelationship {
			return append(relationships(),
				uir.NewRelationship(uir.RelationshipTypeRead, run.AsRef(), policy.AsRef()).Build(),
				uir.NewRelationship(uir.RelationshipTypeWrite, save.AsRef(), policy.AsRef()).Build(),
			)
		}
		accessShape := func(access ...uir.RelationshipType) named {
			GinkgoHelper()
			opts := graph.Options{Direction: graph.DirectionCallers, Depth: 1, Limit: graph.DefaultLimit, Access: access}
			return namedShape(buildWith(source(withData()), opts, policy.IdentityKey()))
		}

		It("draws reads and writes as edges to what they access", func() {
			Expect(accessShape()).To(Equal(named{
				Nodes: []string{"A:run@-1", "B:save@-1", "AsPolicy@0"},
				// The call between the two readers is drawn back from the depth bound.
				Edges: []string{"A:run>B:save call", "A:run>AsPolicy read", "B:save>AsPolicy write"},
			}))
		})

		It("leaves out the access types Options.Access does not follow", func() {
			Expect(accessShape(uir.RelationshipTypeWrite)).To(Equal(named{
				Nodes: []string{"B:save@-1", "AsPolicy@0"},
				Edges: []string{"B:save>AsPolicy write"},
			}))
		})

		It("draws only the calls when Options.Access follows only calls", func() {
			opts := graph.Options{Direction: graph.DirectionCallees, Depth: 2, Limit: graph.DefaultLimit, Access: []uir.RelationshipType{uir.RelationshipTypeCall}}
			Expect(buildWith(source(withData()), opts, run.IdentityKey())).To(Equal(build(source(relationships()), graph.DirectionCallees, 2, run.IdentityKey())))
		})
	})

	DescribeTable("rejects a call, dispatch, read or write relationship with a missing end",
		func(relType uir.RelationshipType, from, to uir.Node, want string) {
			rels := append(relationships(), uir.NewRelationship(relType, from, to).Source("a.go", 9, 9).Build())
			_, err := graph.FromRelationships(rels, describeByType)
			Expect(err).To(MatchError(ContainSubstring(want)))
		},
		Entry("a call with no source", uir.RelationshipTypeCall, nil, methodRef("B", "save"), "relationships[3]: call to B:save at a.go:9 has no source"),
		Entry("a dispatch with no source", uir.RelationshipTypeDispatch, nil, methodRef("B", "save"), "relationships[3]: dispatch to B:save at a.go:9 has no source"),
		Entry("a call with no target", uir.RelationshipTypeCall, methodRef("A", "run"), nil, "relationships[3]: call from A:run at a.go:9 has no target"),
		Entry("a write with no source", uir.RelationshipTypeWrite, nil, methodRef("B", "save"), "relationships[3]: write to B:save at a.go:9 has no source"),
		Entry("a read with no target", uir.RelationshipTypeRead, methodRef("A", "run"), nil, "relationships[3]: read from A:run at a.go:9 has no target"),
	)

	It("rejects a missing describe", func() {
		_, err := graph.FromRelationships(relationships(), nil)
		Expect(err).To(MatchError(ContainSubstring("describe is required")))
	})

	It("reports an id no relationship names instead of an empty neighbourhood", func() {
		src := source(relationships())
		_, outErr := src.Out(context.Background(), "nowhere")
		_, inErr := src.In(context.Background(), "nowhere")

		Expect(outErr).To(MatchError(ContainSubstring(`"nowhere"`)))
		Expect(inErr).To(MatchError(ContainSubstring(`"nowhere"`)))
	})

	It("draws the guarded calls CollectRelationships finds in a method body", func() {
		orders := uir.Identifier{Type: "Orders"}
		validate := uir.NewMethodCall("validate", orders).Build()
		persist := uir.NewMethodCall("persist", orders).Build()
		process := uir.NewMethod("process", orders).WithBody(uir.NewBlock().
			WithStatement(uir.NewIf(uir.ExprStmt{MethodCall: &validate}).
				WithThen(uir.NewBlock().WithStatement(persist).Build()).
				Build()).
			Build()).
			Build()

		g := build(source(uir.CollectRelationships(process)), graph.DirectionCallees, 1, process.GetIdentifier().IdentityKey())

		Expect(namedShape(g)).To(Equal(named{
			Nodes: []string{"Orders:process@0", "Orders:persist@1", "Orders:validate@1"},
			Edges: []string{"Orders:process>Orders:persist call", "Orders:process>Orders:validate call"},
		}))
		Expect(g.Edges[0].Sites).To(Equal([]graph.Site{{Guards: []string{"Orders.validate()"}}}))
		Expect(g.Edges[1].Sites).To(Equal([]graph.Site{{}}))
	})
})
