package query_test

import (
	"encoding/json"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func domainPublication() indexer.Publication {
	root := "example.org/domains"
	publication := indexer.Publication{RootKey: root, Name: "Domains", Location: indexer.ExternalLocation{URI: "test://domains"}, Revision: "r1",
		Kinds: []indexer.KindDeclaration{
			{Name: "http.route", Category: symbolhandle.CategoryMember, Schema: storage.JSON(`{"type":"object","properties":{"method":{"type":"string"},"path":{"type":"string"},"optional":{"type":"string"}},"required":["method","path"],"additionalProperties":false}`)},
			{Name: "messaging.topic", Category: symbolhandle.CategoryMember},
			{Name: "messaging.consumer", Category: symbolhandle.CategoryCallable},
			{Name: "k8s.service", Category: symbolhandle.CategoryMember},
			{Name: "k8s.pod", Category: symbolhandle.CategoryMember},
		},
		Relationships: []storage.RelationshipKind{
			{Name: "http.handled_by", Type: uir.RelationshipTypeReference},
			{Name: "messaging.publishes_to", Type: uir.RelationshipTypeReference},
			{Name: "messaging.delivers_to", Type: uir.RelationshipTypeReference},
			{Name: "k8s.selects", Type: uir.RelationshipTypeReference},
			{Name: "k8s.uses_config", Type: uir.RelationshipTypeReference},
		},
	}
	names := []string{"Orders", "Handle", "OrderCreated", "Consume", "checkout", "pod"}
	kinds := []string{"http.route", "func", "messaging.topic", "messaging.consumer", "k8s.service", "k8s.pod"}
	identities := make([]indexer.Identity, len(names))
	for i, name := range names {
		identities[i] = indexer.Identity{ModuleKey: root, PackagePath: root, Kind: kinds[i], Name: name}
	}
	for i, identity := range identities {
		source := publishedText(identity.Name + "\n")
		identifier := uir.Identifier{Module: root, Package: root, Method: identity.Name, NodeType: uir.NodeTypeFunction}
		decl := source.declaration(identity, identifier, identity.Name, identity.Name)
		if i == 0 {
			decl.Props = map[string]any{"method": "POST", "path": "/Orders/new"}
			decl.Payload = storage.JSON(`{"original":true}`)
		}
		document := source.document(identity.Name+".txt", root, []storage.DocumentSymbol{decl}, nil)
		if i < 3 {
			kind := []string{"http.handled_by", "messaging.publishes_to", "messaging.delivers_to"}[i]
			document.Content.Relationships = []storage.DocumentRelationship{{Source: indexer.SymbolID(identity), Targets: []string{indexer.SymbolID(identities[i+1])}, Kind: kind, Resolution: "static", Evidence: storage.RelationshipEvidence{Range: decl.Name, Bytes: decl.NameBytes, Text: identity.Name}}}
		}
		if i == 4 {
			for _, kind := range []string{"k8s.selects", "k8s.uses_config"} {
				document.Content.Relationships = append(document.Content.Relationships, storage.DocumentRelationship{Source: indexer.SymbolID(identity), Targets: []string{indexer.SymbolID(identities[5])}, Kind: kind, Resolution: "static", Evidence: storage.RelationshipEvidence{Range: decl.Name, Bytes: decl.NameBytes, Text: identity.Name}})
			}
		}
		publication.Symbols = append(publication.Symbols, indexer.PublishedSymbol{Identity: identity, Visibility: "exported"})
		publication.Documents = append(publication.Documents, document)
	}
	return publication
}

var _ = Describe("SQL domain navigation", func() {
	It("round trips publication properties and keeps equal names distinct across packages", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		publication := domainPublication()
		identity := publication.Symbols[0].Identity
		identity.PackagePath += "/tenant-two"
		source := publishedText("Orders\n")
		decl := source.declaration(identity, uir.Identifier{Module: identity.ModuleKey, Package: identity.PackagePath, Method: identity.Name, NodeType: uir.NodeTypeFunction}, identity.Name, identity.Name)
		decl.Props = map[string]any{"method": "GET", "path": "/Orders/new"}
		publication.Symbols = append(publication.Symbols, indexer.PublishedSymbol{Identity: identity, Visibility: "exported"})
		publication.Documents = append(publication.Documents, source.document("tenant-two/Orders.txt", identity.PackagePath, []storage.DocumentSymbol{decl}, nil))
		encoded, err := json.Marshal(publication)
		Expect(err).NotTo(HaveOccurred())
		var decoded indexer.Publication
		Expect(json.Unmarshal(encoded, &decoded)).To(Succeed())
		_, err = indexer.Publish(ctx, database, decoded)
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		result := runQuery(ctx, pipeline, "SELECT symbol.id, symbol.props.method FROM http.route:Orders ORDER BY symbol.props.method", query.ModuleScopeOptions{RootKey: publication.RootKey})
		Expect(result.Total).To(Equal(2))
		Expect(result.Rows[0]["symbol.id"]).NotTo(Equal(result.Rows[1]["symbol.id"]))
		Expect(result.Rows[0]["symbol.props.method"]).To(Equal("GET"))
		Expect(result.Rows[1]["symbol.props.method"]).To(Equal("POST"))
	})

	It("walks native cycles in both directions and reports graph bounds", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		publication := domainPublication()
		last := &publication.Documents[3].Content
		last.Relationships = []storage.DocumentRelationship{{Source: indexer.SymbolID(publication.Symbols[3].Identity), Targets: []string{indexer.SymbolID(publication.Symbols[0].Identity)}, Kind: "http.handled_by", Resolution: "dynamic", Evidence: storage.RelationshipEvidence{Range: last.Symbols[0].Name, Bytes: last.Symbols[0].NameBytes, Text: "Consume"}}}
		_, err := indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: publication.RootKey}
		result := runQuery(ctx, pipeline, "SELECT symbol.name, depth FROM traverse(http.route:Orders, direction => 'incoming', via => 'http.handled_by,messaging.publishes_to,messaging.delivers_to', depth => 8) ORDER BY depth", scope)
		Expect(result.Rows).To(Equal([]map[string]any{{"symbol.name": "Consume", "depth": 1}, {"symbol.name": "OrderCreated", "depth": 2}, {"symbol.name": "Handle", "depth": 3}}))
		result = runQuery(ctx, pipeline, "SELECT * FROM graph(http.route:Orders, via => 'http.handled_by,messaging.publishes_to', direction => 'outgoing', depth => 1)", scope)
		Expect(result.Graph.Omitted.BeyondDepth).To(Equal(1))
		result = runQuery(ctx, pipeline, "SELECT * FROM graph(http.route:Orders, via => 'http.handled_by', direction => 'outgoing', nodes => 1)", scope)
		Expect(result.Graph.Omitted.NodeLimit).To(BeTrue())
		Expect(result.Graph.Nodes).To(HaveLen(1))
	})
	It("rejects invalid properties atomically before registering kinds or publishing snapshots", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		publication := domainPublication()
		publication.Documents[0].Content.Symbols[0].Props["method"] = false
		_, err := indexer.Publish(ctx, database, publication)
		Expect(err).To(HaveOccurred())
		var count int64
		Expect(database.Model(&storage.ModuleSnapshot{}).Count(&count).Error).To(Succeed())
		Expect(count).To(BeZero())
		Expect(database.Model(&storage.SymbolKind{}).Where("name = ?", "http.route").Count(&count).Error).To(Succeed())
		Expect(count).To(BeZero())
	})

	It("completes a registered prefix and reports missing native capability on older documents", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		publication := domainPublication()
		_, err := indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: publication.RootKey, Language: query.LanguageSQL}
		choices, err := pipeline.SuggestSelectors(ctx, "http.route:Ord", scope)
		Expect(err).NotTo(HaveOccurred())
		Expect(choices).To(Equal([]string{"http.route:Orders"}))
		publication.Revision = "old-format"
		publication.Documents = publication.Documents[1:2]
		publication.Documents[0].Content.Version = 3
		publication.Documents[0].Content.Relationships = nil
		_, err = indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		_, err = pipeline.RunModules(ctx, "SELECT * FROM outgoing(func:Handle)", scope)
		Expect(err).To(MatchError(ContainSubstring("reindex or reimport")))
		result := runQuery(ctx, pipeline, "SELECT symbol.name FROM func:Handle", scope)
		Expect(result.Total).To(Equal(1))
	})

	It("keeps properties and native relationships in their immutable historical snapshots", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		publication := domainPublication()
		old, err := indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		publication.Revision = "r2"
		publication.Documents[0].Content.Symbols[0].Props["method"] = "GET"
		publication.Documents[0].Content.Relationships = nil
		_, err = indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		before := runQuery(ctx, pipeline, "SELECT symbol.props.method FROM http.route:*", query.ModuleScopeOptions{RootKey: publication.RootKey, SnapshotID: old.SnapshotID})
		after := runQuery(ctx, pipeline, "SELECT symbol.props.method FROM http.route:*", query.ModuleScopeOptions{RootKey: publication.RootKey})
		Expect(before.Rows[0]["symbol.props.method"]).To(Equal("POST"))
		Expect(after.Rows[0]["symbol.props.method"]).To(Equal("GET"))
		before = runQuery(ctx, pipeline, "SELECT symbol.name FROM outgoing(http.route:*)", query.ModuleScopeOptions{RootKey: publication.RootKey, SnapshotID: old.SnapshotID})
		after = runQuery(ctx, pipeline, "SELECT symbol.name FROM outgoing(http.route:*)", query.ModuleScopeOptions{RootKey: publication.RootKey})
		Expect(before.Total).To(Equal(1))
		Expect(after.Total).To(BeZero())
	})

	It("retains ambiguous candidates and unresolved evidence without inventing links", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		publication := domainPublication()
		link := publication.Documents[0].Content.Relationships[0]
		link.Resolution = "ambiguous"
		link.Targets = append(link.Targets, indexer.SymbolID(publication.Symbols[3].Identity))
		publication.Documents[0].Content.Relationships[0] = link
		link.Resolution, link.Targets, link.Reference = "unresolved", nil, "unindexed-handler"
		publication.Documents[0].Content.Relationships = append(publication.Documents[0].Content.Relationships, link)
		_, err := indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		result := runQuery(ctx, pipeline, "SELECT symbol.name, relationship.resolution FROM outgoing(http.route:*)", query.ModuleScopeOptions{RootKey: publication.RootKey})
		Expect(result.Total).To(Equal(3))
		Expect(result.Matches[0].Relationship).NotTo(BeNil())
		result = runQuery(ctx, pipeline, "SELECT * FROM graph(http.route:Orders, via => 'http.handled_by', direction => 'outgoing', depth => 1)", query.ModuleScopeOptions{RootKey: publication.RootKey})
		Expect(result.Graph.Edges).To(HaveLen(3))
		Expect(result.Graph.Omitted.Unresolved).To(Equal(1))
	})
	It("publishes properties, optional NULLs, native postings and explicit cross-domain paths", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		publication := domainPublication()
		_, err := indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: publication.RootKey}
		result := runQuery(ctx, pipeline, "SELECT r.method, r.optional FROM http.route:* AS r WHERE r.path IMATCH '/orders/**' AND r.optional IS NULL", scope)
		Expect(result.Rows).To(Equal([]map[string]any{{"r.method": "POST", "r.optional": nil}}))
		Expect(string(result.Matches[0].Payload)).To(Equal(`{"original":true}`))
		result = runQuery(ctx, pipeline, "SELECT symbol.name FROM incoming(messaging.topic:OrderCreated, via => 'messaging.publishes_to')", scope)
		Expect(result.Rows).To(Equal([]map[string]any{{"symbol.name": "Handle"}}))
		result = runQuery(ctx, pipeline, "SELECT * FROM path(http.route:Orders, messaging.consumer:Consume, via => 'http.handled_by,messaging.publishes_to,messaging.delivers_to')", scope)
		Expect(result.Path).NotTo(BeNil())
		Expect(result.Path.Symbols).To(HaveLen(4))
		var roles []int32
		Expect(database.Model(&storage.SymbolPosting{}).Where("role IN (4,5)").Distinct("role").Order("role").Pluck("role", &roles).Error).To(Succeed())
		Expect(roles).To(Equal([]int32{4, 5}))
	})
	It("retains parallel native edge kinds in the graph", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		publication := domainPublication()
		_, err := indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		result := runQuery(ctx, pipeline, "SELECT * FROM graph(k8s.service:checkout, via => 'k8s.selects,k8s.uses_config', direction => 'outgoing')", query.ModuleScopeOptions{RootKey: publication.RootKey})
		Expect(result.Graph).NotTo(BeNil())
		Expect(result.Graph.Edges).To(HaveLen(2))
		Expect(result.Graph.Edges[0].ID).NotTo(Equal(result.Graph.Edges[1].ID))
	})
})
