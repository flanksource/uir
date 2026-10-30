package query_test

import (
	"context"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	bootModule  = "example.org/boot"
	bootPackage = bootModule + "/boot"
	// bootA declares two inits: the first at line 5 with a three-line body, the second at line 9 with four.
	bootA = "package boot\n\nvar order []string\n\n" +
		"func init() {\n\torder = append(order, \"a0\")\n}\n\n" +
		"func init() {\n\torder = append(order, \"a1\")\n\torder = append(order, \"a1 again\")\n}\n"
	// bootB declares one init on line 3 with a one-line body.
	bootB = "package boot\n\nfunc init() { order = append(order, \"b\") }\n"
)

type initDeclaration struct {
	Signature string
	NameLine  int
	BodyLines int
}

func bootCheckout() string {
	GinkgoHelper()
	checkout := GinkgoT().TempDir()
	Expect(os.MkdirAll(filepath.Join(checkout, "boot"), 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+bootModule+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "boot", "a.go"), []byte(bootA), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "boot", "b.go"), []byte(bootB), 0o644)).To(Succeed())
	return checkout
}

// initDeclarations reads every init a snapshot's documents declare, with its stored metrics row, keyed by symbol id.
func initDeclarations(ctx context.Context, database *gorm.DB, snapshotID string) map[string]initDeclaration {
	GinkgoHelper()
	active, err := storage.ActiveDocuments(ctx, database, uuid.MustParse(snapshotID), storage.ActiveDocumentOptions{Content: true})
	Expect(err).ToNot(HaveOccurred())
	declarations := map[string]initDeclaration{}
	for path, document := range active {
		content, err := storage.DecodeDocument(document.Document, document.Source)
		Expect(err).ToNot(HaveOccurred(), path)
		for _, symbol := range content.Symbols {
			if symbol.Identifier.Method != "init" {
				continue
			}
			Expect(symbol.ID).ToNot(BeNil(), path)
			var metrics storage.DeclarationMetrics
			Expect(database.Where("document_ordinal = ? AND symbol_key = ?", document.Document.Ordinal, symbol.Key).First(&metrics).Error).To(Succeed(), symbol.Key)
			Expect(metrics.BodyLines).ToNot(BeNil(), symbol.Key)
			declarations[*symbol.ID] = initDeclaration{Signature: symbol.Identifier.Signature, NameLine: metrics.NameLine, BodyLines: *metrics.BodyLines}
		}
	}
	return declarations
}

func initCanonicalKeys(database *gorm.DB) []string {
	GinkgoHelper()
	var keys []string
	Expect(database.Model(&storage.Symbol{}).Where("package_path = ? AND kind = ? AND name = ?", bootPackage, "func", "init").Pluck("canonical_key", &keys).Error).To(Succeed())
	return keys
}

func initIdentity(discriminator string) indexer.Identity {
	return indexer.Identity{ModuleKey: bootModule, PackagePath: bootPackage, Kind: "func", Name: "init", Discriminator: discriminator}
}

var _ = Describe("Go init overloads", func() {
	DescribeTable("give every init of a package its own identity, metrics row, and query row", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		checkout := bootCheckout()
		first := indexCheckout(ctx, database, checkout)

		Expect(initDeclarations(ctx, database, first.SnapshotID)).To(Equal(map[string]initDeclaration{
			indexer.SymbolID(initIdentity("a.go#0")): {Signature: "a.go#0", NameLine: 5, BodyLines: 3},
			indexer.SymbolID(initIdentity("a.go#1")): {Signature: "a.go#1", NameLine: 9, BodyLines: 4},
			indexer.SymbolID(initIdentity("b.go#0")): {Signature: "b.go#0", NameLine: 3, BodyLines: 1},
		}))
		Expect(initCanonicalKeys(database)).To(ConsistOf(
			initIdentity("a.go#0").CanonicalKey(), initIdentity("a.go#1").CanonicalKey(), initIdentity("b.go#0").CanonicalKey()))

		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		const expression = "SELECT symbol.metrics.body_lines FROM func:init"
		Expect(runQuery(ctx, pipeline, expression, query.ModuleScopeOptions{SnapshotID: first.SnapshotID}).Rows).To(ConsistOf(
			map[string]any{"symbol.metrics.body_lines": 3}, map[string]any{"symbol.metrics.body_lines": 4}, map[string]any{"symbol.metrics.body_lines": 1}))

		Expect(os.WriteFile(filepath.Join(checkout, "boot", "b.go"), []byte(bootB+"\nfunc B() {}\n"), 0o644)).To(Succeed())
		republished := indexCheckout(ctx, database, checkout)
		Expect(republished.SnapshotID).ToNot(Equal(first.SnapshotID))
		Expect(initDeclarations(ctx, database, republished.SnapshotID)).To(HaveLen(3))
		Expect(runQuery(ctx, pipeline, expression, query.ModuleScopeOptions{SnapshotID: republished.SnapshotID}).Rows).To(HaveLen(3))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
})
