package query_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	catalogModule = "example.org/catalog"
	catalogFuncs  = 600
	queryLimit    = 3
)

func catalogCheckout() string {
	GinkgoHelper()
	checkout := GinkgoT().TempDir()
	var source strings.Builder
	source.WriteString("package catalog\n\ntype ItemZ struct{}\n")
	for index := range catalogFuncs {
		fmt.Fprintf(&source, "\nfunc Item%03d() {}\n", index)
	}
	Expect(os.MkdirAll(filepath.Join(checkout, "catalog"), 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+catalogModule+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "catalog", "catalog.go"), []byte(source.String()), 0o644)).To(Succeed())
	return checkout
}

var _ = Describe("compact wildcard resolution", func() {
	DescribeTable("expands direct package symbols and applies the result limit after counting",
		func(ctx SpecContext, backend string) {
			database := openQueryDatabase(ctx, backend)
			indexCheckout(ctx, database, catalogCheckout())
			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())
			scope := query.ModuleScopeOptions{RootKey: catalogModule}
			all := runQuery(ctx, pipeline, "catalog.*", scope)
			Expect(all.Total).To(Equal(catalogFuncs + 1))
			Expect(all.Matches[0].Identifier.Type).To(Equal("ItemZ"))
			limited := runQuery(ctx, pipeline, "catalog.*", query.ModuleScopeOptions{RootKey: catalogModule, Limit: queryLimit})
			Expect(limited.Total).To(Equal(all.Total))
			Expect(limited.Matches).To(Equal(all.Matches[:queryLimit]))
			Expect(runQuery(ctx, pipeline, "example.org/catalog/catalog.Item000 =", scope).Matches).To(HaveLen(1))
		}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
})
