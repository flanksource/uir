package query_test

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("structured query composition", func() {
	DescribeTable("compiles flags to the same expression as text", func(text string, options query.StructuredOptions) {
		parsed, err := query.Parse(text)
		Expect(err).NotTo(HaveOccurred())
		compiled, err := query.Compose(nil, options)
		Expect(err).NotTo(HaveOccurred())
		Expect(compiled).To(Equal(parsed.Expr))
	},
		Entry("method kind", "method:*", query.StructuredOptions{Kinds: []string{"method"}}),
		Entry("mixed kinds", "method:* | var:*", query.StructuredOptions{Kinds: []string{"method", "var"}}),
		Entry("outgoing calls", "method:* >", query.StructuredOptions{Kinds: []string{"method"}, Relations: []string{"calls"}}),
		Entry("outgoing calls without a kind", "func:* >", query.StructuredOptions{Relations: []string{"calls"}}),
		Entry("incoming calls without a kind", "func:* <", query.StructuredOptions{Relations: []string{"callers"}}),
		Entry("mixed default relation kinds", "func:* > | type:* :inherits", query.StructuredOptions{Relations: []string{"calls", "inherits"}}),
		Entry("direct embedders", "type:* :inherits", query.StructuredOptions{Kinds: []string{"type"}, Relations: []string{"inherits"}}),
		Entry("path filters", "method:* +path:example.org/app/** -path:example.org/app/internal/**", query.StructuredOptions{
			Kinds: []string{"method"}, Include: []string{"example.org/app/**"}, Exclude: []string{"example.org/app/internal/**"},
		}),
	)

	It("intersects a supplied expression with kinds before traversing relations", func() {
		parsed, err := query.Parse("func:Save")
		Expect(err).NotTo(HaveOccurred())
		compiled, err := query.Compose(parsed.Expr, query.StructuredOptions{Kinds: []string{"method"}, Relations: []string{"callers"}})
		Expect(err).NotTo(HaveOccurred())
		textual, err := query.Parse("(func:Save & method:*) <")
		Expect(err).NotTo(HaveOccurred())
		Expect(compiled).To(Equal(textual.Expr))
	})

	It("rejects a call path as the starting set for flags", func() {
		parsed, err := query.Parse("func:Run >> func:Save")
		Expect(err).NotTo(HaveOccurred())
		_, err = query.Compose(parsed.Expr, query.StructuredOptions{Kinds: []string{"method"}})
		Expect(err).To(MatchError(ContainSubstring("call path")))
	})

	It("selects typed symbols and tree nodes through the same evaluator", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		primary, _ := shopWorkspace()
		Expect(os.WriteFile(filepath.Join(primary, "store", "extra.go"), []byte("package store\nvar Global = 1\ntype Embedded struct{ Store }\n"), 0o644)).To(Succeed())
		indexCheckout(ctx, database, primary)
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: shopModule, Location: primary}
		for _, selection := range []struct{ kind, expected string }{
			{"method", "Save"}, {"var", "Global"}, {"type", "Store"}, {"module", shopModule}, {"package", shopModule + "/store"},
		} {
			expression, err := query.Compose(nil, query.StructuredOptions{Kinds: []string{selection.kind}})
			Expect(err).NotTo(HaveOccurred())
			result, err := pipeline.RunExpr(ctx, expression, scope)
			Expect(err).NotTo(HaveOccurred(), selection.kind)
			if selection.expected != "" {
				Expect(result.Symbols).To(ContainElement(HaveField("Name", selection.expected)))
			}
			if selection.kind == "module" {
				Expect(result.Matches).To(HaveLen(1))
			}
			if selection.kind == "package" {
				Expect(result.Matches).To(HaveLen(2))
			}
		}
		expression, err := query.Compose(nil, query.StructuredOptions{Kinds: []string{"method", "type"}, Include: []string{shopModule + "/store/**"}, Exclude: []string{shopModule + "/store.Store.Save"}})
		Expect(err).NotTo(HaveOccurred())
		filtered, err := pipeline.RunExpr(ctx, expression, scope)
		Expect(err).NotTo(HaveOccurred())
		Expect(filtered.Symbols).To(ContainElement(HaveField("Name", "Store")))
		Expect(filtered.Symbols).NotTo(ContainElement(HaveField("QueryName", shopModule+"/store.Store.Save")))
		Expect(runQuery(ctx, pipeline, "path:"+shopModule+"/store", scope).Symbols).To(ContainElement(HaveField("QueryName", shopModule+"/store.Store.Save")))
		ownerFilter, err := query.Compose(nil, query.StructuredOptions{Kinds: []string{"method"}, Exclude: []string{shopModule + "/store.Store"}})
		Expect(err).NotTo(HaveOccurred())
		ownerResult, err := pipeline.RunExpr(ctx, ownerFilter, scope)
		Expect(err).NotTo(HaveOccurred())
		Expect(ownerResult.Symbols).NotTo(ContainElement(HaveField("QueryName", shopModule+"/store.Store.Save")))
		calls, err := query.Compose(nil, query.StructuredOptions{Relations: []string{"calls"}})
		Expect(err).NotTo(HaveOccurred())
		called, err := pipeline.RunExpr(ctx, calls, scope)
		Expect(err).NotTo(HaveOccurred())
		Expect(called.Total).To(BeNumerically(">", 0))
		Expect(called.Matches).To(ContainElement(HaveField("Relation", ">")))
		combined, err := query.Compose(nil, query.StructuredOptions{Relations: []string{"calls", "inherits"}})
		Expect(err).NotTo(HaveOccurred())
		combinedResult, err := pipeline.RunExpr(ctx, combined, scope)
		Expect(err).NotTo(HaveOccurred())
		Expect(combinedResult.Matches).To(ContainElement(HaveField("Relation", ">")))
		Expect(combinedResult.Matches).To(ContainElement(HaveField("Relation", ":inherits")))
	})

	It("finds direct struct and interface embedders with source witnesses", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := GinkgoT().TempDir()
		const module = "example.org/embedding"
		Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+module+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(checkout, "types.go"), []byte("package embedding\ntype Base struct{}\ntype Child struct{ Base }\ntype GrandChild struct{ Child }\ntype Contract interface{ Run() }\ntype Extended interface{ Contract }\n"), 0o644)).To(Succeed())
		indexCheckout(ctx, database, checkout)
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		for _, target := range []struct{ name, embedder string }{{"Base", "Child"}, {"Contract", "Extended"}} {
			parsed, err := query.Parse("type:" + target.name)
			Expect(err).NotTo(HaveOccurred())
			expression, err := query.Compose(parsed.Expr, query.StructuredOptions{Relations: []string{"inherits"}})
			Expect(err).NotTo(HaveOccurred())
			result, err := pipeline.RunExpr(ctx, expression, query.ModuleScopeOptions{RootKey: module, Location: checkout})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Matches).To(HaveLen(1))
			Expect(result.Matches[0].Identifier.Type).To(Equal(target.embedder))
			Expect(result.Matches[0].Role).To(Equal("embeds"))
			Expect(result.Matches[0].SourceName).To(ContainSubstring(target.name))
		}
	})

	It("keeps format 2 snapshots readable but requires reindexing for inheritance", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := GinkgoT().TempDir()
		const module = "example.org/legacy"
		Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+module+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(checkout, "types.go"), []byte("package legacy\ntype Base struct{}\n"), 0o644)).To(Succeed())
		indexCheckout(ctx, database, checkout)
		var documents []storage.Document
		Expect(database.Find(&documents).Error).To(Succeed())
		Expect(documents).To(HaveLen(1))
		var content storage.DocumentContent
		Expect(json.Unmarshal(documents[0].Content, &content)).To(Succeed())
		content.Version = 2
		encoded, err := json.Marshal(content)
		Expect(err).NotTo(HaveOccurred())
		Expect(database.Model(&storage.Document{}).Where("id = ?", documents[0].ID).Update("content", storage.JSON(encoded)).Error).To(Succeed())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: module, Location: checkout}
		Expect(runQuery(ctx, pipeline, "type:Base", scope).Matches).To(HaveLen(1))
		_, err = pipeline.RunModules(ctx, "type:Base :inherits", scope)
		Expect(err).To(MatchError(ContainSubstring("lacks embedding facts")))
	})
})
