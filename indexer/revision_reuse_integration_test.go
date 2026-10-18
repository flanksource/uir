package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	rpchttp "github.com/flanksource/clicky/rpc/http"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const revisionShop = "example.org/shop"

// revisionHistory is a registered shop checkout that requires the registered, tagged
// example.org/pricing v1.0.0 and imports it, with three commits: first, a body change of
// tax/rate.go in second, and a new root-package file in third, where the checkout's head lies.
type revisionHistory struct {
	shop, pricing        string
	first, second, third string
}

func writeRevisionHistory(ctx context.Context, engine *Indexer) revisionHistory {
	GinkgoHelper()
	base := canonicalTempDir()
	proxy := filepath.Join(base, "proxy")
	useModuleProxy(proxy)
	history := revisionHistory{shop: filepath.Join(base, "shop"), pricing: filepath.Join(base, "pricing")}
	writeTaggedModule(ctx, proxy, history.pricing, "example.org/pricing", "")
	indexOnce(ctx, engine, history.pricing)
	writeModule(history.shop, revisionShop, "\nrequire example.org/pricing v1.0.0\n",
		"package shop\n\nimport (\n\t\"example.org/pricing\"\n\t\"example.org/shop/tax\"\n)\n\nfunc Total() int { return source.Value() + tax.Rate() + tax.Base }\n")
	Expect(os.MkdirAll(filepath.Join(history.shop, "tax"), 0o755)).To(Succeed())
	writeFile(filepath.Join(history.shop, "tax", "tax.go"), "package tax\n\nconst Base = 1\n")
	writeFile(filepath.Join(history.shop, "tax", "rate.go"), "package tax\n\nfunc Rate() int { return 20 }\n")
	downloadModules(ctx, history.shop)
	gitOutput(ctx, history.shop, "init")
	history.first = commitPaths(ctx, history.shop, "test: first", ".")
	writeFile(filepath.Join(history.shop, "tax", "rate.go"), "package tax\n\nfunc Rate() int { return 21 }\n")
	history.second = commitPaths(ctx, history.shop, "test: change a body", "tax/rate.go")
	writeFile(filepath.Join(history.shop, "note.go"), "package shop\n\nfunc Note() string { return \"note\" }\n")
	history.third = commitPaths(ctx, history.shop, "test: add a file", "note.go")
	indexOnce(ctx, engine, history.shop)
	return history
}

func (history revisionHistory) options(commit string) RevisionOptions {
	return RevisionOptions{RootKey: revisionShop, Checkout: history.shop, Commit: commit, Reason: storage.ReasonHistorical}
}

var _ = Describe("historical revision reuse", func() {
	It("reuses an exact repeat of a commit with a versioned dependency without loading packages, until a recorded sibling or the dependency's snapshot changes",
		func(ctx SpecContext) {
			database := openIndexerDB(ctx, indexerSQLiteOptions())
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			history := writeRevisionHistory(ctx, engine)
			loads := countLoads(engine)
			first, err := engine.IndexRevision(ctx, history.options(history.first))
			Expect(err).ToNot(HaveOccurred())
			Expect(*loads).To(Equal(1))

			again, err := engine.IndexRevision(ctx, history.options(history.first))
			Expect(err).ToNot(HaveOccurred())
			Expect(again).To(And(HaveField("Unchanged", true), HaveField("SnapshotID", first.SnapshotID)))
			Expect(*loads).To(Equal(1), "the stored commit, configuration and dependency targets prove the context without type-checking")

			shape := strings.Repeat("a", 64)
			Expect(database.Create(&storage.SnapshotSiblingShape{SnapshotID: uuid.MustParse(first.SnapshotID), ImportPath: "example.org/sibling", ExportShapeHash: &shape}).Error).To(Succeed())
			sibling, err := engine.IndexRevision(ctx, history.options(history.first))
			Expect(err).ToNot(HaveOccurred())
			Expect(sibling).To(And(HaveField("Unchanged", true), HaveField("SnapshotID", first.SnapshotID)))
			Expect(*loads).To(Equal(2), "a sibling the commit does not pin is proven only by extracting")

			writeFile(filepath.Join(history.pricing, "source.go"), "package source\n\nfunc Value() int { return 2 }\n")
			commitPaths(ctx, history.pricing, "test: move tag", "source.go")
			gitOutput(ctx, history.pricing, "tag", "-f", "v1.0.0")
			moved, err := engine.IndexRevision(ctx, history.options(history.first))
			Expect(err).ToNot(HaveOccurred())
			Expect(moved).To(And(HaveField("Unchanged", false), Not(HaveField("SnapshotID", first.SnapshotID))))
			Expect(*loads).To(Equal(4), "pricing at its moved tag, then the shop, are type-checked")
		})

	It("bases a historical commit on its nearest indexed ancestor and renders only the packages whose inputs changed", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		history := writeRevisionHistory(ctx, engine)
		first, err := engine.IndexRevision(ctx, history.options(history.first))
		Expect(err).ToNot(HaveOccurred())

		timed, timings := rpchttp.WithTimings(ctx)
		second, err := engine.IndexRevision(timed, history.options(history.second))
		Expect(err).ToNot(HaveOccurred())

		snapshot := loadSnapshot(database, second.SnapshotID)
		Expect(snapshot.BaseSnapshotID).To(HaveValue(Equal(uuid.MustParse(first.SnapshotID))), "the ancestor, not the head at the third commit, is the base")
		var sourceDeltas []storage.SourceDelta
		Expect(database.Where("snapshot_id = ?", second.SnapshotID).Find(&sourceDeltas).Error).To(Succeed())
		Expect(sourceDeltas).To(ConsistOf(And(HaveField("PathKey", "tax/rate.go"), HaveField("Operation", storage.SourceSet))))
		Expect(countRows(database, &storage.SymbolDelta{}, "snapshot_ordinal = ?", snapshot.Ordinal)).To(Equal(int64(1)), "only Rate's body changed")
		rendered, _ := timings.Counter("render-documents", "rendered")
		Expect(rendered).To(Equal(int64(2)), "only tax's two files are rendered; the root package's input hash equals the base's")

		prepared, err := engine.prepareRevision(ctx, history.options(history.second))
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(prepared.cleanup)
		full, err := extractModule(ctx, extractOptions{loadPackages: engine.loadPackages, root: prepared.root})
		Expect(err).ToNot(HaveOccurred())
		active, err := storage.ActiveDocuments(ctx, database, snapshot.ID, storage.ActiveDocumentOptions{Content: true})
		Expect(err).ToNot(HaveOccurred())
		Expect(active).To(HaveLen(len(full.documents)))
		for path, document := range full.documents {
			Expect(active).To(HaveKey(path))
			Expect(sameJSON(active[path].Document.Content, document.content)).To(BeTrue(), "the published document for %q equals a full extraction's", path)
		}
	})
})
