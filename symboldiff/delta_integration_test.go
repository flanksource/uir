package symboldiff

import (
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const tallyRoot = "example.org/tally"

var tallyBefore = map[string]*string{
	"go.mod":           text("module " + tallyRoot + "\n\ngo 1.26\n"),
	"report/report.go": text("package report\n\nfunc Header() string { return \"tally\" }\n"),
	"tally/tally.go": text(lines(
		"package tally",
		"",
		"// Store keeps counts.",
		"type Store struct {",
		"\tCounts map[string]int",
		"}",
		"",
		"// Add records one occurrence.",
		"func (s *Store) Add(key string) {",
		"\ts.Counts[key]++",
		"}",
		"",
		"func Total(values []int) int {",
		"\tsum := 0",
		"\tfor _, value := range values {",
		"\t\tsum += value",
		"\t}",
		"\treturn sum",
		"}",
		"",
		"func Count(values []int) int { return len(values) }",
		"",
		"func helper() int { return 1 }",
	)),
}

// tallyAfter edits bodies of an exported method and function, moves Count to its own file, adds an
// internal function, and changes only a comment of report.go: no exported signature or identity changes.
var tallyAfter = map[string]*string{
	"report/report.go": text("// Package report renders tallies.\npackage report\n\nfunc Header() string { return \"tally\" }\n"),
	"tally/count.go":   text("package tally\n\nfunc Count(values []int) int { return len(values) }\n"),
	"tally/tally.go": text(lines(
		"package tally",
		"",
		"// Store keeps counts.",
		"type Store struct {",
		"\tCounts map[string]int",
		"}",
		"",
		"// Add records one occurrence.",
		"func (s *Store) Add(key string) {",
		"\ts.Counts[key] += 1",
		"}",
		"",
		"func Total(values []int) int {",
		"\tsum := 0",
		"\tfor _, value := range values {",
		"\t\tsum = sum + value",
		"\t}",
		"\treturn sum",
		"}",
		"",
		"func helper() int { return 1 }",
		"",
		"func later() int { return 2 }",
	)),
}

// countDecodes replaces the decode seam for the rest of the spec and returns the decoded paths.
func countDecodes() *[]string {
	decoded := &[]string{}
	original := decodeDocument
	decodeDocument = func(document storage.Document, source storage.SourceRevision) (storage.DocumentContent, error) {
		*decoded = append(*decoded, document.PathKey)
		return original(document, source)
	}
	DeferCleanup(func() { decodeDocument = original })
	return decoded
}

func indexPair(ctx SpecContext, options storage.DBOptions, before, after map[string]*string) ledger {
	GinkgoHelper()
	database := openDatabase(ctx, options)
	repo := newRepository()
	repo.write(before)
	from := repo.commit("before")
	repo.index(ctx, database)
	repo.write(after)
	to := repo.commit("after")
	repo.index(ctx, database)
	return ledger{repo: repo, database: database, from: from, to: to}
}

func (fixture ledger) diffRoot(ctx SpecContext, root string, visibility Visibility, stat bool) Result {
	GinkgoHelper()
	result, err := Diff(ctx, fixture.database, Options{RootKey: root, From: fixture.from, To: fixture.to, Visibility: visibility, Stat: stat})
	Expect(err).ToNot(HaveOccurred())
	return result
}

var _ = Describe("diff on symbol deltas", func() {
	DescribeTable("classifies body and moved rows without decoding a document and decodes only what added rows show",
		func(ctx SpecContext, options func() storage.DBOptions) {
			fixture := indexPair(ctx, options(), tallyBefore, tallyAfter)
			decoded := countDecodes()
			var outputs [3]string
			var decodes [3][]string
			for index, request := range []struct {
				visibility Visibility
				stat       bool
			}{{VisibilityExported, false}, {VisibilityAll, false}, {VisibilityAll, true}} {
				*decoded = nil
				outputs[index] = body(fixture.diffRoot(ctx, tallyRoot, request.visibility, request.stat))
				decodes[index] = *decoded
			}
			Expect(outputs).To(Equal([3]string{tallyExported, tallyAll, tallyStatAll}))
			Expect(decodes[0]).To(BeEmpty(), "an exported diff without signature, added, or removed rows reads no document")
			Expect(decodes[1]).To(Equal([]string{"tally/tally.go"}), "only the new side of the file that declares the added symbol")
			Expect(decodes[2]).To(ConsistOf("report/report.go", "report/report.go", "tally/tally.go", "tally/tally.go", "tally/count.go"),
				"line counts read both sides of every changed file")
		},
		Entry("SQLite", sqliteOptions),
		Entry("PostgreSQL", postgresOptions),
	)

	DescribeTable("pairs the func init declarations that share one canonical id (gavel TODO 34ea4d17) in declaration order",
		func(ctx SpecContext, options func() storage.DBOptions) {
			fixture := indexPair(ctx, options(), bootBefore, bootAfter)
			Expect([]string{body(fixture.diffRoot(ctx, bootRoot, VisibilityAll, false)), body(fixture.diffRoot(ctx, bootRoot, VisibilityAll, true))}).
				To(Equal([]string{bootAll, bootStatAll}))
		},
		Entry("SQLite", sqliteOptions),
		Entry("PostgreSQL", postgresOptions),
	)

	DescribeTable("matches syntax and partial packages by key without line counts",
		func(ctx SpecContext, options func() storage.DBOptions) {
			fixture := indexPair(ctx, options(), fallbackBefore, fallbackAfter)
			Expect([]string{body(fixture.diffRoot(ctx, fallbackRoot, VisibilityAll, false)), body(fixture.diffRoot(ctx, fallbackRoot, VisibilityExported, false))}).
				To(Equal([]string{fallbackAll, fallbackExported}))
		},
		Entry("SQLite", sqliteOptions),
		Entry("PostgreSQL", postgresOptions),
	)

	It("names every symbol from the symbols table exactly as its decoded declaration does", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		repo := newRepository()
		repo.write(shapesModule)
		repo.commit("shapes")
		active, err := storage.ActiveDocuments(ctx, database, uuid.MustParse(repo.index(ctx, database)), storage.ActiveDocumentOptions{Content: true})
		Expect(err).ToNot(HaveOccurred())
		var declared []declaredName
		ids := []string{}
		for _, document := range active {
			content, err := storage.DecodeDocument(document.Document, document.Source)
			Expect(err).ToNot(HaveOccurred())
			for _, symbol := range content.Symbols {
				declared = append(declared, nameOf(symbol.Kind, symbol.Visibility, symbol.Identifier))
				ids = append(ids, *symbol.ID)
			}
		}
		handles, err := storage.SymbolHandles(ctx, database, ids)
		Expect(err).ToNot(HaveOccurred())
		handleList := make([]int64, 0, len(handles))
		for _, handle := range handles {
			handleList = append(handleList, handle)
		}
		owned, err := storage.OwnedSymbols(ctx, database, handleList)
		Expect(err).ToNot(HaveOccurred())
		var fromRows []declaredName
		for _, symbol := range owned {
			identifier, err := symbol.Identifier()
			Expect(err).ToNot(HaveOccurred())
			fromRows = append(fromRows, nameOf(symbol.Kind, symbol.Visibility, identifier))
		}
		Expect(fromRows).To(ConsistOf(declared))
		Expect(declared).To(ContainElements(
			declaredName{Kind: "method", Visibility: "exported", Display: "List.Push"},
			declaredName{Kind: "method", Visibility: "exported", Display: "Reader.Read"},
			declaredName{Kind: "field", Visibility: "exported", Display: "List.Meta.Tags.Name"},
			declaredName{Kind: "field", Visibility: "exported", Display: "List.Embedded"},
			declaredName{Kind: "var", Visibility: "internal", Display: "second"},
			declaredName{Kind: "const", Visibility: "exported", Display: "Limit"},
		), "the fixture declares methods, interface methods, nested and embedded fields, vars, and consts")
	})
})

// declaredName is what a diff row reports of a symbol's identity, without its id.
type declaredName struct{ Kind, Visibility, Display string }

func nameOf(kind, visibility string, identifier uir.Identifier) declaredName {
	owner, name := ownerAndName(identifier)
	return declaredName{Kind: kind, Visibility: visibility, Display: Row{Owner: owner, Name: name}.DisplayName()}
}

const bootRoot = "example.org/boot"

var bootBefore = map[string]*string{
	"go.mod":    text("module " + bootRoot + "\n\ngo 1.26\n"),
	"boot/a.go": text("package boot\n\nvar order []string\n\nfunc init() { order = append(order, \"a\") }\n\n// Order lists the init order.\nfunc Order() []string { return order }\n"),
	"boot/b.go": text("package boot\n\nfunc init() { order = append(order, \"b\") }\n"),
}

// bootAfter edits the body of b.go's init and adds a third init in a new file.
var bootAfter = map[string]*string{
	"boot/b.go": text("package boot\n\nfunc init() { order = append(order, \"b2\") }\n"),
	"boot/c.go": text("package boot\n\nfunc init() { order = append(order, \"c\") }\n"),
}

const bootAll = `example.org/boot/boot
  boot/b.go (modified)
    init                 body
  boot/c.go (added)
    init                 added      func init()
`

const bootStatAll = `example.org/boot/boot  +4 -1
  boot/b.go (modified)  +1 -1
    init                 body      +1 -1
    (file scope)                   +0 -0
  boot/c.go (added)  +3 -0
    init                 added     +1 -0  func init()
    (file scope)                   +2 -0
`

const fallbackRoot = "example.org/fallback"

var fallbackBefore = map[string]*string{
	"go.mod":         text("module " + fallbackRoot + "\n\ngo 1.26\n"),
	"typed/typed.go": text("package typed\n\nfunc Wrong() int { return \"text\" }\n\nfunc Right() int { return 1 }\n\nfunc gone() {}\n"),
	"flip/flip.go":   text("package flip\n\nfunc Flip() int { return 1 }\n\nfunc Keep() {}\n\nfunc drop() {}\n"),
	"mixed/a.go":     text("package alpha\n\nfunc A() {}\n"),
	"mixed/b.go":     text("package beta\n\nfunc B() {}\n"),
	"fine/fine.go":   text("package fine\n\nfunc Fine() {}\n"),
}

// fallbackAfter keeps typed partial on both sides, turns flip from indexed into syntax (a second
// package name), and edits mixed, which is syntax on both sides.
var fallbackAfter = map[string]*string{
	"typed/typed.go": text("package typed\n\nfunc Wrong() int { return \"text\" }\n\nfunc Right() int { return 2 }\n\nfunc Added() {}\n"),
	"flip/flip.go":   text("package flip\n\nfunc Flip() int { return 2 }\n\nfunc Keep() {}\n"),
	"flip/other.go":  text("package other\n\nfunc Other() {}\n"),
	"mixed/a.go":     text("package alpha\n\nfunc A() { _ = 1 }\n"),
}

const fallbackAll = `example.org/fallback/flip
  flip/flip.go (modified) [syntax]
    Flip                 body      [syntax]
    drop                 removed   [syntax]  func drop()
  flip/other.go (added) [syntax]
    Other                added     [syntax]  func Other()
example.org/fallback/mixed
  mixed/a.go (modified) [syntax]
    A                    body      [syntax]
example.org/fallback/typed
  typed/typed.go (modified) [partial]
    Added                added     [partial]  func Added()
    Right                body      [partial]
    gone                 removed   [partial]  func gone()
`

const fallbackExported = `example.org/fallback/flip
  flip/flip.go (modified) [syntax]  (1 hidden row)
    Flip                 body      [syntax]
  flip/other.go (added) [syntax]
    Other                added     [syntax]  func Other()
example.org/fallback/mixed
  mixed/a.go (modified) [syntax]
    A                    body      [syntax]
example.org/fallback/typed
  typed/typed.go (modified) [partial]  (1 hidden row)
    Added                added     [partial]  func Added()
    Right                body      [partial]
`

var shapesModule = map[string]*string{
	"go.mod": text("module example.org/shapes\n\ngo 1.26\n"),
	"shapes/shapes.go": text(lines(
		"package shapes",
		"",
		"type List[T any] struct {",
		"\titems []T",
		"\tMeta  struct {",
		"\t\tSize int",
		"\t\tTags struct{ Name string }",
		"\t}",
		"\tEmbedded",
		"\t*Other",
		"}",
		"",
		"type Embedded struct{ Flag bool }",
		"",
		"type Other struct{}",
		"",
		"type Alias = Other",
		"",
		"func (l *List[T]) Push(item T) { l.items = append(l.items, item) }",
		"",
		"func (o Other) close() {}",
		"",
		"type Reader interface {",
		"\tRead(p []byte) (int, error)",
		"}",
		"",
		"var Default = List[int]{}",
		"",
		"const Limit = 3",
		"",
		"var first, second = 1, 2",
		"",
		"func Free() {}",
	)),
}

const tallyExported = `example.org/tally/report
  report/report.go (modified)
example.org/tally/tally
  tally/count.go (added)
    Count                moved      from tally/tally.go
  tally/tally.go (modified)  (1 hidden row)
    Store.Add            body
    Total                body
`

const tallyAll = `example.org/tally/report
  report/report.go (modified)
example.org/tally/tally
  tally/count.go (added)
    Count                moved      from tally/tally.go
  tally/tally.go (modified)
    Store.Add            body
    Total                body
    later                added      func later() int
`

const tallyStatAll = `example.org/tally/report  +1 -0
  report/report.go (modified)  +1 -0
    (file scope)                   +1 -0
example.org/tally/tally  +5 -2
  tally/count.go (added)  +2 -0
    Count                moved     +0 -0  from tally/tally.go
    (file scope)                   +2 -0
  tally/tally.go (modified)  +3 -2
    Store.Add            body      +1 -1
    Total                body      +1 -1
    later                added     +1 -0  func later() int
    (file scope)                   +0 -0
`
