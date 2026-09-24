package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	ledgerModule = "example.org/ledger"
	moneyPackage = ledgerModule + "/money"
	bookPackage  = ledgerModule + "/book"

	moneyAmount = "// Amount is a quantity of money.\n" +
		"type Amount struct {\n" +
		"\tCents    int64\n" +
		"\tCurrency string `json:\"currency\"`\n" +
		"}\n"
	moneyAdd = "// Add sums two amounts.\n" +
		"func Add(a Amount, b Amount) Amount {\n" +
		"\treturn Amount{Cents: a.Cents + b.Cents, Currency: a.Currency}\n" +
		"}\n"
	moneyScale = "func Scale(a Amount, factor int) Amount { return Amount{Cents: a.Cents * int64(factor), Currency: a.Currency} }\n"
	moneyUnit  = "func Unit() Amount { return Amount{Cents: 1} }\n"
	moneyZero  = "func Zero() Amount { return Amount{} }\n"

	bookSource = "package book\n" +
		"\n" +
		"import \"example.org/ledger/money\"\n" +
		"\n" +
		"// Namer names things.\n" +
		"type Namer interface {\n" +
		"\tName() string\n" +
		"}\n" +
		"\n" +
		"type Book struct {\n" +
		"\tTitle string\n" +
		"\tPrice money.Amount\n" +
		"}\n" +
		"\n" +
		"func (b Book) Name() string { return b.Title }\n" +
		"\n" +
		"func Total(books []Book) money.Amount {\n" +
		"\ttotal := money.Zero()\n" +
		"\tfor _, book := range books {\n" +
		"\t\ttotal = money.Add(total, book.Price)\n" +
		"\t}\n" +
		"\treturn total\n" +
		"}\n"
	countSource = "func Count(books []Book) int { return len(books) }\n"
	useSource   = "package book\n\nfunc Twice(b Book) string { return b.Name() + b.Name() }\n"
)

// moneySource assembles money/money.go from its declarations so an edit replaces exactly one of them.
func moneySource(declarations ...string) string {
	return "package money\n\n" + strings.Join(declarations, "\n")
}

func baselineMoney() string {
	return moneySource(moneyAmount, moneyAdd, moneyScale, moneyUnit, moneyZero)
}

// writeLedgerModule writes example.org/ledger: money declares Amount and its functions; book imports
// money, declares an interface its struct implements, and calls Book.Name twice from use.go.
func writeLedgerModule(workspace string) {
	GinkgoHelper()
	for _, directory := range []string{"money", "book"} {
		Expect(os.MkdirAll(filepath.Join(workspace, directory), 0o755)).To(Succeed())
	}
	writeFile(filepath.Join(workspace, "go.mod"), "module "+ledgerModule+"\n\ngo 1.26\n")
	writeFile(filepath.Join(workspace, "money", "money.go"), baselineMoney())
	writeFile(filepath.Join(workspace, "book", "book.go"), bookSource+"\n"+countSource)
	writeFile(filepath.Join(workspace, "book", "use.go"), useSource)
}

// typedSnapshot is a published snapshot's active documents decoded, keyed by path.
type typedSnapshot struct {
	id        string
	documents map[string]storage.Document
	contents  map[string]storage.DocumentContent
}

func loadTypedSnapshot(ctx context.Context, database *gorm.DB, snapshotID string) typedSnapshot {
	GinkgoHelper()
	active, err := storage.ActiveDocuments(ctx, database, uuid.MustParse(snapshotID))
	Expect(err).ToNot(HaveOccurred())
	loaded := typedSnapshot{id: snapshotID, documents: map[string]storage.Document{}, contents: map[string]storage.DocumentContent{}}
	for path, document := range active {
		content, err := storage.DecodeDocument(document.Document, document.Source)
		Expect(err).ToNot(HaveOccurred(), path)
		loaded.documents[path], loaded.contents[path] = document.Document, content
	}
	return loaded
}

func (snapshot typedSnapshot) documentIDs() map[string]uuid.UUID {
	ids := map[string]uuid.UUID{}
	for path, document := range snapshot.documents {
		ids[path] = document.ID
	}
	return ids
}

// symbol returns the one symbols row with this package, kind, and name.
func findSymbol(database *gorm.DB, packagePath, kind, name string) storage.Symbol {
	GinkgoHelper()
	var rows []storage.Symbol
	Expect(database.Where("package_path = ? AND kind = ? AND name = ?", packagePath, kind, name).Find(&rows).Error).To(Succeed())
	Expect(rows).To(HaveLen(1), "%s %s.%s", kind, packagePath, name)
	return rows[0]
}

// entry returns the symbol entry with this id in the document at path.
func (snapshot typedSnapshot) entry(path, id string) storage.DocumentSymbol {
	GinkgoHelper()
	for _, symbol := range snapshot.contents[path].Symbols {
		if symbol.ID != nil && *symbol.ID == id {
			return symbol
		}
	}
	Fail("symbol " + id + " is not declared in " + path)
	return storage.DocumentSymbol{}
}

func (snapshot typedSnapshot) packageRow(database *gorm.DB, packagePath string) storage.PackageCoverage {
	GinkgoHelper()
	var row storage.PackageCoverage
	Expect(database.Where("snapshot_id = ? AND package_path = ?", snapshot.id, packagePath).First(&row).Error).To(Succeed())
	return row
}

// postings returns this symbol's postings among the snapshot's active documents as "path role count".
func (snapshot typedSnapshot) postings(database *gorm.DB, symbolID string) []string {
	GinkgoHelper()
	var rows []storage.SymbolPosting
	Expect(database.Where("symbol_id = ?", symbolID).Find(&rows).Error).To(Succeed())
	paths := map[uuid.UUID]string{}
	for path, document := range snapshot.documents {
		paths[document.ID] = path
	}
	result := []string{}
	for _, row := range rows {
		if path, active := paths[row.DocumentID]; active {
			result = append(result, path+" "+row.Role+" "+strconv.Itoa(row.OccurrenceCount))
		}
	}
	return result
}

func tableCounts(database *gorm.DB) map[string]int64 {
	GinkgoHelper()
	return map[string]int64{
		"symbols":          countRows(database, &storage.Symbol{}, "1 = 1"),
		"documents":        countRows(database, &storage.Document{}, "1 = 1"),
		"symbol_postings":  countRows(database, &storage.SymbolPosting{}, "1 = 1"),
		"package_coverage": countRows(database, &storage.PackageCoverage{}, "1 = 1"),
		"snapshots":        countRows(database, &storage.ModuleSnapshot{}, "1 = 1"),
		"source_revisions": countRows(database, &storage.SourceRevision{}, "1 = 1"),
		"source_deltas":    countRows(database, &storage.SourceDelta{}, "1 = 1"),
	}
}
