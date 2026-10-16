package symboldiff

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	accountRoot = "example.org/accounts"
	// Account gains an unexported field: its private shape changes while its public shape, which already
	// stands for the unexported balance with an opaque line, does not. Owner gains an exported field.
	accountsBefore = "package accounts\n\ntype Account struct {\n\tID      string\n\tbalance int64\n}\n\ntype Owner struct {\n\tName string\n}\n"
	accountsAfter  = "package accounts\n\ntype Account struct {\n\tID       string\n\tbalance  int64\n\tcurrency string\n}\n\ntype Owner struct {\n\tName  string\n\tEmail string\n}\n"
)

var _ = Describe("layered classification", func() {
	DescribeTable("reports a private-only change as structure, shown only with internal or all visibility, and a public change as signature",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDatabase(ctx, options())
			repo := newRepository()
			repo.write(map[string]*string{"go.mod": text("module " + accountRoot + "\n\ngo 1.26\n"), "accounts.go": text(accountsBefore)})
			from := repo.commit("accounts before")
			repo.index(ctx, database)
			repo.write(map[string]*string{"accounts.go": text(accountsAfter)})
			to := repo.commit("accounts after")
			repo.index(ctx, database)
			diff := func(visibility Visibility, stat bool) FileDiff {
				result, err := Diff(ctx, database, Options{RootKey: accountRoot, Revisions: []string{from, to}, Visibility: visibility, Stat: stat})
				Expect(err).ToNot(HaveOccurred())
				return result.file("accounts.go")
			}

			all := diff(VisibilityAll, false)
			Expect(all.names()).To(Equal([]string{"structure Account", "added Account.currency", "signature Owner", "added Owner.Email"}))
			privateBefore := "type Account struct {\n\tID      string\n\tbalance int64\n}"
			privateAfter := "type Account struct {\n\tID       string\n\tbalance  int64\n\tcurrency string\n}"
			Expect(all.row("Account", ClassStructure)).To(Equal(Row{
				Class: ClassStructure, Kind: "type", Name: "Account", Visibility: "exported",
				ShapeBefore: privateBefore, ShapeAfter: privateAfter, ShapeDiff: DiffShapes(privateBefore, privateAfter, TokenizerGo),
				PathBefore: "accounts.go", PathAfter: "accounts.go",
			}))
			publicBefore, publicAfter := "type Owner struct {\n\tName string\n}", "type Owner struct {\n\tName  string\n\tEmail string\n}"
			Expect(all.row("Owner", ClassSignature)).To(HaveField("ShapeDiff", Equal(DiffShapes(publicBefore, publicAfter, TokenizerGo))))

			exported := diff(VisibilityExported, true)
			Expect([]any{exported.names(), exported.HiddenRows, *exported.Lines}).To(Equal([]any{
				[]string{"signature Owner", "added Owner.Email"}, 2, repo.numstat(from, to, "accounts.go"),
			}), "the hidden structure and unexported-field rows still count in the file's lines")

			internal := diff(VisibilityInternal, false)
			Expect([]any{internal.names(), internal.HiddenRows}).To(Equal([]any{[]string{"structure Account", "added Account.currency"}, 2}))
		},
		Entry("SQLite", sqliteOptions),
		Entry("PostgreSQL", postgresOptions),
	)
})

// The schema fixture is one externally published table, dbo.ORDERS, at two producer revisions: v2 adds
// the nullable column NOTE, which changes the table's public layer.
const (
	schemaRoot    = "example.org/schema"
	schemaPackage = schemaRoot + "/dbo"
	ordersPath    = "dbo/ORDERS.sql"
	ordersV1      = "CREATE TABLE ORDERS (\n  ID INT NOT NULL,\n  TOTAL DECIMAL\n)\n"
	ordersV2      = "CREATE TABLE ORDERS (\n  ID INT NOT NULL,\n  TOTAL DECIMAL,\n  NOTE VARCHAR(100)\n)\n"
)

func sha256Hex(text string) string {
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:])
}

// schemaColumn is one column of ORDERS as its definition reads in the table's source.
type schemaColumn struct{ name, definition string }

func (column schemaColumn) identity() indexer.Identity {
	return indexer.Identity{ModuleKey: schemaRoot, PackagePath: schemaPackage, Kind: "column", OwnerID: indexer.SymbolID(ordersIdentity), Name: column.name}
}

var ordersIdentity = indexer.Identity{ModuleKey: schemaRoot, PackagePath: schemaPackage, Kind: "table", Name: "ORDERS"}

// declared is a hand-built declaration whose extent is the text at its first occurrence in source, with
// layer hashes derived from its shapes and its extent.
func declared(source string, identity indexer.Identity, identifier uir.Identifier, extent, publicShape, privateShape string) storage.DocumentSymbol {
	GinkgoHelper()
	start := strings.Index(source, extent)
	Expect(start).To(BeNumerically(">=", 0), "extent %q", extent)
	end := start + len(extent)
	line := func(offset int) int { return 1 + strings.Count(source[:offset], "\n") }
	column := func(offset int) int { return offset - strings.LastIndex(source[:offset], "\n") }
	nameStart := start + strings.Index(extent, identity.Name)
	id := indexer.SymbolID(identity)
	return storage.DocumentSymbol{
		ID: &id, Key: identifier.IdentityKey(), Kind: identity.Kind, Visibility: "exported", Shape: privateShape, PublicShape: publicShape,
		PublicHash: sha256Hex("public " + publicShape), PrivateHash: sha256Hex("private " + publicShape + "\n" + privateShape), ContentHash: sha256Hex("content " + identity.Name),
		Name:      storage.Range{line(nameStart), column(nameStart), line(nameStart + len(identity.Name)), column(nameStart + len(identity.Name))},
		NameBytes: storage.ByteSpan{nameStart, nameStart + len(identity.Name)},
		Extent:    storage.Range{line(start), column(start), line(end), column(end)}, ExtentBytes: storage.ByteSpan{start, end},
		Identifier: identifier,
	}
}

// ordersShape is the table's shape: its columns in order, and in the private layer its primary key.
func ordersShape(columns []schemaColumn, private bool) string {
	lines := []string{"table ORDERS"}
	for _, column := range columns {
		lines = append(lines, column.definition)
	}
	if private {
		lines = append(lines, "PRIMARY KEY (ID)")
	}
	return strings.Join(lines, "\n")
}

func schemaPublication(revision, source string, columns []schemaColumn) indexer.Publication {
	GinkgoHelper()
	table := uir.Identifier{Module: schemaRoot, Package: schemaPackage, Type: "ORDERS", NodeType: uir.NodeTypeType}
	symbols := []storage.DocumentSymbol{declared(source, ordersIdentity, table, strings.TrimSuffix(source, "\n"), ordersShape(columns, false), ordersShape(columns, true))}
	published := []indexer.PublishedSymbol{{Identity: ordersIdentity, Visibility: "exported"}}
	for _, column := range columns {
		identifier := uir.Identifier{Module: schemaRoot, Package: schemaPackage, Type: "ORDERS", Field: column.name, NodeType: uir.NodeTypeField}
		symbols = append(symbols, declared(source, column.identity(), identifier, column.definition, column.definition, column.definition))
		published = append(published, indexer.PublishedSymbol{Identity: column.identity(), Visibility: "exported"})
	}
	return indexer.Publication{
		RootKey: schemaRoot, Name: "Schema", Location: indexer.ExternalLocation{URI: "test://schema"}, Revision: revision, Symbols: published,
		Documents: []indexer.PublishedDocument{{
			PathKey: ordersPath, PackagePath: schemaPackage, ContentHash: sha256Hex(source), Source: source,
			Content: storage.DocumentContent{Version: storage.DocumentFormatVersion, PackagePath: schemaPackage, Symbols: symbols,
				Occurrences: []storage.DocumentOccurrence{}, Diagnostics: []storage.DocumentDiagnostic{}},
		}},
	}
}

var _ = Describe("external roots", func() {
	DescribeTable("diffs two published revisions without Git, reading line counts from stored blobs",
		func(ctx SpecContext, options func() storage.DBOptions) {
			database := openDatabase(ctx, options())
			id, total, note := schemaColumn{"ID", "ID INT NOT NULL"}, schemaColumn{"TOTAL", "TOTAL DECIMAL"}, schemaColumn{"NOTE", "NOTE VARCHAR(100)"}
			for _, publication := range []indexer.Publication{
				schemaPublication("schema-v1", ordersV1, []schemaColumn{id, total}),
				schemaPublication("schema-v2", ordersV2, []schemaColumn{id, total, note}),
			} {
				_, err := indexer.Publish(ctx, database, publication)
				Expect(err).ToNot(HaveOccurred())
			}

			result, err := Diff(ctx, database, Options{RootKey: schemaRoot, Revisions: []string{"schema-v1", "schema-v2"}, Visibility: VisibilityAll, Stat: true})
			Expect(err).ToNot(HaveOccurred())
			Expect([]string{result.From.Commit, result.To.Commit, result.From.WorktreeState, result.To.Checkout, result.LinesError}).To(
				Equal([]string{"schema-v1", "schema-v2", "unknown", "", ""}))
			orders := result.file(ordersPath)
			publicBefore, publicAfter := ordersShape([]schemaColumn{id, total}, false), ordersShape([]schemaColumn{id, total, note}, false)
			Expect(orders.Rows).To(Equal([]Row{
				{
					Class: ClassSignature, Kind: "table", Name: "ORDERS", Visibility: "exported", ShapeBefore: publicBefore, ShapeAfter: publicAfter,
					ShapeDiff: DiffShapes(publicBefore, publicAfter, TokenizerText), PathBefore: ordersPath, PathAfter: ordersPath,
					Lines: &LineCount{Added: 2, Removed: 1},
				},
				{Class: ClassAdded, Kind: "column", Owner: "ORDERS", Name: "NOTE", Visibility: "exported", ShapeAfter: note.definition, PathAfter: ordersPath},
			}))
			Expect([]*LineCount{orders.Lines, orders.FileScope}).To(Equal([]*LineCount{{Added: 2, Removed: 1}, {}}))
			Expect(result.Packages).To(HaveLen(1), "an external root has no manifests")

			_, err = Diff(ctx, database, Options{RootKey: schemaRoot, Revisions: []string{"schema-v1", "schema-v2"}, Visibility: VisibilityAll, AutoIndex: true, TaskContext: ctx})
			Expect(err).To(MatchError(And(ContainSubstring("published by an external producer"), ContainSubstring("uir import"))))
			_, err = Diff(ctx, database, Options{RootKey: schemaRoot, Revisions: []string{"schema-v1", "schema-v9"}, Visibility: VisibilityAll})
			Expect(err).To(MatchError(And(ContainSubstring(`"schema-v9"`), ContainSubstring(schemaRoot))))
			_, err = ListHistory(ctx, database, schemaRoot, "", 10)
			Expect(err).To(MatchError(ContainSubstring("published by an external producer")))
		},
		Entry("SQLite", sqliteOptions),
		Entry("PostgreSQL", postgresOptions),
	)
})
