package query_test

import (
	"os"
	"path/filepath"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL syntax analysis", func() {
	It("preserves declaration row kinds, bodyless metrics and syntax witnesses", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module example.org/syntax\n\ngo 1.26\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(checkout, "assembly.go"), []byte("package syntax\nfunc Assembly(a, b int)\ntype Reader interface { Read(a, b int) }\n"), 0o600)).To(Succeed())
		engine, err := indexer.New(database)
		Expect(err).NotTo(HaveOccurred())
		snapshot, err := engine.Analyze(ctx, indexer.AnalysisOptions{Directory: checkout, Mode: storage.AnalysisSyntax})
		Expect(err).NotTo(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		var rows []query.SQLRow
		_, err = pipeline.VisitSQL(ctx, "SELECT symbol.metrics.param_count AS params, symbol.metrics.body_lines AS lines FROM func:Assembly", query.ModuleScopeOptions{SnapshotID: snapshot.SnapshotID}, func(row query.SQLRow) error {
			rows = append(rows, row)
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].Match.Kind).To(Equal("symbol"))
		Expect(rows[0].Values).To(Equal(map[string]any{"params": 2, "lines": nil}))
		Expect(rows[0].WitnessSymbol).To(And(HaveField("ID", ""), HaveField("DeclarationKey", Not(BeEmpty()))))
		methods, err := pipeline.RunModules(ctx, "SELECT symbol.metrics.param_count AS params, symbol.metrics.body_lines AS lines FROM func:Read", query.ModuleScopeOptions{SnapshotID: snapshot.SnapshotID})
		Expect(err).NotTo(HaveOccurred())
		Expect(methods.Rows).To(Equal([]map[string]any{{"params": 2, "lines": nil}}))
		_, err = pipeline.VisitSQL(ctx, "SELECT symbol.id FROM func:Assembly", query.ModuleScopeOptions{SnapshotID: snapshot.SnapshotID}, func(query.SQLRow) error { return nil })
		Expect(err).To(MatchError(ContainSubstring("requires typed facts")))
	})
})
