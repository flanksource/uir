package query_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("exact indexed references", func() {
	It("returns definitions, calls, and function values without the interactive result limit", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		root := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.org/refs\n\ngo 1.26\n"), 0o644)).To(Succeed())
		var source strings.Builder
		source.WriteString("package refs\n\nfunc Run() {}\nvar Value = Run\n")
		for index := range 1001 {
			fmt.Fprintf(&source, "var Call%d = Run()\n", index)
		}
		Expect(os.WriteFile(filepath.Join(root, "refs.go"), []byte(source.String()), 0o644)).To(Succeed())
		engine, err := indexer.New(database)
		Expect(err).ToNot(HaveOccurred())
		indexed, err := engine.IndexModules(ctx, indexer.ModuleOptions{Path: root, IncludeTests: true, Reason: storage.ReasonAdd})
		Expect(err).ToNot(HaveOccurred())
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		refs, err := pipeline.FindReferences(ctx, query.ReferenceRequest{
			SnapshotIDs: []string{indexed[0].SnapshotID},
			Symbols:     []query.SymbolSelector{{PackagePath: "example.org/refs", Kind: "func", Name: "Run"}},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(refs).To(HaveLen(1003))
		Expect(refs[0].Role).To(Equal("definition"))
		Expect(refs[0].SourceHash).To(HaveLen(64))
		Expect(refs[0].EndByte).To(BeNumerically(">", refs[0].StartByte))
	})
})
