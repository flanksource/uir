package indexer

import (
	"context"
	"os"
	"path/filepath"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	chainModule = "example.org/chain"

	deepSource = "package deep\n\nfunc One() int { return 1 }\n"
	midSource  = "package mid\n\nimport \"example.org/chain/deep\"\n\nfunc Two() int { return deep.One() + 1 }\n"
	topSource  = "package top\n\nimport (\n\t\"io\"\n\n\t\"example.org/chain/mid\"\n)\n\nfunc Three() int { return mid.Two() + 1 }\n\n" +
		"type Reader struct{}\n\nfunc (Reader) Read([]byte) (int, error) { return 0, io.EOF }\n"
	// readerFact relates top.Reader to io.Reader, which no file of the module names.
	readerFact = chainModule + "/top.Reader io.Reader"
	baseSource = "package base\n\ntype T struct{}\n\nfunc (T) A() {}\n"
	wrapSource = "package wrap\n\nimport \"example.org/chain/base\"\n\ntype W struct{ base.T }\n"
	userSource = "package user\n\nimport \"example.org/chain/wrap\"\n\ntype ALike interface{ A() }\n\ntype BLike interface{ B() }\n\nfunc Call(w wrap.W) { w.A() }\n"
)

// writeChainModule writes example.org/chain: top imports mid, which imports deep without naming it in
// its API; wrap embeds base.T in its exported W; user, which neither base nor wrap imports, declares the
// interfaces their types implement.
func writeChainModule(workspace string) {
	GinkgoHelper()
	writeFile(filepath.Join(workspace, "go.mod"), "module "+chainModule+"\n\ngo 1.26\n")
	for name, source := range map[string]string{"deep": deepSource, "mid": midSource, "top": topSource, "base": baseSource, "wrap": wrapSource, "user": userSource} {
		Expect(os.MkdirAll(filepath.Join(workspace, name), 0o755)).To(Succeed())
		writeFile(filepath.Join(workspace, name, name+".go"), source)
	}
}

func implementsPostgresOptions() storage.DBOptions {
	return storage.DBOptions{DSN: dbtest.ForGinkgo(dbtest.Options{Name: "uir_indexer_implements"}).DSN(), Schema: "uir_indexer_implements"}
}

// effectiveImplements is a snapshot's implements facts as "package.Type package.Interface", resolved
// from handles through the symbols table, so two databases compare by identity.
func effectiveImplements(ctx context.Context, database *gorm.DB, snapshotID string) []string {
	GinkgoHelper()
	pairs, err := storage.EffectiveImplements(ctx, database, uuid.MustParse(snapshotID), storage.ImplementsFilter{})
	Expect(err).ToNot(HaveOccurred())
	names := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		var rows []storage.Symbol
		Expect(database.Where("handle IN ?", []int64{pair.Type, pair.Interface}).Find(&rows).Error).To(Succeed())
		Expect(rows).To(HaveLen(2))
		byHandle := map[int64]storage.Symbol{rows[0].Handle: rows[0], rows[1].Handle: rows[1]}
		named, iface := byHandle[pair.Type], byHandle[pair.Interface]
		names = append(names, named.PackagePath+"."+named.Name+" "+iface.PackagePath+"."+iface.Name)
	}
	return names
}

var _ = Describe("snapshot implements facts and the non-transitive export shape", func() {
	DescribeTable("re-keys only API-referencing importers and keeps implements facts equal to a full index",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openIndexerDB(ctx, options())
			workspace := GinkgoT().TempDir()
			writeChainModule(workspace)
			engine, err := New(database)
			Expect(err).ToNot(HaveOccurred())
			first := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)
			Expect(effectiveImplements(ctx, database, first.id)).To(ConsistOf(
				chainModule+"/base.T "+chainModule+"/user.ALike", chainModule+"/wrap.W "+chainModule+"/user.ALike", readerFact,
			), "types implement an interface declared in a package they do not import, or that no file names")
			Expect(first.entry("base/base.go", findSymbol(database, chainModule+"/base", "type", "T").ID).Implements).To(BeNil())

			writeFile(filepath.Join(workspace, "deep", "deep.go"), deepSource+"\nfunc Extra() int { return 2 }\n")
			extended := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)
			row := func(snapshot typedSnapshot, name string) storage.PackageCoverage {
				return snapshot.packageRow(database, chainModule+"/"+name)
			}
			Expect([]bool{
				*row(extended, "deep").ExportShapeHash == *row(first, "deep").ExportShapeHash,
				row(extended, "mid").InputHash == row(first, "mid").InputHash,
				*row(extended, "mid").ExportShapeHash == *row(first, "mid").ExportShapeHash,
				row(extended, "top").InputHash == row(first, "top").InputHash,
			}).To(Equal([]bool{false, false, true, true}), "deep shape, mid input, mid shape, top input unchanged")
			Expect(extended.documents["top/top.go"].ID).To(Equal(first.documents["top/top.go"].ID))

			writeFile(filepath.Join(workspace, "base", "base.go"), baseSource+"\nfunc (T) B() {}\n")
			promoted := loadTypedSnapshot(ctx, database, indexOnce(ctx, engine, workspace).SnapshotID)
			Expect([]bool{
				*row(promoted, "wrap").ExportShapeHash == *row(extended, "wrap").ExportShapeHash,
				row(promoted, "user").InputHash == row(extended, "user").InputHash,
			}).To(Equal([]bool{false, false}), "the method base.T promotes into wrap.W changes wrap's shape and user's input")
			Expect(effectiveImplements(ctx, database, promoted.id)).To(ConsistOf(
				chainModule+"/base.T "+chainModule+"/user.ALike", chainModule+"/wrap.W "+chainModule+"/user.ALike",
				chainModule+"/base.T "+chainModule+"/user.BLike", chainModule+"/wrap.W "+chainModule+"/user.BLike", readerFact,
			))

			writeFile(filepath.Join(workspace, "user", "user.go"), "package user\n\nimport \"example.org/chain/wrap\"\n\ntype BLike interface{ B() }\n\nfunc Call(w wrap.W) { w.A() }\n")
			dropped := indexOnce(ctx, engine, workspace)
			Expect(loadSnapshot(database, dropped.SnapshotID).BaseSnapshotID).To(HaveValue(Equal(uuid.MustParse(promoted.id))))
			Expect(countRows(database, &storage.ImplementsDelta{}, "snapshot_ordinal = ? AND operation = ?", loadSnapshot(database, dropped.SnapshotID).Ordinal, storage.SourceDelete)).
				To(Equal(int64(2)), "the two ALike facts are deleted against the base")
			incremental := effectiveImplements(ctx, database, dropped.SnapshotID)

			fresh := openIndexerSQLite(ctx)
			freshEngine, err := New(fresh)
			Expect(err).ToNot(HaveOccurred())
			Expect(effectiveImplements(ctx, fresh, indexOnce(ctx, freshEngine, workspace).SnapshotID)).To(ConsistOf(incremental))
			Expect(incremental).To(ConsistOf(chainModule+"/base.T "+chainModule+"/user.BLike", chainModule+"/wrap.W "+chainModule+"/user.BLike", readerFact))

			_, err = storage.PruneHistory(ctx, database, chainModule, storage.HistoryPruneOptions{Keep: 1})
			Expect(err).ToNot(HaveOccurred())
			Expect(effectiveImplements(ctx, database, dropped.SnapshotID)).To(ConsistOf(incremental), "the head, rebased, keeps its facts")
			Expect(countRows(database, &storage.ImplementsDelta{}, "1 = 1")).To(Equal(int64(len(incremental))), "and sets every one itself")
		},
		Entry("SQLite", indexerSQLiteOptions),
		Entry("PostgreSQL", implementsPostgresOptions),
	)
})
