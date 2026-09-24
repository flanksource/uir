package query_test

import (
	"os"
	"path/filepath"

	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("compact indexed queries", func() {
	It("selects typed symbols and keeps binary relation witnesses", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		primary, _ := shopWorkspace()
		indexCheckout(ctx, database, primary)
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: shopModule, Location: primary}

		full := runQuery(ctx, pipeline, "pkg:example.org/shop/app", scope)
		relative := runQuery(ctx, pipeline, "pkg:example.org/shop:app", scope)
		Expect(relative.Symbols).To(Equal(full.Symbols))
		Expect(runQuery(ctx, pipeline, "func:Run & pkg:example.org/shop:app", scope).Symbols).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "mod:example.org/shop", scope).Symbols).ToNot(BeEmpty())
		Expect(runQuery(ctx, pipeline, "struct:Store", scope).Symbols).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "struct:Saver", scope).Symbols).To(BeEmpty())
		Expect(runQuery(ctx, pipeline, "field:Store.count", scope).Symbols).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "field:Run", scope).Symbols).To(BeEmpty())
		Expect(runQuery(ctx, pipeline, "func:Store.Save", scope).Symbols).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "func:save", scope).Symbols).To(BeEmpty())
		Expect(runQuery(ctx, pipeline, "func:S.ve", scope).Symbols).To(BeEmpty())
		Expect(runQuery(ctx, pipeline, "func:* +pkg:example.org/shop:app", scope).Symbols).To(HaveLen(2))
		Expect(runQuery(ctx, pipeline, "func:* +pkg:example.org/shop:app +pkg:example.org/shop:store -func:Save", scope).Symbols).To(HaveLen(3))
		Expect(runQuery(ctx, pipeline, "func:* +pkg:example.org/shop:app -func:Run", scope).Symbols).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "func:Store.Save < pkg:example.org/shop:app", scope).Total).To(Equal(2))
		Expect(runQuery(ctx, pipeline, "func:Store.Save < func:Run", scope).Total).To(Equal(1))
		Expect(runQuery(ctx, pipeline, "func:Run > func:Store.Save", scope).Total).To(Equal(1))
		_, err = pipeline.RunModules(ctx, "func:Run > struct:Store", scope)
		Expect(err).To(MatchError(ContainSubstring("requires a function or method on the right")))
		Expect(runQuery(ctx, pipeline, "func:Store.Save <<3 func:Run", scope).Total).To(Equal(1))
		Expect(runQuery(ctx, pipeline, "store.Saver :impl struct:Store", scope).Total).To(Equal(1))
		Expect(runQuery(ctx, pipeline, "struct:Store :methods func:Store.Save", scope).Total).To(Equal(1))
		Expect(runQuery(ctx, pipeline, "field:Store.count ~w func:Store.Save", scope).Total).To(Equal(1))
		Expect(runQuery(ctx, pipeline, "func:Run >>3 func:Store.Save", scope).Path).ToNot(BeNil())
	})
	It("classifies a type from its selected snapshot", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		workspace := GinkgoT().TempDir()
		for location, source := range map[string]string{
			"struct":    "package sample\ntype Entity struct{}\ntype Defined Entity\ntype Alias = Entity\n",
			"interface": "package sample\ntype Entity interface{ M() }\n",
		} {
			checkout := filepath.Join(workspace, location)
			Expect(os.MkdirAll(checkout, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module example.org/sample\n\ngo 1.26\n"), 0o644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(checkout, "sample.go"), []byte(source), 0o644)).To(Succeed())
			indexCheckout(ctx, database, checkout)
		}
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		structure := runQuery(ctx, pipeline, "struct:Entity", query.ModuleScopeOptions{RootKey: "example.org/sample", Location: filepath.Join(workspace, "struct")})
		iface := runQuery(ctx, pipeline, "struct:Entity", query.ModuleScopeOptions{RootKey: "example.org/sample", Location: filepath.Join(workspace, "interface")})
		Expect(structure.Symbols).To(HaveLen(1))
		Expect(structure.Matches).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "struct:Defined", query.ModuleScopeOptions{RootKey: "example.org/sample", Location: filepath.Join(workspace, "struct")}).Symbols).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "struct:Alias", query.ModuleScopeOptions{RootKey: "example.org/sample", Location: filepath.Join(workspace, "struct")}).Symbols).To(BeEmpty())
		Expect(iface.Symbols).To(BeEmpty())
		Expect(iface.Matches).To(BeEmpty())
	})
	It("applies a binary transitive caller selector after traversing intermediate callers", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		primary, _ := shopWorkspace()
		chain := "package app\nimport \"example.org/shop/store\"\nfunc Middle() error { return (&store.Store{}).Save(\"x\") }\nfunc Outer() error { return Middle() }\n"
		Expect(os.WriteFile(filepath.Join(primary, "app", "chain.go"), []byte(chain), 0o644)).To(Succeed())
		indexCheckout(ctx, database, primary)
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		result := runQuery(ctx, pipeline, "func:Store.Save <<3 func:Outer", query.ModuleScopeOptions{RootKey: shopModule, Location: primary})
		Expect(result.Matches).To(HaveLen(1))
		Expect(result.Matches[0].Depth).To(Equal(2))
	})
	It("filters an exact package, its subpackages, or an entire module", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		primary, _ := shopWorkspace()
		Expect(os.MkdirAll(filepath.Join(primary, "app", "sub"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(primary, "app", "sub", "sub.go"), []byte("package sub\nimport \"example.org/shop/store\"\nfunc Nested(s *store.Store) error { return s.Save(\"nested\") }\n"), 0o644)).To(Succeed())
		indexCheckout(ctx, database, primary)
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: shopModule, Location: primary}

		Expect(runQuery(ctx, pipeline, "store.Store.Save < -pkg example.org/shop/app", scope).Total).To(Equal(2))
		Expect(runQuery(ctx, pipeline, "store.Store.Save < -pkg example.org/shop/app/...", scope).Total).To(Equal(1))
		Expect(runQuery(ctx, pipeline, "store.Store.Save < +pkg example.org/shop/app/...", scope).Total).To(Equal(3))
		Expect(runQuery(ctx, pipeline, "store.Store.Save < -pkg example.org/shop/...", scope).Total).To(Equal(0))
		Expect(runQuery(ctx, pipeline, "store.Store.Save < -pkg example.org/sho/...", scope).Total).To(Equal(4))
		children := runQuery(ctx, pipeline, "pkg:example.org/shop:*", scope)
		Expect(children.Symbols).ToNot(BeEmpty())
		for _, symbol := range children.Symbols {
			Expect(symbol.PackagePath).To(BeElementOf("example.org/shop/app", "example.org/shop/store"))
		}
		Expect(runQuery(ctx, pipeline, "pkg:example.org/shop:app/**", scope).Symbols).To(HaveLen(3))
		Expect(runQuery(ctx, pipeline, "pkg:example.org/shop:.", scope).Symbols).To(BeEmpty())
		Expect(runQuery(ctx, pipeline, "pkg:example.org/shop:ap?", scope).Symbols).To(HaveLen(2))
	})

	It("matches relative packages independently in several module roots", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		workspace := GinkgoT().TempDir()
		for _, module := range []string{"example.org/one", "example.org/two"} {
			checkout := filepath.Join(workspace, filepath.Base(module))
			Expect(os.MkdirAll(filepath.Join(checkout, "internal", "deep"), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+module+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(checkout, "root.go"), []byte("package root\nfunc Root() {}\n"), 0o644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(checkout, "internal", "deep", "deep.go"), []byte("package deep\nfunc Deep() {}\n"), 0o644)).To(Succeed())
			indexCheckout(ctx, database, checkout)
		}
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())

		roots := runQuery(ctx, pipeline, "pkg:example.org/*:.", query.ModuleScopeOptions{})
		deep := runQuery(ctx, pipeline, "pkg:example.org/*:internal/**", query.ModuleScopeOptions{})
		all := runQuery(ctx, pipeline, "pkg:example.org/*:**", query.ModuleScopeOptions{})
		Expect(roots.Symbols).To(HaveLen(2))
		Expect(deep.Symbols).To(HaveLen(2))
		Expect(all.Symbols).To(HaveLen(4))
		for _, symbol := range deep.Symbols {
			Expect(symbol.PackagePath).To(BeElementOf("example.org/one/internal/deep", "example.org/two/internal/deep"))
		}
	})

	It("resolves Go names, roles, filters, composition, and a shortest call path", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		primary, branch := shopWorkspace()
		indexCheckout(ctx, database, primary)
		indexCheckout(ctx, database, branch)
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		scope := query.ModuleScopeOptions{RootKey: shopModule, Location: primary}
		defaultResult := runQuery(ctx, pipeline, "store.Store.Save <", query.ModuleScopeOptions{RootKey: shopModule})
		Expect(defaultResult.Matches).ToNot(BeEmpty())
		for _, row := range defaultResult.Matches {
			Expect(row.Location).To(Equal(defaultResult.Matches[0].Location))
		}

		incoming := runQuery(ctx, pipeline, "store.Store.Save <", scope)
		Expect(incoming.Operation).To(Equal(query.OperationIncoming))
		Expect(incoming.Total).To(Equal(3))
		Expect(incoming.Matches[0].PackagePath).To(Equal("example.org/shop/app"))
		Expect(runQuery(ctx, pipeline, "store.Store.Save < +pkg app", scope).Total).To(Equal(2))
		external := runQuery(ctx, pipeline, "store.Store.Save < -pkg store", scope)
		Expect(external.Total).To(Equal(2))
		for _, row := range external.Matches {
			Expect(row.PackagePath).To(Equal("example.org/shop/app"))
		}
		Expect(runQuery(ctx, pipeline, "store.Store.Save < -pkg example.org/shop/store", scope).Matches).To(Equal(external.Matches))
		Expect(runQuery(ctx, pipeline, "store.Store.Save < -f app.go", scope).Total).To(Equal(1))
		Expect(runQuery(ctx, pipeline, "example.org/shop/store.Store.Save =", scope).Matches).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "store.Saver :impl", scope).Matches).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "store.Store :methods", scope).Matches).To(HaveLen(1))
		Expect(runQuery(ctx, pipeline, "app.Run >", scope).Matches).To(HaveLen(2))
		Expect(runQuery(ctx, pipeline, "store.Store.count ~w", scope).Matches).To(HaveLen(1))

		union := runQuery(ctx, pipeline, "app.Run | app.Direct", scope)
		Expect(union.Total).To(Equal(2))
		intersection := runQuery(ctx, pipeline, "store.Store.Save < & app.Run", scope)
		Expect(intersection.Total).To(Equal(1))
		Expect(intersection.Symbols[0].QueryName).To(Equal("example.org/shop/app.Run"))
		transitive := runQuery(ctx, pipeline, "store.Store.Save <<3", scope)
		Expect(transitive.Total).To(Equal(3))
		Expect(transitive.Matches[0].Depth).To(Equal(1))

		path := runQuery(ctx, pipeline, "app.* >> store.Store.Save", scope)
		Expect(path.Path).ToNot(BeNil())
		Expect(path.Path.Symbols).To(HaveLen(2))
		Expect(path.Path.Calls).To(HaveLen(1))
		Expect(path.Path.Symbols[1].QueryName).To(Equal("example.org/shop/store.Store.Save"))
	})
})
