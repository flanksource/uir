package lint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/sqlglot-go/database/migrate/dialect"
	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/sqlglot-go/migrate/engine"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer"
	uirmigrate "github.com/flanksource/uir/sql/migrate"
)

// engineDialects names the sqlglot dialect that reads HCL for each source
// dialect. Only SQL Server catalogues are modelled by sql/migrate, which the
// HCL producer builds on.
var engineDialects = map[string]string{"tsql": "sqlserver"}

// addHCL declares the tables, columns, indexes and foreign keys that HCL
// sources describe as symbols of the publication. A table takes the module
// key, package (its schema) and name the SQL producer gives a table a script
// reads or writes, so those references resolve to the declaration. A source
// file declares one schema, since a document has one package.
func (pub *sqlPublication) addHCL(sources []mappedSource) error {
	var documents []indexer.NodeDocument
	declared := map[string]mappedSource{}
	for _, source := range sources {
		document, err := hclDocument(source)
		if err != nil {
			return fmt.Errorf("%s: %w", source.Filename, err)
		}
		if len(document.Nodes) > 0 {
			documents = append(documents, document)
			declared[document.PathKey] = source
		}
	}
	if len(documents) == 0 {
		return nil
	}
	published, err := indexer.PublicationFromNodes(indexer.NodePublication{RootKey: pub.RootKey, Name: pub.Name, Location: pub.Location, Documents: documents})
	if err != nil {
		return err
	}
	for _, symbol := range published.Symbols {
		pub.symbols[indexer.SymbolID(symbol.Identity)] = symbol
	}
	for _, document := range published.Documents {
		source := declared[document.PathKey]
		pub.files[document.PathKey] = mappedSource{Filename: source.Inputs[0], Inputs: source.Inputs, Content: document.Source}
		pub.Documents = append(pub.Documents, document)
	}
	return nil
}

// hclDocument evaluates one HCL source to its declared tables, as the nodes of
// one document.
func hclDocument(source mappedSource) (indexer.NodeDocument, error) {
	engineDialect, ok := engineDialects[source.Dialect]
	if !ok {
		return indexer.NodeDocument{}, fmt.Errorf("HCL sources need the tsql dialect, got %q", source.Dialect)
	}
	schema, err := dialect.DesiredFromHCL(engineDialect, source.Content)
	if err != nil {
		return indexer.NodeDocument{}, err
	}
	schema, schemas := normalizeTables(schema, source.Dialect)
	if len(schema.Tables) == 0 {
		return indexer.NodeDocument{PathKey: source.Filename}, nil
	}
	if len(schemas) != 1 {
		return indexer.NodeDocument{}, fmt.Errorf("a source file declares one schema, but this one declares tables in %s", strings.Join(schemas, ", "))
	}
	catalogue, err := uirmigrate.Enrich(uir.UIR{}, schema, engine.Scope{Mode: engine.ScopeDeclared, Schemas: schemas})
	if err != nil {
		return indexer.NodeDocument{}, err
	}
	if len(catalogue.Functions) > 0 || len(catalogue.SQLCatalog.TableTypes)+len(catalogue.SQLCatalog.Sequences) > 0 {
		return indexer.NodeDocument{}, fmt.Errorf("HCL sources publish tables, columns, indexes and foreign keys only")
	}
	return indexer.NodeDocument{PathKey: source.Filename, PackagePath: schemas[0], Nodes: uir.NodesOf(catalogue.Tables)}, nil
}

// normalizeTables names each table and each table a foreign key references as
// the SQL producer names a table a script touches, and returns the schemas the
// tables are in.
func normalizeTables(schema model.Schema, dialect string) (model.Schema, []string) {
	var schemas []string
	for i := range schema.Tables {
		table := &schema.Tables[i]
		table.Name = normalizedTable(dialect, table.Name)
		schemas = append(schemas, table.Name.Schema)
		for j := range table.ForeignKeys {
			table.ForeignKeys[j].References = normalizedTable(dialect, table.ForeignKeys[j].References)
		}
	}
	slices.Sort(schemas)
	return schema, slices.Compact(schemas)
}

func normalizedTable(dialect string, name model.Name) model.Name {
	return model.Name{Schema: normalizedIdentifier(dialect, name.Schema), Name: normalizedIdentifier(dialect, name.Name)}
}

// normalizedIdentifier is how the SQL producer names an object it reads: SQL
// Server and SQLite names are case-insensitive, so they are lower-cased.
func normalizedIdentifier(dialect, name string) string {
	if dialect == "tsql" || dialect == "sqlite" {
		return strings.ToLower(name)
	}
	return name
}
