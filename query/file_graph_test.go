package query_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	ordersFilePath   = "service/orders/orders.go"
	ordersFileSource = `package orders

import "example.org/billing"

type Order struct{ Total int }

func (o Order) Valid() bool { return o.Total > 0 }

func Submit(order Order) error {
	if order.Valid() {
		charge(order)
	}
	return billing.Invoice(order.Total)
}

func charge(order Order) { audit() }

func audit() {}

func Idle() {}
`
	// editedFlow is an edited copy of the indexed flow.go: Handle alone, calling functions the file no
	// longer declares.
	editedFlow = `package flow

import "example.org/flow/notify"

func Handle(order Order, channel notify.Channel) error {
	audit(order)
	return Process(order, channel, 2)
}
`
)

func ordersFileLine(snippet string) int {
	GinkgoHelper()
	Expect(strings.Count(ordersFileSource, snippet)).To(Equal(1), "%q appears once in the fixture", snippet)
	before, _, _ := strings.Cut(ordersFileSource, snippet)
	return strings.Count(before, "\n") + 1
}

func standaloneGraph(ctx SpecContext, options query.GraphOptions) query.GraphResult {
	GinkgoHelper()
	result, err := query.StandaloneGraph(ctx, options)
	Expect(err).ToNot(HaveOccurred())
	return result
}

var _ = Describe("the call graph of one file", func() {
	ordersFile := func() *query.GraphFile {
		return &query.GraphFile{Path: ordersFilePath, Content: []byte(ordersFileSource)}
	}

	Describe("standalone Go", func() {
		It("draws the file's calls with their guards and leaves out-of-file callees unresolved", func(ctx SpecContext) {
			result := standaloneGraph(ctx, query.GraphOptions{File: ordersFile(), Selector: "Submit", Direction: graph.DirectionCallees, Depth: 2})

			Expect(graphEdges(result)).To(ConsistOf(
				graphEdge{From: "Submit", To: "charge", Type: uir.RelationshipTypeCall},
				graphEdge{From: "Submit", To: "order.Valid", Type: uir.RelationshipTypeCall},
				graphEdge{From: "Submit", To: "billing.Invoice", Type: uir.RelationshipTypeCall},
				graphEdge{From: "charge", To: "audit", Type: uir.RelationshipTypeCall},
			))
			Expect(edgeBetween(result, "Submit", "charge", uir.RelationshipTypeCall).Sites).To(Equal([]graph.Site{{
				Path: ordersFilePath, Line: ordersFileLine("charge(order)\n"), Column: 3, Text: "charge(order)", Guards: []string{"order.Valid()"},
			}}))
			invoice := graphNode(result, "billing.Invoice")
			Expect([]any{invoice.Unresolved, invoice.Group}).To(Equal([]any{true, "example.org/billing"}))
			submit := graphNode(result, "Submit")
			Expect([]any{submit.Unresolved, submit.Group, submit.Location}).To(Equal([]any{false, "orders",
				&graph.Location{Path: ordersFilePath, Line: ordersFileLine("func Submit"), Column: 6}}))
			Expect(result.Roots).To(Equal([]string{submit.ID}))
			Expect(result.Omitted.Unresolved).To(Equal(2))
			Expect(result.Diagnostics).To(BeEmpty())
		})

		It("roots the graph at a function with no calls", func(ctx SpecContext) {
			result := standaloneGraph(ctx, query.GraphOptions{File: ordersFile(), Selector: "Idle"})
			Expect([]any{graphLabels(result), result.Edges}).To(Equal([]any{[]string{"Idle"}, []graph.Edge{}}))
		})

		It("refuses to pick a root among several functions", func(ctx SpecContext) {
			_, err := query.StandaloneGraph(ctx, query.GraphOptions{File: ordersFile()})
			var invalid *query.InvalidQueryError
			Expect(errors.As(err, &invalid)).To(BeTrue(), "%v", err)
			Expect(invalid.Message).To(ContainSubstring("names no root"))
			Expect(invalid.Hint).To(ContainSubstring("Order.Valid, Submit, charge, audit, Idle"))
		})

		DescribeTable("rejects options a file graph cannot take", func(ctx SpecContext, options query.GraphOptions, message string) {
			_, err := query.StandaloneGraph(ctx, options)
			var invalid *query.InvalidQueryError
			Expect(errors.As(err, &invalid)).To(BeTrue(), "%v", err)
			Expect(invalid.Message).To(ContainSubstring(message))
		},
			Entry("a symbol id", query.GraphOptions{File: &query.GraphFile{Path: ordersFilePath, Content: []byte(ordersFileSource)}, Symbol: "0123"}, "symbol"),
			Entry("stdin with no path", query.GraphOptions{File: &query.GraphFile{Content: []byte(ordersFileSource)}, Selector: "Submit"}, "path"),
			Entry("an unknown extension", query.GraphOptions{File: &query.GraphFile{Path: "orders.rb", Content: []byte("")}, Selector: "Submit"}, ".rb"),
			Entry("SQL with no dialect", query.GraphOptions{File: &query.GraphFile{Path: "orders.sql", Content: []byte("")}, Selector: "Submit"}, "dialect"),
		)
	})

	DescribeTable("standalone SQL draws calls, table reads and table writes at their statements' lines",
		func(ctx SpecContext, fixture, dialect, selector string, call graphEdge, statement string, expected []graphEdge) {
			path := filepath.Join("..", "fixtures", "sql", fixture)
			content, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			result := standaloneGraph(ctx, query.GraphOptions{
				File: &query.GraphFile{Path: path, Content: content, Dialect: dialect}, Selector: selector, Direction: graph.DirectionCallees, Depth: 2,
				Access: []uir.RelationshipType{uir.RelationshipTypeCall, uir.RelationshipTypeRead, uir.RelationshipTypeWrite},
			})
			Expect(graphEdges(result)).To(ConsistOf(expected))
			Expect(result.Diagnostics).To(BeEmpty())
			sites := edgeBetween(result, call.From, call.To, uir.RelationshipTypeCall).Sites
			Expect(strings.Count(string(content), statement)).To(Equal(1), "%q appears once in the fixture", statement)
			before, _, _ := strings.Cut(string(content), statement)
			Expect(sites).To(HaveLen(1))
			Expect([]any{sites[0].Path, sites[0].Line, len(sites[0].Guards)}).To(Equal([]any{path, strings.Count(before, "\n") + 1, 1}),
				"the call is located at its statement and guarded by the IF it sits in")
		},
		Entry("T-SQL", "tsql_orders.sql", "tsql", "dbo.SubmitOrder",
			graphEdge{From: "SubmitOrder", To: "ValidateOrder"}, "EXEC dbo.ValidateOrder", []graphEdge{
				{From: "SubmitOrder", To: "dbo.Customers", Type: uir.RelationshipTypeRead},
				{From: "SubmitOrder", To: "ValidateOrder", Type: uir.RelationshipTypeCall},
				{From: "SubmitOrder", To: "dbo.Orders", Type: uir.RelationshipTypeWrite},
				{From: "SubmitOrder", To: "RejectOrder", Type: uir.RelationshipTypeCall},
				{From: "SubmitOrder", To: "dbo.OrderLines", Type: uir.RelationshipTypeRead},
				{From: "SubmitOrder", To: "dbo.OrderLines", Type: uir.RelationshipTypeWrite},
				{From: "ValidateOrder", To: "OrderTotal", Type: uir.RelationshipTypeCall},
				{From: "ValidateOrder", To: "dbo.Orders", Type: uir.RelationshipTypeRead},
				{From: "ValidateOrder", To: "dbo.Customers", Type: uir.RelationshipTypeRead},
				{From: "RejectOrder", To: "dbo.Orders", Type: uir.RelationshipTypeRead},
				{From: "RejectOrder", To: "dbo.RejectedOrders", Type: uir.RelationshipTypeWrite},
				{From: "OrderTotal", To: "dbo.OrderLines", Type: uir.RelationshipTypeRead},
			}),
		Entry("PostgreSQL", "postgres_orders.sql", "postgres", "submit_order",
			graphEdge{From: "submit_order", To: "validate_order"}, "PERFORM public.validate_order", []graphEdge{
				{From: "submit_order", To: "public.customers", Type: uir.RelationshipTypeRead},
				{From: "submit_order", To: "validate_order", Type: uir.RelationshipTypeCall},
				{From: "submit_order", To: "public.orders", Type: uir.RelationshipTypeWrite},
				{From: "submit_order", To: "public.order_lines", Type: uir.RelationshipTypeRead},
				{From: "submit_order", To: "public.order_lines", Type: uir.RelationshipTypeWrite},
				{From: "validate_order", To: "order_total", Type: uir.RelationshipTypeCall},
				{From: "validate_order", To: "public.orders", Type: uir.RelationshipTypeRead},
				{From: "validate_order", To: "public.customers", Type: uir.RelationshipTypeRead},
				{From: "order_total", To: "public.order_lines", Type: uir.RelationshipTypeRead},
			}),
	)

	It("draws anonymous T-SQL statements across GO batches from one script root", func(ctx SpecContext) {
		const source = "SELECT * FROM dbo.Source;\nGO\nEXEC dbo.Notify;\nGO\nINSERT INTO dbo.Audit (Id) VALUES (1)"
		result := standaloneGraph(ctx, query.GraphOptions{
			File:      &query.GraphFile{Path: "test.sql", Content: []byte(source), Dialect: "tsql"},
			Direction: graph.DirectionCallees, Depth: 1,
		})

		root := graphNode(result, "test.sql")
		Expect([]any{root.Kind, root.Location, result.Roots}).To(Equal([]any{
			"script", &graph.Location{Path: "test.sql", Line: 1}, []string{root.ID},
		}))
		Expect(result.Diagnostics).To(BeEmpty())
		Expect(graphEdges(result)).To(ConsistOf(
			graphEdge{From: "test.sql", To: "dbo.Source", Type: uir.RelationshipTypeRead},
			graphEdge{From: "test.sql", To: "dbo.Notify", Type: uir.RelationshipTypeCall},
			graphEdge{From: "test.sql", To: "dbo.Audit", Type: uir.RelationshipTypeWrite},
		))
		Expect(edgeBetween(result, "test.sql", "dbo.Notify", uir.RelationshipTypeCall).Sites).To(Equal([]graph.Site{{
			Path: "test.sql", Line: 3, Text: "EXEC dbo.Notify",
		}}))
	})

	Describe("backed by the index", func() {
		It("graphs SQL against an older snapshot without reading implements facts for unrelated symbols", func(ctx SpecContext) {
			database := openQueryDatabase(ctx, "sqlite")
			pipeline, scope, _ := flowPipeline(ctx, database)
			var rows []storage.Symbol
			for _, candidate := range []struct{ kind, name string }{{"func", "Audit"}, {"type", "Ledger"}} {
				identity := indexer.Identity{ModuleKey: flowModule, PackagePath: flowModule + "/dbo", Kind: candidate.kind, Name: candidate.name}
				rows = append(rows, storage.Symbol{
					ID: indexer.SymbolID(identity), IdentityVersion: 1, CanonicalKey: identity.CanonicalKey(),
					ModuleKey: identity.ModuleKey, PackagePath: identity.PackagePath, Kind: identity.Kind,
					Name: identity.Name, SearchName: storage.SearchName(identity.Name), Visibility: "exported", ParameterTypes: storage.JSON(`[]`),
				})
			}
			Expect(storage.AssignSymbolHandles(ctx, database, rows)).To(Succeed())
			Expect(storage.UpsertSymbols(ctx, database, rows, 1)).To(Succeed())
			Expect(database.Where("1 = 1").Delete(&storage.ImplementsFacts{}).Error).To(Succeed())

			result, err := pipeline.Graph(ctx, query.GraphOptions{
				File:      &query.GraphFile{Path: "test.sql", Content: []byte("EXEC dbo.Audit; SELECT * FROM dbo.Ledger;"), Dialect: "tsql"},
				Direction: graph.DirectionCallees, Depth: 1, Scope: scope,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(graphEdges(result)).To(ConsistOf(
				graphEdge{From: "test.sql", To: "dbo.Audit", Type: uir.RelationshipTypeCall},
				graphEdge{From: "test.sql", To: "dbo.Ledger", Type: uir.RelationshipTypeRead},
			))
		})

		It("continues the walk into the index from an edited copy of an indexed file, and finds its callers there", func(ctx SpecContext) {
			pipeline, scope, _ := flowPipeline(ctx, openQueryDatabase(ctx, "sqlite"))
			indexed := callGraph(ctx, pipeline, query.GraphOptions{Selector: flowModule + ".Process", Direction: graph.DirectionCallees, Depth: 1, Scope: scope})

			result := callGraph(ctx, pipeline, query.GraphOptions{
				File: &query.GraphFile{Path: flowPath, Content: []byte(editedFlow)}, Selector: "Handle", Direction: graph.DirectionBoth, Depth: 2, Scope: scope,
			})

			depths := map[string]int{}
			for _, node := range result.Nodes {
				depths[node.Label] = node.Depth
			}
			Expect(depths).To(HaveKeyWithValue("Handle", 0))
			Expect(depths).To(HaveKeyWithValue("Process", 1))
			Expect(depths).To(HaveKeyWithValue("audit", 1))
			Expect(depths).To(HaveKeyWithValue("charge", 2), "the walk continues in the index past Process")
			Expect(depths).To(HaveKeyWithValue("Entry", -1))
			Expect(depths).To(HaveKeyWithValue("Top", -2))
			Expect(graphNode(result, "Process").ID).To(Equal(graphNode(indexed, "Process").ID), "Process resolves to its indexed symbol")
			Expect(graphNode(result, "Handle").Location).To(Equal(&graph.Location{Path: flowPath, Line: 5, Column: 6}), "the root comes from the file")
			Expect(edgeBetween(result, "Handle", "Process", uir.RelationshipTypeCall).Sites).To(Equal([]graph.Site{{
				Path: flowPath, Line: 7, Column: 9, Text: "Process(order, channel, 2)",
			}}))
		})

		It("refuses a path the selected snapshots do not hold", func(ctx SpecContext) {
			pipeline, scope, _ := flowPipeline(ctx, openQueryDatabase(ctx, "sqlite"))
			_, err := pipeline.Graph(ctx, query.GraphOptions{File: &query.GraphFile{Path: "missing.go", Content: []byte(editedFlow)}, Selector: "Handle", Scope: scope})
			var invalid *query.InvalidQueryError
			Expect(errors.As(err, &invalid)).To(BeTrue(), "%v", err)
			Expect(invalid.Hint).To(ContainSubstring("--index=false"))
		})
	})
})
