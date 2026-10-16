package indexer

import (
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	locateRoot   = "example.org/locate"
	locateNested = "example.org/locate/nested"
)

func writeGoMod(directory, module string) {
	GinkgoHelper()
	Expect(os.MkdirAll(directory, 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module "+module+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
}

func registerCheckout(database *gorm.DB, rootKey, directory string, ordinal int32) {
	GinkgoHelper()
	now := time.Now().UTC()
	root := storage.ModuleRoot{ID: uuid.New(), RootKey: rootKey, Name: filepath.Base(rootKey), CreatedAt: now, Ordinal: ordinal}
	location := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: directory, Kind: "module", CreatedAt: now}
	Expect(database.Create(&root).Error).To(Succeed())
	Expect(database.Create(&location).Error).To(Succeed())
}

var _ = Describe("LocateCheckout", func() {
	type locateCase struct {
		directory func(workspace string) string
		expected  func(workspace string) (Checkout, bool)
	}
	registered := func(workspace string) (Checkout, bool) {
		return Checkout{RootKey: locateRoot, Location: workspace}, true
	}

	DescribeTable("finds the registered checkout enclosing a directory",
		func(ctx SpecContext, scenario locateCase) {
			database := openIndexerSQLite(ctx)
			workspace := canonicalTempDir()
			writeGoMod(workspace, locateRoot)
			writeGoMod(filepath.Join(workspace, "nested"), locateNested)
			writeGoMod(filepath.Join(workspace, "unregistered"), "example.org/unregistered")
			Expect(os.MkdirAll(filepath.Join(workspace, "pkg", "deep"), 0o755)).To(Succeed())
			registerCheckout(database, locateRoot, workspace, 1)
			registerCheckout(database, locateNested, filepath.Join(workspace, "nested"), 2)

			checkout, found, err := LocateCheckout(ctx, database, scenario.directory(workspace))
			Expect(err).ToNot(HaveOccurred())
			expected, expectedFound := scenario.expected(workspace)
			Expect([]any{checkout, found}).To(Equal([]any{expected, expectedFound}))
		},
		Entry("at the module directory", locateCase{directory: func(workspace string) string { return workspace }, expected: registered}),
		Entry("from a subdirectory", locateCase{directory: func(workspace string) string { return filepath.Join(workspace, "pkg", "deep") }, expected: registered}),
		Entry("in a nested module", locateCase{
			directory: func(workspace string) string { return filepath.Join(workspace, "nested") },
			expected: func(workspace string) (Checkout, bool) {
				return Checkout{RootKey: locateNested, Location: filepath.Join(workspace, "nested")}, true
			},
		}),
		Entry("not in an unregistered module", locateCase{
			directory: func(workspace string) string { return filepath.Join(workspace, "unregistered") },
			expected:  func(string) (Checkout, bool) { return Checkout{}, false },
		}),
	)

	It("resolves a symlinked directory to the canonical checkout", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		workspace := canonicalTempDir()
		writeGoMod(workspace, locateRoot)
		registerCheckout(database, locateRoot, workspace, 1)
		link := filepath.Join(canonicalTempDir(), "link")
		Expect(os.Symlink(workspace, link)).To(Succeed())

		checkout, found, err := LocateCheckout(ctx, database, link)
		Expect(err).ToNot(HaveOccurred())
		Expect([]any{checkout, found}).To(Equal([]any{Checkout{RootKey: locateRoot, Location: workspace}, true}))
	})

	It("finds nothing outside any Go module", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		checkout, found, err := LocateCheckout(ctx, database, canonicalTempDir())
		Expect(err).ToNot(HaveOccurred())
		Expect([]any{checkout, found}).To(Equal([]any{Checkout{}, false}))
	})

	It("fails on a go.mod without a module path", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		workspace := canonicalTempDir()
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("go 1.26\n"), 0o644)).To(Succeed())
		_, _, err := LocateCheckout(ctx, database, workspace)
		Expect(err).To(MatchError(ContainSubstring("has no module path")))
	})
})
