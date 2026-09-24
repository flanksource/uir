package query_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	catalogModule = "example.org/catalog"
	// catalogFuncs is more than two scan batches of 256, so a scan that stops at the limit reads a
	// fraction of the symbols its prefix matches.
	catalogFuncs = 600
	searchLimit  = 3
)

// catalogCheckout declares one type and catalogFuncs functions, all matching the prefix "item".
func catalogCheckout() string {
	GinkgoHelper()
	checkout := GinkgoT().TempDir()
	var source strings.Builder
	source.WriteString("package catalog\n\ntype ItemZ struct{}\n")
	for index := range catalogFuncs {
		fmt.Fprintf(&source, "\nfunc Item%03d() {}\n", index)
	}
	Expect(os.MkdirAll(filepath.Join(checkout, "catalog"), 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+catalogModule+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "catalog", "catalog.go"), []byte(source.String()), 0o644)).To(Succeed())
	return checkout
}

// symbolReads counts the queries against the symbols table and the rows they returned.
type symbolReads struct{ queries, rows int64 }

func countSymbolReads(database *gorm.DB) *symbolReads {
	GinkgoHelper()
	reads := &symbolReads{}
	Expect(database.Callback().Query().After("gorm:query").Register("test:count_symbol_reads", func(tx *gorm.DB) {
		if tx.Statement.Table == "symbols" {
			reads.queries++
			reads.rows += tx.Statement.RowsAffected
		}
	})).To(Succeed())
	return reads
}

func matchNames(matches []query.ModuleMatch) []string {
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match.Identifier.Method+match.Identifier.Type)
	}
	return names
}

const edgesModule = "example.org/edges"

// edgesCheckout declares names at the edges of the search alphabet: prefixes ending in z and 9, the
// names just past their upper bounds (Fiza, Fj), an underscore, and names of only z.
func edgesCheckout() string {
	GinkgoHelper()
	checkout := GinkgoT().TempDir()
	source := "package edges\n\ntype Fizz struct{}\n\ntype FizzBuzz struct{}\n\nfunc Fiz9() {}\n\nfunc Fiz9Lives() {}\n\n" +
		"func Fiza() {}\n\nfunc Fj() {}\n\nfunc Save_V2() {}\n\nfunc Zy() {}\n\nfunc Zz() {}\n\nfunc Zzz() {}\n"
	Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+edgesModule+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "edges.go"), []byte(source), 0o644)).To(Succeed())
	return checkout
}

// collateSearchNames gives symbols.search_name PostgreSQL's ICU root collation, under which
// punctuation sorts below digits and letters, so a bound past the alphabet would sort below the prefix.
func collateSearchNames(database *gorm.DB) {
	GinkgoHelper()
	Expect(database.Exec(`ALTER TABLE symbols ALTER COLUMN search_name TYPE text COLLATE "und-x-icu"`).Error).To(Succeed())
	var ordered []string
	Expect(database.Raw(`SELECT value FROM (VALUES ('fizz'), ('fiz{'), ('fiz9'), ('fiz:')) AS v(value) ORDER BY value COLLATE "und-x-icu"`).Scan(&ordered).Error).To(Succeed())
	Expect(ordered).To(Equal([]string{"fiz:", "fiz{", "fiz9", "fizz"}), "punctuation sorts below digits and letters under the collation")
}

var _ = Describe("module search", func() {
	DescribeTable("matches prefixes at the edges of the search alphabet",
		func(ctx SpecContext, backend string) {
			database := openQueryDatabase(ctx, backend)
			if backend == "postgres" {
				collateSearchNames(database)
			}
			indexCheckout(ctx, database, edgesCheckout())
			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())
			found := map[string][]string{}
			for _, input := range []string{"fizz", "fiz9", "save_v2", "savev2", "zz"} {
				found[input] = matchNames(runQuery(ctx, pipeline, fmt.Sprintf("search %q", input), query.ModuleScopeOptions{RootKey: edgesModule, Limit: 100}).Matches)
			}
			Expect(found).To(Equal(map[string][]string{
				"fizz":    {"Fizz", "FizzBuzz"},
				"fiz9":    {"Fiz9", "Fiz9Lives"},
				"save_v2": {"Save_V2"},
				"savev2":  {"Save_V2"},
				"zz":      {"Zz", "Zzz"},
			}))
		},
		Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))

	DescribeTable("stops scanning once the limit is filled and returns the rows in rank order",
		func(ctx SpecContext, backend string) {
			database := openQueryDatabase(ctx, backend)
			indexCheckout(ctx, database, catalogCheckout())
			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())

			everything := runQuery(ctx, pipeline, `search "item"`, query.ModuleScopeOptions{RootKey: catalogModule, Limit: 1000})
			Expect(everything.Matches).To(HaveLen(catalogFuncs+1), "the prefix matches the type and every function")

			reads := countSymbolReads(database)
			limited := runQuery(ctx, pipeline, `search "item"`, query.ModuleScopeOptions{RootKey: catalogModule, Limit: searchLimit})
			Expect(matchNames(limited.Matches)).To(Equal([]string{"ItemZ", "Item000", "Item001"}), "types rank before functions")
			Expect(limited.Matches).To(Equal(everything.Matches[:searchLimit]), "the limited rows are the head of the full ranking")
			Expect(*reads).To(Equal(symbolReads{queries: 2, rows: 1 + 256}),
				"one scan batch for the types and one for the functions, never the whole prefix range")
		},
		Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
})
