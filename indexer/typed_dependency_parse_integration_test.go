package indexer

import (
	"archive/zip"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/tools/go/packages"

	"github.com/flanksource/uir/storage"
)

const (
	typecheckDepsModule  = "example.org/typecheck-deps"
	typecheckDepsVersion = "v1.0.0"

	// equivalenceRootsVariable lists checkouts, separated by the OS path list separator, whose
	// modules the real-module equivalence spec extracts.
	equivalenceRootsVariable = "UIR_EQUIVALENCE_ROOTS"

	genericSource = "// Package generic declares generic helpers whose bodies an importer never needs.\n" +
		"package generic\n\n" +
		"import (\n\t\"fmt\"\n\t\"sort\"\n\t\"strings\"\n)\n\n" +
		"// Pair is a key and its value.\n" +
		"type Pair[K comparable, V any] struct {\n\tKey   K\n\tValue V\n}\n\n" +
		"// String renders the pair; fmt is used only in this body.\n" +
		"func (pair Pair[K, V]) String() string { return fmt.Sprintf(\"%v=%v\", pair.Key, pair.Value) }\n\n" +
		"// Map applies transform to every value.\n" +
		"func Map[T, U any](values []T, transform func(T) U) []U {\n" +
		"\tmapped := make([]U, 0, len(values))\n" +
		"\tfor _, value := range values {\n\t\tmapped = append(mapped, transform(value))\n\t}\n" +
		"\treturn mapped\n}\n\n" +
		"// SortedKeys returns the keys of values in order; sort is used only in this body.\n" +
		"func SortedKeys[K ~string, V any](values map[K]V) []K {\n" +
		"\tkeys := make([]K, 0, len(values))\n" +
		"\tfor key := range values {\n\t\tkeys = append(keys, key)\n\t}\n" +
		"\tsort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })\n" +
		"\treturn keys\n}\n\n" +
		"// Upper is initialised with a closure.\n" +
		"var Upper = func(text string) string { return strings.ToUpper(text) }\n\n" +
		"// Defaults is the result of calling a closure that declares a local type.\n" +
		"var Defaults = func() map[string]Pair[string, int] {\n" +
		"\ttype counted struct{ count int }\n" +
		"\treturn map[string]Pair[string, int]{\"one\": {Key: \"one\", Value: counted{count: 1}.count}}\n" +
		"}()\n\n" +
		"// Lengths is inferred from a generic call over a closure.\n" +
		"var Lengths = Map([]string{\"a\", \"bb\"}, func(text string) int { return len(text) })\n\n" +
		"func init() { Upper = strings.TrimSpace }\n"

	shapesSource = "// Package shapes embeds generic types and declares a generic registry.\n" +
		"package shapes\n\n" +
		"import (\n\t\"strconv\"\n\n\t\"example.org/typecheck-deps/generic\"\n)\n\n" +
		"// Named is satisfied by anything with a name.\n" +
		"type Named interface {\n\tName() string\n}\n\n" +
		"// Entry embeds an instantiated generic pair.\n" +
		"type Entry struct {\n" +
		"\tgeneric.Pair[string, int]\n" +
		"\tTags   []string `json:\"tags\"`\n" +
		"\tNested struct {\n\t\tDepth int\n\t}\n}\n\n" +
		"// Name names the entry; strconv is used only in this body.\n" +
		"func (entry Entry) Name() string { return entry.Key + strconv.Itoa(entry.Value) }\n\n" +
		"// Registry holds named items.\n" +
		"type Registry[T Named] struct {\n\titems map[string]T\n}\n\n" +
		"// NewRegistry returns an empty registry.\n" +
		"func NewRegistry[T Named]() *Registry[T] { return &Registry[T]{items: map[string]T{}} }\n\n" +
		"// Add stores item under its name.\n" +
		"func (registry *Registry[T]) Add(item T) { registry.items[item.Name()] = item }\n\n" +
		"// Get returns the item named name.\n" +
		"func (registry *Registry[T]) Get(name string) (T, bool) {\n" +
		"\titem, found := registry.items[name]\n" +
		"\treturn item, found\n}\n"

	siblingSource = "// Package sibling is a local replacement the consumer imports.\n" +
		"package sibling\n\n" +
		"import \"fmt\"\n\n" +
		"// Widget is named by its identifier.\n" +
		"type Widget struct {\n\tID int\n}\n\n" +
		"// Name formats the identifier; fmt is used only in this body.\n" +
		"func (widget Widget) Name() string { return fmt.Sprint(widget.ID) }\n"

	brokenSource = "// Package broken has a type error inside a function body only.\n" +
		"package broken\n\n" +
		"// Count reports a count.\n" +
		"func Count() int { return \"many\" }\n"

	consumerSource = "// Package app uses every kind of dependency declaration.\n" +
		"package app\n\n" +
		"import (\n\t\"strings\"\n\n" +
		"\t\"example.org/sibling\"\n\t\"example.org/sibling/broken\"\n" +
		"\t\"example.org/typecheck-deps/generic\"\n\t\"example.org/typecheck-deps/shapes\"\n)\n\n" +
		"// Item is named through its embedded entry.\n" +
		"type Item struct {\n\tshapes.Entry\n\tExtra generic.Pair[int, string]\n}\n\n" +
		"// Build calls generic functions, reads closure-initialised variables, and uses promoted members.\n" +
		"func Build(values []string) []string {\n" +
		"\tupper := generic.Map(values, generic.Upper)\n" +
		"\tregistry := shapes.NewRegistry[sibling.Widget]()\n" +
		"\tregistry.Add(sibling.Widget{ID: broken.Count()})\n" +
		"\twidget, _ := registry.Get(\"1\")\n" +
		"\tentry := shapes.Entry{Tags: upper}\n" +
		"\tentry.Key = widget.Name()\n" +
		"\tentry.Nested.Depth = len(generic.Lengths)\n" +
		"\tkeys := generic.SortedKeys(generic.Defaults)\n" +
		"\treturn append(keys, strings.Join(entry.Tags, \",\"), entry.String(), Item{Entry: entry}.Name())\n" +
		"}\n"

	consumerTestSource = "package app_test\n\n" +
		"import (\n\t\"testing\"\n\n\t\"example.org/consumer/app\"\n)\n\n" +
		"func TestBuild(t *testing.T) {\n" +
		"\tif len(app.Build([]string{\"a\"})) == 0 {\n\t\tt.Fatal(\"Build returned nothing\")\n\t}\n}\n"
)

// publishModule writes module@version with files to the file proxy at directory.
func publishModule(directory, module, version string, files map[string]string) {
	GinkgoHelper()
	versionDirectory := filepath.Join(directory, filepath.FromSlash(module), "@v")
	Expect(os.MkdirAll(versionDirectory, 0o755)).To(Succeed())
	writeFile(filepath.Join(versionDirectory, version+".mod"), files["go.mod"])
	writeFile(filepath.Join(versionDirectory, version+".info"), `{"Version":"`+version+`","Time":"2020-01-01T00:00:00Z"}`)
	writeFile(filepath.Join(versionDirectory, "list"), version+"\n")
	archive, err := os.Create(filepath.Join(versionDirectory, version+".zip"))
	Expect(err).ToNot(HaveOccurred())
	writer := zip.NewWriter(archive)
	for _, name := range sortedKeys(files) {
		entry, err := writer.Create(module + "@" + version + "/" + name)
		Expect(err).ToNot(HaveOccurred())
		_, err = entry.Write([]byte(files[name]))
		Expect(err).ToNot(HaveOccurred())
	}
	Expect(writer.Close()).To(Succeed())
	Expect(archive.Close()).To(Succeed())
}

// writeTypecheckFixture writes consumer/, a module importing typecheck-deps from the module cache
// and sibling/ through a local replace, and returns consumer's discovered root.
func writeTypecheckFixture(ctx context.Context, workspace string) discoveredRoot {
	GinkgoHelper()
	proxy, consumer, sibling := filepath.Join(workspace, "proxy"), filepath.Join(workspace, "consumer"), filepath.Join(workspace, "sibling")
	useModuleProxy(proxy)
	publishModule(proxy, typecheckDepsModule, typecheckDepsVersion, map[string]string{
		"go.mod": "module " + typecheckDepsModule + "\n\ngo 1.26\n", "generic/generic.go": genericSource, "shapes/shapes.go": shapesSource,
	})
	for _, directory := range []string{filepath.Join(sibling, "broken"), filepath.Join(consumer, "app")} {
		Expect(os.MkdirAll(directory, 0o755)).To(Succeed())
	}
	writeFile(filepath.Join(sibling, "go.mod"), "module example.org/sibling\n\ngo 1.26\n")
	writeFile(filepath.Join(sibling, "widget.go"), siblingSource)
	writeFile(filepath.Join(sibling, "broken", "broken.go"), brokenSource)
	writeFile(filepath.Join(consumer, "go.mod"), "module example.org/consumer\n\ngo 1.26\n\n"+
		"require (\n\texample.org/sibling v0.0.0\n\t"+typecheckDepsModule+" "+typecheckDepsVersion+"\n)\n\n"+
		"replace example.org/sibling => ../sibling\n")
	writeFile(filepath.Join(consumer, "app", "app.go"), consumerSource)
	writeFile(filepath.Join(consumer, "app", "app_test.go"), consumerTestSource)
	downloadModules(ctx, consumer)
	roots, err := discoverModules(ctx, consumer, true)
	Expect(err).ToNot(HaveOccurred())
	Expect(roots).To(HaveLen(1))
	return roots[0]
}

// goEnvValue is one variable of the go command's environment in directory.
func goEnvValue(ctx context.Context, directory, name string) string {
	GinkgoHelper()
	command := exec.CommandContext(ctx, "go", "env", name)
	command.Dir = directory
	output, err := command.Output()
	Expect(err).ToNot(HaveOccurred())
	value := strings.TrimSpace(string(output))
	Expect(value).ToNot(BeEmpty(), name)
	return value
}

// parsedInFull reports whether a file kept its comments or any function body.
func parsedInFull(file *ast.File) bool {
	if len(file.Comments) > 0 {
		return true
	}
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Body != nil {
			return true
		}
	}
	return false
}

// observingLoader loads packages and records, per parsed file, whether the load parsed it in full.
func observingLoader(forms map[string]bool) packageLoader {
	var mutex sync.Mutex
	return func(config *packages.Config, patterns ...string) ([]*packages.Package, error) {
		parse := config.ParseFile
		config.ParseFile = func(fileSet *token.FileSet, filename string, source []byte) (*ast.File, error) {
			file, err := parse(fileSet, filename, source)
			mutex.Lock()
			defer mutex.Unlock()
			forms[filename] = file != nil && parsedInFull(file)
			return file, err
		}
		return packages.Load(config, patterns...)
	}
}

// fullLoad is the typed load as it was before dependency files were parsed as declarations only:
// it keeps the load's bookkeeping of what it parsed, but type-checks every file parsed in full.
func fullLoad(config *packages.Config, patterns ...string) ([]*packages.Package, error) {
	record := config.ParseFile
	config.ParseFile = func(fileSet *token.FileSet, filename string, source []byte) (*ast.File, error) {
		// The full parse below reports every syntax error the recording parse reports.
		_, _ = record(token.NewFileSet(), filename, source)
		return parser.ParseFile(fileSet, filename, source, parser.AllErrors|parser.ParseComments|parser.SkipObjectResolution)
	}
	return packages.Load(config, patterns...)
}

// extractBoth extracts root once with the production load and once with fullLoad, without start times.
func extractBoth(ctx context.Context, root discoveredRoot) (moduleExtraction, moduleExtraction) {
	GinkgoHelper()
	declarations, err := extractModule(ctx, packages.Load, root, true)
	Expect(err).ToNot(HaveOccurred(), root.RootKey)
	full, err := extractModule(ctx, fullLoad, root, true)
	Expect(err).ToNot(HaveOccurred(), root.RootKey)
	declarations.indexStartedAt, full.indexStartedAt = time.Time{}, time.Time{}
	return declarations, full
}

// expectSameExtraction compares the documents one by one, so a difference names its file, and then
// the whole extraction.
func expectSameExtraction(declarations, full moduleExtraction) {
	GinkgoHelper()
	Expect(sortedKeys(declarations.documents)).To(Equal(sortedKeys(full.documents)), full.root.RootKey)
	for _, path := range sortedKeys(full.documents) {
		Expect(string(declarations.documents[path].content)).To(Equal(string(full.documents[path].content)), path)
	}
	Expect(declarations).To(Equal(full), full.root.RootKey)
}

var _ = Describe("typed load of dependencies", func() {
	It("parses standard-library and module-cache files without comments or function bodies, and every workspace file in full", func(ctx SpecContext) {
		workspace := canonicalTempDir()
		root := writeTypecheckFixture(ctx, workspace)
		forms := map[string]bool{}

		_, err := extractModule(ctx, observingLoader(forms), root, true)

		Expect(err).ToNot(HaveOccurred())
		cached := filepath.Join(goEnvValue(ctx, root.LocalPath, "GOMODCACHE"), filepath.FromSlash(typecheckDepsModule)+"@"+typecheckDepsVersion)
		standard := filepath.Join(goEnvValue(ctx, root.LocalPath, "GOROOT"), "src", "strings")
		fixture, standardFiles := map[string]bool{}, map[string]bool{}
		for name, full := range forms {
			if relative, err := filepath.Rel(workspace, name); err == nil && filepath.IsLocal(relative) {
				fixture[filepath.ToSlash(relative)] = full
			} else if relative, err := filepath.Rel(cached, name); err == nil && filepath.IsLocal(relative) {
				fixture["cache/"+filepath.ToSlash(relative)] = full
			} else if filepath.Dir(name) == standard {
				standardFiles[filepath.Base(name)] = full
			}
		}
		Expect(fixture).To(Equal(map[string]bool{
			"consumer/app/app.go": true, "consumer/app/app_test.go": true,
			"sibling/widget.go": true, "sibling/broken/broken.go": true,
			"cache/generic/generic.go": false, "cache/shapes/shapes.go": false,
		}))
		Expect(standardFiles).ToNot(BeEmpty())
		Expect(standardFiles).To(HaveEach(BeFalse()), "every file of package strings is parsed as declarations only")
	})

	It("extracts exactly what a load that parses every dependency file in full extracts", func(ctx SpecContext) {
		root := writeTypecheckFixture(ctx, canonicalTempDir())

		declarations, full := extractBoth(ctx, root)

		Expect(full.coverage).To(Equal(storage.CoverageIndexed), "the comparison covers typed documents")
		expectSameExtraction(declarations, full)
	})

	It("fails when a workspace module's sources sit in the module cache", func(ctx SpecContext) {
		workspace := canonicalTempDir()
		writeTypecheckFixture(ctx, workspace)
		mirror := filepath.Join(workspace, "mirror")
		cached := filepath.Join(goEnvValue(ctx, workspace, "GOMODCACHE"), filepath.FromSlash(typecheckDepsModule)+"@"+typecheckDepsVersion)
		writeModule(mirror, "example.org/mirror", "\nrequire "+typecheckDepsModule+" "+typecheckDepsVersion+"\n\nreplace "+typecheckDepsModule+" => "+cached+"\n",
			"package mirror\n\nimport \"example.org/typecheck-deps/generic\"\n\nvar Keys = generic.SortedKeys(generic.Defaults)\n")
		roots, err := discoverModules(ctx, mirror, false)
		Expect(err).ToNot(HaveOccurred())

		_, err = extractModule(ctx, packages.Load, roots[0], false)

		Expect(err).To(MatchError(And(ContainSubstring(typecheckDepsModule+"/generic"), ContainSubstring("without function bodies"))))
	})

	It("extracts real modules exactly as a load that parses every dependency file in full", Label("equivalence"), func(ctx SpecContext) {
		checkouts := os.Getenv(equivalenceRootsVariable)
		if checkouts == "" {
			Skip("set " + equivalenceRootsVariable + " to the checkouts to compare")
		}
		for _, checkout := range filepath.SplitList(checkouts) {
			roots, err := discoverModules(ctx, checkout, true)
			Expect(err).ToNot(HaveOccurred(), checkout)
			for _, root := range roots {
				declarations, full := extractBoth(ctx, root)
				expectSameExtraction(declarations, full)
				GinkgoWriter.Printf("%s: %d packages, %d documents, %d symbols, coverage %s: identical\n",
					root.LocalPath, len(full.packages), len(full.documents), len(full.symbols), full.coverage)
			}
		}
	})
})
