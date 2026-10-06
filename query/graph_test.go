package query_test

import (
	"os"
	"path/filepath"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

type calledAt struct {
	Symbol string
	Line   int
}

var _ = Describe("the call graph of indexed modules", func() {
	const (
		process = flowModule + ".Process"
		charge  = flowModule + ".charge"
	)

	DescribeTable("agrees with the compact operators on who calls whom", func(ctx SpecContext, backend string) {
		pipeline, scope, _ := flowPipeline(ctx, openQueryDatabase(ctx, backend))

		callees := callGraph(ctx, pipeline, query.GraphOptions{Selector: process, Direction: graph.DirectionCallees, Depth: 1, Exclude: []string{query.ExcludeNone}, Scope: scope})
		root := graphNode(callees, "Process")
		Expect(callees.Roots).To(Equal([]string{root.ID}))
		var drawn []calledAt
		for _, edge := range callees.Edges {
			if edge.From != root.ID || edge.Type != uir.RelationshipTypeCall || graphNodeByID(callees, edge.To).Unresolved {
				continue
			}
			for _, site := range edge.Sites {
				drawn = append(drawn, calledAt{Symbol: edge.To, Line: site.Line})
			}
		}
		var listed []calledAt
		for _, row := range runQuery(ctx, pipeline, process+" >", scope).Matches {
			Expect(row.SymbolID).ToNot(BeEmpty(), "every call in Process resolves")
			listed = append(listed, calledAt{Symbol: row.SymbolID, Line: *row.Line})
		}
		Expect(listed).To(HaveLen(13))
		Expect(drawn).To(ConsistOf(listed))

		callers := callGraph(ctx, pipeline, query.GraphOptions{Selector: charge, Direction: graph.DirectionCallers, Depth: 3, Scope: scope})
		drawnCallers := map[string]int{}
		for _, node := range callers.Nodes {
			if node.Depth != 0 {
				drawnCallers[node.ID] = -node.Depth
			}
		}
		listedCallers := map[string]int{}
		for _, row := range runQuery(ctx, pipeline, charge+" <<3", scope).Matches {
			listedCallers[row.SymbolID] = row.Depth
		}
		Expect(listedCallers).To(HaveLen(4))
		Expect(drawnCallers).To(Equal(listedCallers))
		Expect(graphLabels(callers)).To(Equal([]string{"Entry", "Handle", "Process", "Refill", "charge"}))
		Expect(callers.Omitted.BeyondDepth).To(Equal(1), "Top calls Entry from beyond the depth")
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	Describe("over one indexed checkout", func() {
		var (
			database *gorm.DB
			pipeline *query.Pipeline
			scope    query.ModuleScopeOptions
			checkout string
		)

		BeforeEach(func(ctx SpecContext) {
			database = openQueryDatabase(ctx, "sqlite")
			pipeline, scope, checkout = flowPipeline(ctx, database)
		})

		It("puts the guards of each construct on the call site it guards", func(ctx SpecContext) {
			result := callGraph(ctx, pipeline, query.GraphOptions{Selector: process, Direction: graph.DirectionCallees, Depth: 1, Scope: scope})
			guards := map[string][]string{}
			for _, edge := range result.Edges {
				if edge.Type == uir.RelationshipTypeCall {
					Expect(edge.Sites).To(HaveLen(1))
					guards[graphNodeByID(result, edge.To).Label] = edge.Sites[0].Guards
				}
			}
			Expect(guards).To(Equal(map[string][]string{
				"charge":       {"order.Total > 0"},
				"refund":       {"!(order.Total > 0)"},
				"wrap":         {`order.Kind == "gift"`},
				"ship":         {`!(order.Kind == "gift")`, "order.Ready"},
				"deliver":      {`order.Kind == "digital"`},
				"pack":         {`order.Kind == "gift" || order.Kind == "bulk"`},
				"shelve":       {`!(order.Kind == "digital" || order.Kind == "gift" || order.Kind == "bulk")`},
				"audit":        {"order.Total > 100"},
				"retry":        {"retries > 0"},
				"valid":        nil,
				"record":       {"valid(order)"},
				"Channel.Send": nil,
			}))
			Expect(edgeBetween(result, "Process", "ship", uir.RelationshipTypeCall).Sites).To(Equal([]graph.Site{{
				Path: flowPath, Line: flowLine("ship(order)"), Column: 3, Text: "ship(order)", Guards: []string{`!(order.Kind == "gift")`, "order.Ready"},
			}}))
			Expect(result.Omitted).To(Equal(graph.Omitted{Excluded: map[string]int{"errors": 1}}), "errors.New is in the standard library")

			unexcluded := callGraph(ctx, pipeline, query.GraphOptions{Selector: process, Direction: graph.DirectionCallees, Depth: 1, Exclude: []string{query.ExcludeNone}, Scope: scope})
			Expect(edgeBetween(unexcluded, "Process", "New", uir.RelationshipTypeCall).Sites).To(Equal([]graph.Site{{
				Path: flowPath, Line: flowLine(`errors.New("negative total")`), Column: 17, Text: `errors.New("negative total")`, Guards: []string{"order.Total < 0"},
			}}))
		})

		It("draws a call through an interface as a call to its method and a dispatch to each implementation", func(ctx SpecContext) {
			result := callGraph(ctx, pipeline, query.GraphOptions{Selector: process, Direction: graph.DirectionCallees, Depth: 1, Scope: scope})
			line := flowLine("channel.Send(order.Kind)")
			for target, kind := range map[string]uir.RelationshipType{
				"Channel.Send": uir.RelationshipTypeCall, "Email.Send": uir.RelationshipTypeDispatch, "SMS.Send": uir.RelationshipTypeDispatch,
			} {
				Expect(edgeBetween(result, "Process", target, kind).Sites).To(Equal([]graph.Site{{Path: flowPath, Line: line, Column: 17, Text: "channel.Send(order.Kind)"}}), target)
			}

			reverse := callGraph(ctx, pipeline, query.GraphOptions{Selector: flowModule + "/notify.Email.Send", Direction: graph.DirectionCallers, Depth: 1, Scope: scope})
			Expect(graphEdges(reverse)).To(Equal([]graphEdge{{From: "Process", To: "Email.Send", Type: uir.RelationshipTypeDispatch}}))
			Expect(reverse.Edges[0].Sites).To(Equal([]graph.Site{{Path: flowPath, Line: line, Column: 17, Text: "channel.Send(order.Kind)"}}))
		})

		It("ends direct and mutual recursion at a back edge", func(ctx SpecContext) {
			direct := callGraph(ctx, pipeline, query.GraphOptions{Selector: flowModule + ".countdown", Depth: 3, Scope: scope})
			Expect(graphEdges(direct)).To(Equal([]graphEdge{{From: "countdown", To: "countdown", Type: uir.RelationshipTypeCall}}))
			Expect(direct.Edges[0].Sites[0].Guards).To(Equal([]string{"n > 0"}))
			Expect(direct.Nodes).To(HaveLen(1))

			mutual := callGraph(ctx, pipeline, query.GraphOptions{Selector: flowModule + ".ping", Direction: graph.DirectionCallees, Depth: 8, Scope: scope})
			Expect(graphEdges(mutual)).To(ConsistOf(
				graphEdge{From: "ping", To: "pong", Type: uir.RelationshipTypeCall},
				graphEdge{From: "pong", To: "ping", Type: uir.RelationshipTypeCall},
			))
			Expect(graphLabels(mutual)).To(Equal([]string{"ping", "pong"}))
		})

		It("draws a call the index could not resolve as an unresolved leaf and counts it", func(ctx SpecContext) {
			result := callGraph(ctx, pipeline, query.GraphOptions{Selector: flowModule + ".Apply", Direction: graph.DirectionCallees, Scope: scope})
			hook := graphNode(result, "hook")
			Expect(hook.Unresolved).To(BeTrue())
			Expect(hook.Location).To(BeNil())
			Expect(hook.ID).To(HavePrefix("unresolved:"))
			Expect(edgeBetween(result, "Apply", "hook", uir.RelationshipTypeCall).Sites).To(Equal([]graph.Site{{
				Path: flowPath, Line: flowLine("hook()"), Column: 3, Text: "hook()", Guards: []string{"hook != nil"},
			}}))
			Expect(result.Omitted.Unresolved).To(Equal(1))
		})

		It("locates a declared node for the Explorer and leaves one declared outside the scope without a location", func(ctx SpecContext) {
			result := callGraph(ctx, pipeline, query.GraphOptions{Selector: process, Direction: graph.DirectionCallees, Depth: 1, Exclude: []string{query.ExcludeNone}, Scope: scope})
			root := graphNode(result, "Process")
			Expect(root.Kind).To(Equal("func"))
			Expect(root.Group).To(Equal(flowModule))
			Expect(root.Identifier.Method).To(Equal("Process"))
			browsed, err := pipeline.BrowseModules(ctx, scope)
			Expect(err).ToNot(HaveOccurred())
			Expect(root.Location).ToNot(BeNil())
			Expect(*root.Location).To(Equal(graph.Location{
				RootKey: flowModule, CheckoutPath: browsed.Sources[0].Location, SnapshotID: browsed.Sources[0].SnapshotID,
				SourceID: root.Location.SourceID, Path: flowPath, IdentityKey: root.Location.IdentityKey,
				Line: flowLine("func Process("), Column: 6,
			}))
			Expect(browsed.Nodes).To(ContainElement(And(
				HaveField("ID", root.Location.SourceID+":"+root.Location.IdentityKey), HaveField("Identifier.Method", "Process"),
			)))
			Expect(graphNode(result, "Email.Send").Group).To(Equal(flowModule + "/notify"))

			external := graphNode(result, "New")
			Expect(external.Group).To(Equal("errors"))
			Expect(external.Location).To(BeNil())
			Expect(external.Unresolved).To(BeFalse())
		})

		It("walks callers above the root and callees below it, and takes a symbol id in place of a selector", func(ctx SpecContext) {
			both := callGraph(ctx, pipeline, query.GraphOptions{Selector: process, Scope: scope})
			depths := map[string]int{}
			for _, node := range both.Nodes {
				depths[node.Label] = node.Depth
			}
			Expect(depths).To(And(
				HaveKeyWithValue("Process", 0), HaveKeyWithValue("Handle", -1), HaveKeyWithValue("Entry", -2),
				HaveKeyWithValue("charge", 1), HaveKeyWithValue("Email.Send", 1),
			))
			Expect(depths).ToNot(HaveKey("Refill"), "a callee's other callers are not walked")
			Expect(both.Stages).To(ContainElement(query.ResolutionStage{Name: "resolve", Value: process}))

			bySymbol := callGraph(ctx, pipeline, query.GraphOptions{Symbol: both.Roots[0], Scope: scope})
			Expect(bySymbol.Graph).To(Equal(both.Graph))
		})

		It("stops at the node limit and at the depth, and says so", func(ctx SpecContext) {
			limited := callGraph(ctx, pipeline, query.GraphOptions{Selector: process, Direction: graph.DirectionCallees, Depth: 1, Limit: 3, Scope: scope})
			Expect(limited.Nodes).To(HaveLen(3))
			Expect(limited.Omitted.NodeLimit).To(BeTrue())
			Expect(graphNode(limited, "Process").Out).To(Equal(14), "the root still reports every edge it has but the excluded one to errors.New")

			shallow := callGraph(ctx, pipeline, query.GraphOptions{Selector: charge, Direction: graph.DirectionCallers, Depth: 1, Scope: scope})
			Expect(graphLabels(shallow)).To(Equal([]string{"Process", "Refill", "charge"}))
			Expect(shallow.Omitted.BeyondDepth).To(Equal(1), "Handle calls Process from beyond the depth")
			Expect(edgeBetween(shallow, "Refill", "charge", uir.RelationshipTypeCall).Sites).To(HaveLen(2))
		})

		It("returns the callable candidates of an ambiguous selector with an empty graph", func(ctx SpecContext) {
			result := callGraph(ctx, pipeline, query.GraphOptions{Selector: "Send", Scope: scope})
			names := make([]string, 0, len(result.Candidates))
			for _, candidate := range result.Candidates {
				names = append(names, candidate.QueryName)
			}
			Expect(names).To(Equal([]string{
				flowModule + "/notify.Channel.Send", flowModule + "/notify.Email.Send", flowModule + "/notify.SMS.Send",
			}))
			Expect(result.Roots).To(Equal([]string{}))
			Expect(result.Nodes).To(Equal([]graph.Node{}))
			Expect(result.Edges).To(Equal([]graph.Edge{}))
			Expect(result.Stages).To(ContainElement(query.ResolutionStage{Name: "resolve", Value: "3 candidates"}))
		})

		DescribeTable("rejects a request it cannot draw",
			func(ctx SpecContext, options query.GraphOptions, message string) {
				options.Scope = scope
				_, err := pipeline.Graph(ctx, options)
				Expect(err).To(MatchError(ContainSubstring(message)))
			},
			Entry("neither selector nor symbol", query.GraphOptions{}, "requires a selector or a symbol"),
			Entry("both selector and symbol", query.GraphOptions{Selector: process, Symbol: "abc"}, "not both"),
			Entry("a depth beyond the bound", query.GraphOptions{Selector: process, Depth: 9}, "depth 9 is outside 1..8"),
			Entry("a negative depth", query.GraphOptions{Selector: process, Depth: -1}, "depth -1 is outside 1..8"),
			Entry("a limit beyond the bound", query.GraphOptions{Selector: process, Limit: 1001}, "limit 1001 is outside 1..1000"),
			Entry("an unknown direction", query.GraphOptions{Selector: process, Direction: "sideways"}, `direction "sideways"`),
			Entry("a relation in place of a selector", query.GraphOptions{Selector: process + " <"}, "must select symbols"),
			Entry("a selector that is not a query", query.GraphOptions{Selector: "func:Process &"}, "parse error"),
			Entry("a type", query.GraphOptions{Selector: flowModule + ".Order"}, "matched no function or method"),
			Entry("a field under the default call access", query.GraphOptions{Selector: "field:Order.Total"},
				"the graph of field "+flowModule+".Order.Total needs read or write access, access call follows neither"),
			Entry("a field under call access", query.GraphOptions{Selector: "field:Order.Total", Access: []uir.RelationshipType{uir.RelationshipTypeCall}},
				"needs read or write access"),
			Entry("an access that is not call, read or write", query.GraphOptions{Selector: process, Access: []uir.RelationshipType{uir.RelationshipTypeImport}}, `access "import" is not one of call, read, write`),
			Entry("an unindexed symbol name", query.GraphOptions{Selector: flowModule + ".Missing"}, "matched no indexed symbols"),
			Entry("an unknown symbol id", query.GraphOptions{Symbol: "0123456789abcdef"}, "matched no indexed symbols"),
		)

		It("keeps the calls of a source it can no longer read, without guards, and names the file", func(ctx SpecContext) {
			Expect(os.WriteFile(filepath.Join(checkout, flowPath), []byte(flowSource+"\n// edited after indexing\n"), 0o644)).To(Succeed())
			Expect(database.Where("1 = 1").Delete(&storage.SourceBlob{}).Error).To(Succeed())

			result := callGraph(ctx, pipeline, query.GraphOptions{Selector: process, Direction: graph.DirectionCallees, Depth: 1, Scope: scope})
			Expect(result.Omitted.UnreadableSource).To(Equal([]string{flowPath}))
			Expect(edgeBetween(result, "Process", "charge", uir.RelationshipTypeCall).Sites).To(Equal([]graph.Site{{
				Path: flowPath, Line: flowLine("charge(order)\n\t} else"), Column: 3, Text: "charge",
			}}))
			Expect(result.Edges).To(HaveLen(14), "every call but the excluded one to errors.New")
			Expect(result.Stages).To(ContainElement(And(HaveField("Name", "guards"), HaveField("Value", ContainSubstring(flowPath)))))
		})
	})
})

func graphNodeByID(result query.GraphResult, id string) graph.Node {
	GinkgoHelper()
	for _, node := range result.Nodes {
		if node.ID == id {
			return node
		}
	}
	Fail("the graph has no node " + id)
	return graph.Node{}
}
