package query_test

import (
	"slices"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// publishedSite is the graph site of an occurrence at text inside context of one fixture source.
func publishedSite(path, source, context, text, siteText string) graph.Site {
	GinkgoHelper()
	at, _ := publishedText(source).span(context, text)
	return graph.Site{Path: path, Line: at[0], Column: at[1], Text: siteText}
}

var _ = Describe("the graph of an external producer's symbols", func() {
	const (
		call  = uir.RelationshipTypeCall
		read  = uir.RelationshipTypeRead
		write = uir.RelationshipTypeWrite
	)
	everything := []uir.RelationshipType{call, read, write}

	DescribeTable("draws the calls, reads and writes of custom callables to nodes named by their symbols",
		func(ctx SpecContext, backend string) {
			pipeline, scope := insurancePipeline(ctx, openQueryDatabase(ctx, backend))
			ids := insuranceIdentities()

			out := callGraph(ctx, pipeline, query.GraphOptions{Selector: "func:Apply", Direction: graph.DirectionCallees, Depth: 2, Access: everything, Scope: scope})
			Expect(graphEdges(out)).To(ConsistOf(
				graphEdge{From: "Apply", To: "Calc", Type: call},
				graphEdge{From: "Apply", To: "Policy.Amount", Type: read},
				graphEdge{From: "Apply", To: "PREMIUM", Type: read},
				graphEdge{From: "Calc", To: "Policy.Amount", Type: write},
				graphEdge{From: "Calc", To: "PREMIUM.AMOUNT", Type: write},
			))
			Expect(edgeBetween(out, "Apply", "Calc", call).Sites).To(Equal([]graph.Site{
				publishedSite(applyPath, applySource, `<Call rule="Calc"/>`, "Calc", `<Call rule="Calc"/>`),
			}), "a call site keeps the text the producer recorded")
			Expect(edgeBetween(out, "Apply", "PREMIUM", read).Sites).To(Equal([]graph.Site{
				publishedSite(applyPath, applySource, `<Query table="PREMIUM"/>`, "PREMIUM", ""),
			}))
			Expect(out.Omitted).To(Equal(graph.Omitted{}), "a producer's sources have no Go syntax to read guards from, and are not unreadable")

			root := graphNode(out, "Apply")
			Expect([]any{root.ID, root.Kind, root.Group, root.Properties}).To(Equal([]any{
				indexer.SymbolID(ids.apply), "acme.rule", insRules,
				map[string]string{query.GraphPropertySymbolKind: "acme.rule", query.GraphPropertyIdentifierID: applyGUID.String()},
			}), "a node names its symbol, its custom kind, its package and the producer's id")
			column := graphNode(out, "PREMIUM.AMOUNT")
			Expect([]any{column.ID, column.Kind, column.Group, column.Properties}).To(Equal([]any{
				indexer.SymbolID(ids.premiumAmount), "column", insDatabase, map[string]string{query.GraphPropertySymbolKind: "column"},
			}))

			callsOnly := callGraph(ctx, pipeline, query.GraphOptions{Selector: "func:Apply", Direction: graph.DirectionCallees, Depth: 1, Scope: scope})
			Expect(graphEdges(callsOnly)).To(Equal([]graphEdge{{From: "Apply", To: "Calc", Type: call}}), "no access follows calls alone")
		},
		Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"),
	)

	DescribeTable("draws the callers of a custom callable and the readers and writers of fields, columns, tables and records",
		func(ctx SpecContext, backend string) {
			pipeline, scope := insurancePipeline(ctx, openQueryDatabase(ctx, backend))
			ids := insuranceIdentities()
			// callers are the edges into the root, leaving out those Build draws between two of its callers.
			callers := func(id string, access ...uir.RelationshipType) []graphEdge {
				result := callGraph(ctx, pipeline, query.GraphOptions{Symbol: id, Direction: graph.DirectionCallers, Depth: 1, Access: access, Scope: scope})
				root := graphNodeByID(result, id).Label
				return slices.DeleteFunc(graphEdges(result), func(edge graphEdge) bool { return edge.To != root })
			}
			Expect(callers(indexer.SymbolID(ids.calc), everything...)).To(Equal([]graphEdge{{From: "Apply", To: "Calc", Type: call}}))
			Expect(callers(indexer.SymbolID(ids.amount), everything...)).To(ConsistOf(
				graphEdge{From: "Apply", To: "Policy.Amount", Type: read}, graphEdge{From: "Calc", To: "Policy.Amount", Type: write}))
			Expect(callers(indexer.SymbolID(ids.premiumAmount), everything...)).To(Equal([]graphEdge{{From: "Calc", To: "PREMIUM.AMOUNT", Type: write}}))
			Expect(callers(indexer.SymbolID(ids.premium), everything...)).To(ConsistOf(
				graphEdge{From: "Apply", To: "PREMIUM", Type: read}, graphEdge{From: "Calc", To: "PREMIUM", Type: write}),
				"a table is read by its readers and written by the writers of its columns")
			Expect(callers(indexer.SymbolID(ids.policy), everything...)).To(ConsistOf(
				graphEdge{From: "Apply", To: "Policy", Type: read}, graphEdge{From: "Calc", To: "Policy", Type: write}),
				"a record is read and written through its fields")
			Expect(callers(indexer.SymbolID(ids.policy), write)).To(Equal([]graphEdge{{From: "Calc", To: "Policy", Type: write}}))

			table := callGraph(ctx, pipeline, query.GraphOptions{Symbol: indexer.SymbolID(ids.premium), Direction: graph.DirectionCallers, Depth: 1, Access: everything, Scope: scope})
			Expect(edgeBetween(table, "Calc", "PREMIUM", write).Sites).To(Equal([]graph.Site{
				publishedSite(calcPath, calcSource, `<Update column="PREMIUM.AMOUNT"/>`, "PREMIUM.AMOUNT", ""),
			}), "a column's write is the table's write site")

			field := callGraph(ctx, pipeline, query.GraphOptions{Selector: "field:Policy.Amount", Direction: graph.DirectionCallers, Depth: 1, Access: []uir.RelationshipType{read}, Scope: scope})
			Expect(graphEdges(field)).To(Equal([]graphEdge{{From: "Apply", To: "Policy.Amount", Type: read}}), "a selector roots the graph at a field")
		},
		Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"),
	)

	DescribeTable("draws the callers of a custom type kind that bodies include, beside its readers, each as the access allows",
		func(ctx SpecContext, backend string) {
			pipeline, scope := insurancePipeline(ctx, openQueryDatabase(ctx, backend))
			screen := indexer.SymbolID(insuranceIdentities().screen)
			callers := func(access ...uir.RelationshipType) []graphEdge {
				return graphEdges(callGraph(ctx, pipeline, query.GraphOptions{Symbol: screen, Direction: graph.DirectionCallers, Depth: 1, Access: access, Scope: scope}))
			}
			Expect(callers(everything...)).To(ConsistOf(
				graphEdge{From: "Quote", To: "PolicyScreen", Type: call}, graphEdge{From: "Quote", To: "PolicyScreen", Type: read}))
			Expect(callers()).To(Equal([]graphEdge{{From: "Quote", To: "PolicyScreen", Type: call}}), "the default access follows the includes alone")
			Expect(callers(read, write)).To(Equal([]graphEdge{{From: "Quote", To: "PolicyScreen", Type: read}}), "an access without call draws no include")

			included := callGraph(ctx, pipeline, query.GraphOptions{Symbol: screen, Direction: graph.DirectionCallers, Depth: 1, Scope: scope})
			Expect(edgeBetween(included, "Quote", "PolicyScreen", call).Sites).To(Equal([]graph.Site{
				publishedSite(quotePath, quoteSource, `<Include screen="PolicyScreen"/>`, "PolicyScreen", `<Include screen="PolicyScreen"/>`),
			}), "an include site keeps the text the producer recorded")
		},
		Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"),
	)
})
