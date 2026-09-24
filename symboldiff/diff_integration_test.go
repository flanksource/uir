package symboldiff

import (
	"strings"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// file returns the file diff at path, failing when it is absent.
func (result Result) file(path string) FileDiff {
	GinkgoHelper()
	for _, pkg := range result.Packages {
		for _, file := range pkg.Files {
			if file.Path == path {
				return file
			}
		}
	}
	Fail("the diff has no file " + path)
	return FileDiff{}
}

// row returns the one row of the file with this display name and class.
func (file FileDiff) row(name string, class Class) Row {
	GinkgoHelper()
	var found []Row
	for _, row := range file.Rows {
		if row.DisplayName() == name && row.Class == class {
			found = append(found, row)
		}
	}
	Expect(found).To(HaveLen(1), "%s %s in %s", class, name, file.Path)
	return found[0]
}

func (file FileDiff) names() []string {
	names := []string{}
	for _, row := range file.Rows {
		names = append(names, string(row.Class)+" "+row.DisplayName())
	}
	return names
}

// body is the plain-text rendering without its header line.
func body(result Result) string {
	GinkgoHelper()
	rendered := result.Pretty().String()
	header, rest, found := strings.Cut(rendered, "\n")
	Expect(found).To(BeTrue())
	Expect(header).To(Equal(result.RootKey + " " + result.From.Commit + ".." + result.To.Commit))
	return rest
}

type ledger struct {
	repo     *repository
	database *gorm.DB
	from, to string
}

func indexLedger(ctx SpecContext, options storage.DBOptions) ledger {
	GinkgoHelper()
	database := openDatabase(ctx, options)
	repo := newRepository()
	repo.write(ledgerBefore)
	from := repo.commit("ledger before")
	repo.index(ctx, database)
	repo.write(ledgerAfter)
	to := repo.commit("ledger after")
	repo.index(ctx, database)
	return ledger{repo: repo, database: database, from: from, to: to}
}

func (fixture ledger) diff(ctx SpecContext, visibility Visibility, stat bool) Result {
	GinkgoHelper()
	result, err := Diff(ctx, fixture.database, Options{RootKey: ledgerRoot, From: fixture.from, To: fixture.to, Visibility: visibility, Stat: stat})
	Expect(err).ToNot(HaveOccurred())
	return result
}

var _ = Describe("commit-to-commit diff", func() {
	DescribeTable("classifies every edit-table row, attributes lines by symbol, and renders shape diffs",
		func(ctx SpecContext, options func() storage.DBOptions) {
			fixture := indexLedger(ctx, options())
			all := fixture.diff(ctx, VisibilityAll, true)
			Expect(body(all)).To(Equal(ledgerStatAll))
			Expect(*all.file("money/money.go").Lines).To(Equal(fixture.repo.numstat(fixture.from, fixture.to, "money/money.go")),
				"a file without moved symbols totals exactly what Git counts")
			Expect(all.From.WorktreeState).To(Equal("clean"))
			Expect(all.To.Checkout).To(Equal(fixture.repo.path))

			money := all.file("money/money.go")
			Expect(money.row("Scale", ClassSignature)).To(Equal(Row{
				Class: ClassSignature, Kind: "func", Name: "Scale", Visibility: "exported",
				ShapeBefore: "func Scale(a Amount, factor int) Amount", ShapeAfter: "func Scale(a Amount, by int) Amount",
				ShapeDiff:  DiffShapes("func Scale(a Amount, factor int) Amount", "func Scale(a Amount, by int) Amount"),
				PathBefore: "money/money.go", PathAfter: "money/money.go", Lines: &LineCount{Added: 2, Removed: 2},
			}))
			Expect(money.row("Scale", ClassSignature).ShapeDiff[0].Tokens).To(Equal(tokens("equal", "func Scale(a Amount, ", "delete", "factor", "equal", " int) Amount")))
			Expect(money.row("Amount.Currency", ClassSignature)).To(HaveField("Lines", BeNil()), "a nested symbol's lines belong to its outermost declaration")
			removed, added := money.row("Half", ClassRemoved), money.row("Half", ClassAdded)
			Expect([]string{removed.Group, added.Group, added.ShapeBefore, added.ShapeAfter}).To(Equal([]string{"Half", "Half", "func Half(a Amount) Amount", "func Half(a *Amount) Amount"}))
			Expect(removed.ShapeDiff).To(BeNil())
			Expect(added.ShapeDiff).To(Equal(DiffShapes("func Half(a Amount) Amount", "func Half(a *Amount) Amount")))
			Expect(money.row("Add", ClassBody).Note).To(Equal(NoteLayoutOnly))
			Expect(all.file("book/count.go").row("Count", ClassMoved)).To(Equal(Row{
				Class: ClassMoved, Kind: "func", Name: "Count", Visibility: "exported",
				PathBefore: "book/book.go", PathAfter: "book/count.go", Lines: &LineCount{},
			}))

			exported := fixture.diff(ctx, VisibilityExported, false)
			Expect(body(exported)).To(Equal(ledgerExported))
			Expect(exported.Packages[0].Lines).To(BeNil())

			exportedStat := fixture.diff(ctx, VisibilityExported, true)
			hidden := exportedStat.file("money/money.go")
			Expect([]any{hidden.HiddenRows, *hidden.Lines}).To(Equal([]any{1, LineCount{Added: 11, Removed: 6}}),
				"totals still include the hidden round row")
			Expect(hidden.names()).ToNot(ContainElement("body round"))

			internal := fixture.diff(ctx, VisibilityInternal, true).file("money/money.go")
			Expect([]any{internal.names(), internal.HiddenRows}).To(Equal([]any{[]string{"body round"}, 8}))

			short, err := Diff(ctx, fixture.database, Options{RootKey: ledgerRoot, From: "HEAD~1", To: "main", Visibility: VisibilityAll, Stat: true})
			Expect(err).ToNot(HaveOccurred())
			Expect([]string{short.From.Commit, short.To.Commit}).To(Equal([]string{fixture.from, fixture.to}))
			Expect(body(short)).To(Equal(ledgerStatAll))
		},
		Entry("SQLite", sqliteOptions),
		Entry("PostgreSQL", postgresOptions),
	)

	DescribeTable("marks partial and syntax rows and attributes an excluded file to file scope",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDatabase(ctx, options())
			repo := newRepository()
			repo.write(map[string]*string{
				"go.mod":         text("module example.org/broken\n\ngo 1.26\n"),
				"typed/typed.go": text("package typed\n\nfunc Wrong() int { return \"text\" }\n\nfunc Right() int { return 1 }\n"),
				"mixed/a.go":     text("package alpha\n\nfunc A() {}\n"),
				"mixed/b.go":     text("package beta\n\nfunc B() {}\n"),
				"fine/fine.go":   text("package fine\n\nfunc Fine() {}\n"),
				"fine/never.go":  text("//go:build uirnevertagged\n\npackage fine\n\nfunc Never() {}\n"),
			})
			from := repo.commit("broken before")
			repo.index(ctx, database)
			repo.write(map[string]*string{
				"typed/typed.go": text("package typed\n\nfunc Wrong() int { return \"text\" }\n\nfunc Right() int { return 2 }\n"),
				"mixed/a.go":     text("package alpha\n\nfunc A() { _ = 1 }\n"),
				"fine/never.go":  text("//go:build uirnevertagged\n\npackage fine\n\nfunc Never() {}\n\nfunc Later() {}\n"),
			})
			to := repo.commit("broken after")
			repo.index(ctx, database)

			result, err := Diff(ctx, database, Options{RootKey: "example.org/broken", From: from, To: to, Visibility: VisibilityAll, Stat: true})
			Expect(err).ToNot(HaveOccurred())
			Expect(body(result)).To(Equal(`example.org/broken/fine  +2 -0
  fine/never.go (modified) [excluded]  +2 -0
    (file scope)                   +2 -0
example.org/broken/mixed  +1 -1
  mixed/a.go (modified) [syntax]  +1 -1
    A                    body      +1 -1 [syntax]
    (file scope)                   +0 -0
example.org/broken/typed  +1 -1
  typed/typed.go (modified) [partial]  +1 -1
    Right                body      +1 -1 [partial]
    (file scope)                   +0 -0
`))
			Expect(result.file("fine/never.go").Excluded).To(HavePrefix("build constraints exclude the file"))
		},
		Entry("SQLite", sqliteOptions),
		Entry("PostgreSQL", postgresOptions),
	)

	DescribeTable("verifies line counts against checkout bytes when .gitattributes converts line endings",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDatabase(ctx, options())
			repo := newRepository()
			crlf := func(parts ...string) *string { return text(strings.Join(parts, "\r\n") + "\r\n") }
			repo.write(map[string]*string{
				".gitattributes": text("* text eol=crlf\n"),
				"go.mod":         crlf("module example.org/crlf", "", "go 1.26"),
				"eol/eol.go":     crlf("package eol", "", "func Keep() int {", "\treturn 1", "}"),
			})
			from := repo.commit("crlf before")
			repo.index(ctx, database)
			repo.write(map[string]*string{
				"go.mod":     crlf("module example.org/crlf", "", "go 1.26", "", "toolchain go1.26.0"),
				"eol/eol.go": crlf("package eol", "", "func Keep() int {", "\treturn 2", "}"),
			})
			to := repo.commit("crlf after")
			repo.index(ctx, database)
			Expect(repo.git("cat-file", "-p", to+":eol/eol.go")).ToNot(ContainSubstring("\r"), "the committed blob is normalised to LF")

			result, err := Diff(ctx, database, Options{RootKey: "example.org/crlf", From: from, To: to, Visibility: VisibilityAll, Stat: true})
			Expect(err).ToNot(HaveOccurred())
			Expect(result.LinesError).To(BeEmpty())
			for _, path := range []string{"eol/eol.go", "go.mod"} {
				counted := repo.numstat(from, to, path)
				Expect(result.file(path)).To(And(HaveField("LinesError", BeEmpty()), HaveField("Lines", Equal(&counted))), path)
			}
		},
		Entry("SQLite", sqliteOptions),
		Entry("PostgreSQL", postgresOptions),
	)
})
