package query_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const sourcesModule = "example.org/sources"

// sourcesFiles are the module's Go sources, each with bytes no other has.
var sourcesFiles = map[string]string{
	"a.go": "package sources\n\nfunc A() {}\n",
	"b.go": "package sources\n\nfunc B() { A() }\n",
	"c.go": "package sources\n\nfunc C() { B() }\n",
}

var sourcesPaths = []string{"a.go", "b.go", "c.go"}

// sourcesCheckout writes the module, and with git commits it so the snapshot is a clean commit.
func sourcesCheckout(git bool) string {
	GinkgoHelper()
	checkout := GinkgoT().TempDir()
	Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+sourcesModule+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
	for path, content := range sourcesFiles {
		Expect(os.WriteFile(filepath.Join(checkout, path), []byte(content), 0o644)).To(Succeed())
	}
	if git {
		runGitIn(checkout, "init", "--quiet")
		runGitIn(checkout, "add", ".")
		runGitIn(checkout, "-c", "user.name=test", "-c", "user.email=test@example.org", "commit", "--quiet", "-m", "sources")
	}
	return checkout
}

func runGitIn(directory string, args ...string) {
	GinkgoHelper()
	output, err := exec.Command("git", append([]string{"-C", directory}, args...)...).CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), "git %s: %s", strings.Join(args, " "), output)
}

// statementCounter counts the SQL statements a pipeline runs.
type statementCounter struct{ statements atomic.Int64 }

func (counter *statementCounter) LogMode(logger.LogLevel) logger.Interface { return counter }
func (counter *statementCounter) Info(context.Context, string, ...any)     {}
func (counter *statementCounter) Warn(context.Context, string, ...any)     {}
func (counter *statementCounter) Error(context.Context, string, ...any)    {}
func (counter *statementCounter) Trace(context.Context, time.Time, func() (string, int64), error) {
	counter.statements.Add(1)
}

// countedSourcesRead reads paths through a fresh pipeline and reports the SQL statements it ran.
func countedSourcesRead(ctx context.Context, database *gorm.DB, snapshotID string, paths []string) ([]query.ModuleSourceContent, int64) {
	GinkgoHelper()
	counter := &statementCounter{}
	pipeline, err := query.NewPipeline(database.Session(&gorm.Session{Logger: counter}))
	Expect(err).ToNot(HaveOccurred())
	contents, err := pipeline.ReadModuleSources(ctx, query.ModuleSourcesOptions{SnapshotID: snapshotID, Paths: paths})
	Expect(err).ToNot(HaveOccurred())
	return contents, counter.statements.Load()
}

var _ = Describe("reading many module sources", func() {
	It("reads generated external sources without one SQL query per source", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		publication := indexer.Publication{RootKey: sourcesModule, Name: "Sources", Location: indexer.ExternalLocation{URI: "example://sources"}}
		for _, name := range sourcesPaths {
			body := sourcesFiles[name]
			publication.Documents = append(publication.Documents, indexer.PublishedDocument{PathKey: name, PackagePath: sourcesModule,
				Source: body, ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(body))),
				Content: storage.DocumentContent{Version: storage.DocumentFormatVersion, PackagePath: sourcesModule,
					Symbols: []storage.DocumentSymbol{}, Occurrences: []storage.DocumentOccurrence{}, Diagnostics: []storage.DocumentDiagnostic{}}})
		}
		result, err := indexer.Publish(ctx, database, publication)
		Expect(err).NotTo(HaveOccurred())
		_, oneStatements := countedSourcesRead(ctx, database, result.SnapshotID, sourcesPaths[:1])
		all, allStatements := countedSourcesRead(ctx, database, result.SnapshotID, sourcesPaths)
		Expect(allStatements).To(Equal(oneStatements))
		for index, content := range all {
			Expect(content.Path).To(Equal(sourcesPaths[index]))
			Expect(content.Content).To(Equal(sourcesFiles[content.Path]))
			Expect(content.Origin).To(Equal("snapshot"))
		}
	})

	DescribeTable("returns each source's indexed bytes, as a single read does, from where they are recoverable",
		func(ctx SpecContext, git bool, change func(checkout string), origin string) {
			database := openQueryDatabase(ctx, "sqlite")
			checkout := sourcesCheckout(git)
			snapshotID := indexCheckout(ctx, database, checkout).SnapshotID
			change(checkout)
			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())

			contents, err := pipeline.ReadModuleSources(ctx, query.ModuleSourcesOptions{SnapshotID: snapshotID, Paths: sourcesPaths})

			Expect(err).ToNot(HaveOccurred())
			var expected, singles []query.ModuleSourceContent
			for _, path := range sourcesPaths {
				single, err := pipeline.ReadModuleSource(ctx, snapshotID, path)
				Expect(err).ToNot(HaveOccurred())
				singles = append(singles, single)
				expected = append(expected, query.ModuleSourceContent{Path: path, Content: sourcesFiles[path], Origin: origin, Revision: single.Revision, SnapshotID: snapshotID})
			}
			Expect(contents).To(Equal(expected))
			Expect(contents).To(Equal(singles))
		},
		Entry("an unchanged checkout", false, func(string) {}, "local"),
		Entry("a non-Git checkout whose files are gone, from the stored blobs", false, removeSources, "snapshot"),
		Entry("a Git checkout whose files changed, from the pinned commit", true, rewriteSources, "git"),
	)

	It("reads any number of sources with the SQL statements one source takes", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		snapshotID := indexCheckout(ctx, database, sourcesCheckout(false)).SnapshotID

		one, oneStatements := countedSourcesRead(ctx, database, snapshotID, sourcesPaths[:1])
		all, allStatements := countedSourcesRead(ctx, database, snapshotID, sourcesPaths)

		Expect([]int{len(one), len(all)}).To(Equal([]int{1, len(sourcesPaths)}))
		Expect(allStatements).To(Equal(oneStatements))
	})

	DescribeTable("fails naming the source whose indexed bytes cannot be recovered",
		func(ctx SpecContext, paths []string, tamper bool, message string) {
			database := openQueryDatabase(ctx, "sqlite")
			checkout := sourcesCheckout(true)
			snapshotID := indexCheckout(ctx, database, checkout).SnapshotID
			if tamper {
				Expect(database.Exec("UPDATE source_revisions SET content_hash = ? WHERE path_key = ?", strings.Repeat("0", 64), "b.go").Error).To(Succeed())
			}
			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())

			_, err = pipeline.ReadModuleSources(ctx, query.ModuleSourcesOptions{SnapshotID: snapshotID, Paths: paths})

			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("a path the snapshot does not hold", []string{"a.go", "missing.go"}, false, `source "missing.go" is not in snapshot`),
		Entry("a pinned Git blob without the indexed hash", sourcesPaths, true, `git source "b.go" at `),
		Entry("a path outside the module", []string{"a.go", "../escape.go"}, false, `source path "../escape.go" must be local to its module root`),
		Entry("no path at all", []string{}, false, "at least one source path"),
	)
})

func removeSources(checkout string) {
	GinkgoHelper()
	for _, path := range sourcesPaths {
		Expect(os.Remove(filepath.Join(checkout, path))).To(Succeed())
	}
}

func rewriteSources(checkout string) {
	GinkgoHelper()
	for _, path := range sourcesPaths {
		Expect(os.WriteFile(filepath.Join(checkout, path), []byte(fmt.Sprintf("package sources\n\n// rewritten %s\n", path)), 0o644)).To(Succeed())
	}
}
