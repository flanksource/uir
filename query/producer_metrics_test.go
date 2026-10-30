package query_test

import (
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	ruleRoot   = "example.org/rules"
	ruleKind   = "acme.rule"
	screenKind = "acme.screen"
)

// ruleMetrics are the metrics acme.rule declares; ruleMeasures every value a rule can record.
var (
	ruleMetrics  = map[string]storage.MetricSpec{"variables": {Description: "declared variables"}, "fan_in": {Description: "distinct callers"}}
	ruleMeasures = map[string]map[string]int64{
		"Alpha": {"variables": 14, "fan_in": 0}, "Beta": {"variables": 22, "fan_in": 3}, "Gamma": {"variables": 11, "fan_in": 2}, "Screen": {"fields": 7},
	}
)

// rulePublication publishes three acme.rule callables, Alpha calling Beta calling Gamma, and one
// acme.screen member. Each declaration records the values of exactly the metrics its kind declares;
// with no rule metrics the document is format 9.
func rulePublication(revision string, metrics map[string]storage.MetricSpec) indexer.Publication {
	GinkgoHelper()
	source := publishedText("Alpha\nBeta\nGamma\nScreen\n")
	publication := indexer.Publication{RootKey: ruleRoot, Name: "Rules", Location: indexer.ExternalLocation{URI: "test://rules"}, Revision: revision,
		Kinds: []indexer.KindDeclaration{
			{Name: ruleKind, Category: symbolhandle.CategoryCallable, Metrics: metrics},
			{Name: screenKind, Category: symbolhandle.CategoryMember, Metrics: map[string]storage.MetricSpec{"fields": {Description: "screen fields"}}},
		},
		Relationships: []storage.RelationshipKind{{Name: "acme.calls", Type: uir.RelationshipTypeReference}},
	}
	declared := map[string]map[string]storage.MetricSpec{ruleKind: metrics, screenKind: publication.Kinds[1].Metrics}
	var symbols []storage.DocumentSymbol
	for _, name := range []string{"Alpha", "Beta", "Gamma", "Screen"} {
		kind := ruleKind
		if name == "Screen" {
			kind = screenKind
		}
		identity := indexer.Identity{ModuleKey: ruleRoot, PackagePath: ruleRoot, Kind: kind, Name: name}
		declaration := source.declaration(identity, uir.Identifier{Module: ruleRoot, Package: ruleRoot, Method: name, NodeType: uir.NodeTypeFunction}, name, name)
		for metric := range declared[kind] {
			if declaration.Measures == nil {
				declaration.Measures = map[string]int64{}
			}
			declaration.Measures[metric] = ruleMeasures[name][metric]
		}
		symbols = append(symbols, declaration)
		publication.Symbols = append(publication.Symbols, indexer.PublishedSymbol{Identity: identity, Visibility: "exported"})
	}
	document := source.document("rules.txt", ruleRoot, symbols, nil)
	for i := range 2 {
		from, to := publication.Symbols[i].Identity, publication.Symbols[i+1].Identity
		document.Content.Relationships = append(document.Content.Relationships, storage.DocumentRelationship{Source: indexer.SymbolID(from), Targets: []string{indexer.SymbolID(to)},
			Kind: "acme.calls", Resolution: "static", Evidence: storage.RelationshipEvidence{Range: symbols[i].Name, Bytes: symbols[i].NameBytes, Text: from.Name}})
	}
	if len(metrics) == 0 {
		document.Content.Version = 9
		symbols[3].Measures = nil
		publication.Kinds[1].Metrics = nil
	}
	publication.Documents = []indexer.PublishedDocument{document}
	return publication
}

var _ = Describe("Producer metrics", func() {
	DescribeTable("select, filter and order declared metrics on symbols and navigation rows", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		_, err := indexer.Publish(ctx, database, rulePublication("r1", ruleMetrics))
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: ruleRoot}
		result := runQuery(ctx, pipeline, "SELECT symbol.name, symbol.metrics.variables, symbol.metrics.fan_in FROM acme.rule:* WHERE symbol.metrics.variables > 10 AND symbol.metrics.fan_in >= 2 ORDER BY symbol.metrics.variables DESC", scope)
		Expect(result.Rows).To(Equal([]map[string]any{
			{"symbol.name": "Beta", "symbol.metrics.variables": 22, "symbol.metrics.fan_in": 3},
			{"symbol.name": "Gamma", "symbol.metrics.variables": 11, "symbol.metrics.fan_in": 2},
		}))
		Expect(result.Columns[1]).To(Equal(query.QueryColumn{Name: "symbol.metrics.variables", Field: "symbol.metrics.variables", Type: "integer"}))
		result = runQuery(ctx, pipeline, "SELECT symbol.name, symbol.metrics.fields FROM acme.screen:*", scope)
		Expect(result.Rows).To(Equal([]map[string]any{{"symbol.name": "Screen", "symbol.metrics.fields": 7}}))
		result = runQuery(ctx, pipeline, "SELECT symbol.name, depth, symbol.metrics.fan_in FROM traverse(acme.rule:Alpha, via => 'acme.calls', depth => 3) ORDER BY depth", scope)
		Expect(result.Rows).To(Equal([]map[string]any{{"symbol.name": "Beta", "depth": 1, "symbol.metrics.fan_in": 3}, {"symbol.name": "Gamma", "depth": 2, "symbol.metrics.fan_in": 2}}))
		for _, expression := range []string{"SELECT symbol.metrics.fanin FROM acme.rule:*", "SELECT symbol.name FROM acme.screen:* WHERE symbol.metrics.variables > 1"} {
			_, err = pipeline.RunModules(ctx, expression, scope)
			Expect(err).To(MatchError(ContainSubstring("unknown SQL field")), expression)
		}
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	It("evolves metrics additively, refuses measures that disagree with the kind and guides older snapshots to republish", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		first, err := indexer.Publish(ctx, database, rulePublication("r1", nil))
		Expect(err).NotTo(HaveOccurred())
		_, err = indexer.Publish(ctx, database, rulePublication("r2", ruleMetrics))
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		old := query.ModuleScopeOptions{SnapshotID: first.SnapshotID}
		Expect(runQuery(ctx, pipeline, "SELECT symbol.name FROM acme.rule:* ORDER BY symbol.name", old).Total).To(Equal(3))
		_, err = pipeline.RunModules(ctx, "SELECT symbol.name FROM acme.rule:* WHERE symbol.metrics.variables > 10", old)
		Expect(err).To(MatchError(And(ContainSubstring(first.SnapshotID), ContainSubstring(`metric "variables"`), ContainSubstring("republish"))))

		removed := rulePublication("r3", map[string]storage.MetricSpec{"variables": ruleMetrics["variables"]})
		redescribed := rulePublication("r3", map[string]storage.MetricSpec{"variables": {Description: "locals"}, "fan_in": ruleMetrics["fan_in"]})
		undeclared := rulePublication("r3", ruleMetrics)
		undeclared.Documents[0].Content.Symbols[0].Measures["fanin"] = 1
		missing := rulePublication("r3", ruleMetrics)
		delete(missing.Documents[0].Content.Symbols[1].Measures, "fan_in")
		for _, refused := range []struct {
			publication indexer.Publication
			message     string
		}{
			{removed, `kind acme.rule metric "fan_in" is no longer declared`},
			{redescribed, `kind acme.rule metric "variables" changes its description`},
			{undeclared, `records metric "fanin", which its kind does not declare`},
			{missing, `has no value for its kind's declared metric "fan_in"`},
		} {
			_, err = indexer.Publish(ctx, database, refused.publication)
			Expect(err).To(MatchError(And(ContainSubstring(refused.message), ContainSubstring(ruleKind))), refused.message)
		}
		Expect(runQuery(ctx, pipeline, "SELECT symbol.metrics.variables FROM acme.rule:Beta", query.ModuleScopeOptions{RootKey: ruleRoot}).Rows).To(Equal([]map[string]any{{"symbol.metrics.variables": 22}}))
	})
})
