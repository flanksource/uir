package query_test

import (
	"os"
	"path/filepath"

	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL reference selectors", func() {
	It("keeps referenced identities and use sites in their checkout and immutable snapshot", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		primary, branch := shopWorkspace()
		first := indexCheckout(ctx, database, primary)
		indexCheckout(ctx, database, branch)
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		statement := "SELECT symbol.name AS name, path, role FROM references(method:Store.Save)"
		current := query.ModuleScopeOptions{RootKey: shopModule, Location: primary}
		Expect(runQuery(ctx, pipeline, statement, current).Rows).To(ConsistOf(
			map[string]any{"name": "Save", "path": "app/app.go", "role": "call"},
			map[string]any{"name": "Save", "path": "app/app.go", "role": "call"},
		))
		Expect(runQuery(ctx, pipeline, statement, query.ModuleScopeOptions{RootKey: shopModule, Location: branch}).Total).To(Equal(3))
		Expect(os.WriteFile(filepath.Join(primary, "app/app.go"), []byte("package app\n"), 0o600)).To(Succeed())
		indexCheckout(ctx, database, primary)
		Expect(runQuery(ctx, pipeline, statement, current).Rows).To(BeEmpty())
		Expect(runQuery(ctx, pipeline, statement, query.ModuleScopeOptions{SnapshotID: first.SnapshotID}).Total).To(Equal(2))
		Expect(runQuery(ctx, pipeline, "SELECT symbol.name FROM references(type:Save)", current).Rows).To(BeEmpty())
	})
})
