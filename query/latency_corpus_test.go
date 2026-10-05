package query_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"gorm.io/gorm"
)

// The latency corpus has the shape of a producer catalog published with indexer.Publish: an entities
// root declaring the Plan record and its fields, and, per product, a product root and a plan root below
// it (Co/ProductNN and Co/ProductNN/Plan). Every product and plan root holds rule documents, which read
// Plan.SchemeNumber, write a Plan field, and call the rule before them, and screen documents, each
// declaring a screen with its fields. Names are readable and the counts are the knobs.
const (
	corpusEntities = "Co/Entities"
	corpusRecords  = corpusEntities + "/records"
)

var corpusKinds = []indexer.KindDeclaration{
	{Name: "test.rule", Category: symbolhandle.CategoryCallable},
	{Name: "test.screen", Category: symbolhandle.CategoryType},
}

// corpusShape sizes the latency corpus: products of two roots each, and per root its rule and screen
// documents and the fields of each screen; planFields are the Plan record's fields besides
// SchemeNumber. packagePerDocument gives every rule and screen a package of its own, as a business
// rule catalog does, instead of one rules and one screens package per root.
type corpusShape struct {
	products, rules, screens, screenFields, planFields int
	packagePerDocument                                 bool
}

// corpusProduct is the key of the product root of product number.
func corpusProduct(number int) string { return fmt.Sprintf("Co/Product%02d", number) }

// publishCorpus publishes every root of the corpus into database.
func publishCorpus(ctx context.Context, database *gorm.DB, shape corpusShape) error {
	if shape.planFields < 1 {
		return fmt.Errorf("the corpus needs a Plan field besides SchemeNumber for its rules to write, got %d", shape.planFields)
	}
	publications := []indexer.Publication{entitiesPublication(shape)}
	for product := range shape.products {
		publications = append(publications, rootPublication(corpusProduct(product), shape), rootPublication(corpusProduct(product)+"/Plan", shape))
	}
	for _, publication := range publications {
		if _, err := indexer.Publish(ctx, database, publication); err != nil {
			return err
		}
	}
	return nil
}

func planIdentity() indexer.Identity {
	return indexer.Identity{ModuleKey: corpusEntities, PackagePath: corpusRecords, Kind: "record", Name: "Plan"}
}

func planFieldIdentity(name string) indexer.Identity {
	return indexer.Identity{ModuleKey: corpusEntities, PackagePath: corpusRecords, Kind: "field", OwnerID: indexer.SymbolID(planIdentity()), Name: name}
}

func planFieldNames(shape corpusShape) []string {
	names := []string{"SchemeNumber"}
	for field := range shape.planFields {
		names = append(names, fmt.Sprintf("Field%02d", field))
	}
	return names
}

func entitiesPublication(shape corpusShape) indexer.Publication {
	source := &corpusSource{}
	plan := source.open(`<Record name="Plan">`, "Plan")
	identities := []indexer.Identity{planIdentity()}
	var fields []storage.DocumentSymbol
	for _, name := range planFieldNames(shape) {
		identity := planFieldIdentity(name)
		identities = append(identities, identity)
		fields = append(fields, source.leaf(identity, uir.Identifier{Module: corpusEntities, Package: corpusRecords, Type: "Plan", Field: name, NodeType: uir.NodeTypeField}, `  <Field name="`+name+`"/>`, name))
	}
	record := source.close(plan, planIdentity(), uir.Identifier{Module: corpusEntities, Package: corpusRecords, Type: "Plan", NodeType: uir.NodeTypeRecord}, "</Record>")
	return indexer.Publication{
		RootKey: corpusEntities, Name: "Entities", Location: indexer.ExternalLocation{URI: "test://corpus/" + corpusEntities}, Revision: "r1", Kinds: corpusKinds,
		Symbols:   publishedSymbols(identities),
		Documents: []indexer.PublishedDocument{source.document("records/Plan.xml", corpusRecords, append([]storage.DocumentSymbol{record}, fields...), nil)},
	}
}

// rootPublication is one product or plan root: its rule and screen documents.
func rootPublication(root string, shape corpusShape) indexer.Publication {
	identities := []indexer.Identity{planIdentity()}
	for _, name := range planFieldNames(shape) {
		identities = append(identities, planFieldIdentity(name))
	}
	var documents []indexer.PublishedDocument
	for rule := range shape.rules {
		document, named := ruleDocument(root, rule, shape)
		documents, identities = append(documents, document), append(identities, named...)
	}
	for screen := range shape.screens {
		document, named := screenDocument(root, screen, shape)
		documents, identities = append(documents, document), append(identities, named...)
	}
	return indexer.Publication{
		RootKey: root, Name: root[strings.LastIndex(root, "/")+1:], Location: indexer.ExternalLocation{URI: "test://corpus/" + root}, Revision: "r1",
		Kinds: corpusKinds, Symbols: publishedSymbols(identities), Documents: documents,
	}
}

func ruleIdentity(root string, rule int, shape corpusShape) indexer.Identity {
	name := fmt.Sprintf("Rule%03d", rule)
	return indexer.Identity{ModuleKey: root, PackagePath: shape.packageOf(root, "rules", name), Kind: "test.rule", Name: name}
}

// packageOf is the package of a declaration in a root's rules or screens: that directory, or below it
// one package per declaration when the shape asks for it.
func (shape corpusShape) packageOf(root, directory, name string) string {
	if shape.packagePerDocument {
		return root + "/" + directory + "/" + name
	}
	return root + "/" + directory
}

func ruleIdentifier(identity indexer.Identity) uir.Identifier {
	return uir.Identifier{Module: identity.ModuleKey, Package: identity.PackagePath, Method: identity.Name, NodeType: uir.NodeTypeFunction}
}

// ruleDocument is a rule that writes a Plan field, reads Plan.SchemeNumber when its number is even,
// and calls the rule before it, with the symbols it names besides the Plan record and fields.
func ruleDocument(root string, rule int, shape corpusShape) (indexer.PublishedDocument, []indexer.Identity) {
	identity := ruleIdentity(root, rule, shape)
	source := &corpusSource{}
	open := source.open(`<Rule name="`+identity.Name+`">`, identity.Name)
	ruleID := indexer.SymbolID(identity)
	written := planFieldNames(shape)[1+rule%shape.planFields]
	var occurrences []storage.DocumentOccurrence
	if rule%2 == 0 {
		occurrences = append(occurrences, source.use(planFieldIdentity("SchemeNumber"), "reference", `  <Read field="Plan.SchemeNumber"/>`, "Plan.SchemeNumber", ruleID))
	}
	occurrences = append(occurrences, source.use(planFieldIdentity(written), "write", `  <Set field="Plan.`+written+`"/>`, "Plan."+written, ruleID))
	named := []indexer.Identity{identity}
	if rule > 0 {
		callee := ruleIdentity(root, rule-1, shape)
		call := source.use(callee, "call", `  <Call rule="`+callee.Name+`"/>`, callee.Name, ruleID)
		target := ruleIdentifier(callee)
		call.Target, call.StatementPath, call.Resolvable, call.Text = &target, "0", true, `<Call rule="`+callee.Name+`"/>`
		occurrences, named = append(occurrences, call), append(named, callee)
	}
	declaration := source.close(open, identity, ruleIdentifier(identity), "</Rule>")
	return source.document(fmt.Sprintf("rules/%s.xml", identity.Name), identity.PackagePath, []storage.DocumentSymbol{declaration}, occurrences), named
}

// screenDocument is a screen declaring its fields, with the symbols it declares.
func screenDocument(root string, screen int, shape corpusShape) (indexer.PublishedDocument, []indexer.Identity) {
	name := fmt.Sprintf("Screen%03d", screen)
	packagePath := shape.packageOf(root, "screens", name)
	identity := indexer.Identity{ModuleKey: root, PackagePath: packagePath, Kind: "test.screen", Name: name}
	source := &corpusSource{}
	open := source.open(`<Screen name="`+identity.Name+`">`, identity.Name)
	named := []indexer.Identity{identity}
	var fields []storage.DocumentSymbol
	for field := range shape.screenFields {
		name := fmt.Sprintf("Field%02d", field)
		fieldIdentity := indexer.Identity{ModuleKey: root, PackagePath: packagePath, Kind: "field", OwnerID: indexer.SymbolID(identity), Name: name}
		named = append(named, fieldIdentity)
		fields = append(fields, source.leaf(fieldIdentity, uir.Identifier{Module: root, Package: packagePath, Type: identity.Name, Field: name, NodeType: uir.NodeTypeField}, `  <Field name="`+name+`"/>`, name))
	}
	declaration := source.close(open, identity, uir.Identifier{Module: root, Package: packagePath, Type: identity.Name, NodeType: uir.NodeTypeType}, "</Screen>")
	return source.document(fmt.Sprintf("screens/%s.xml", identity.Name), packagePath, append([]storage.DocumentSymbol{declaration}, fields...), nil), named
}

func publishedSymbols(identities []indexer.Identity) []indexer.PublishedSymbol {
	seen := map[string]bool{}
	var symbols []indexer.PublishedSymbol
	for _, identity := range identities {
		if id := indexer.SymbolID(identity); !seen[id] {
			seen[id] = true
			symbols = append(symbols, indexer.PublishedSymbol{Identity: identity, Visibility: "exported"})
		}
	}
	return symbols
}

// corpusSource writes a source one line at a time and positions what it declares and uses on them.
type corpusSource struct {
	text strings.Builder
	line int
}

// opened is a declaration whose first line is written and whose extent is still open.
type opened struct {
	line, offset int
	nameRange    storage.Range
	nameBytes    storage.ByteSpan
}

// write appends one line and returns its number and starting byte offset.
func (source *corpusSource) write(text string) (int, int) {
	offset := source.text.Len()
	source.text.WriteString(text + "\n")
	source.line++
	return source.line, offset
}

// at is the range and bytes of the first occurrence of word in a line written at line and offset.
func at(line, offset int, text, word string) (storage.Range, storage.ByteSpan) {
	column := strings.Index(text, word)
	if column < 0 {
		panic(fmt.Sprintf("corpus line %q does not contain %q", text, word))
	}
	return storage.Range{line, column + 1, line, column + 1 + len(word)}, storage.ByteSpan{offset + column, offset + column + len(word)}
}

func (source *corpusSource) open(text, name string) opened {
	line, offset := source.write(text)
	nameRange, nameBytes := at(line, offset, text, name)
	return opened{line: line, offset: offset, nameRange: nameRange, nameBytes: nameBytes}
}

// close writes the declaration's last line and returns its entry, extending over every line since open.
func (source *corpusSource) close(start opened, identity indexer.Identity, identifier uir.Identifier, text string) storage.DocumentSymbol {
	line, offset := source.write(text)
	extent := storage.Range{start.line, 1, line, len(text) + 1}
	return declared(identity, identifier, start.nameRange, start.nameBytes, extent, storage.ByteSpan{start.offset, offset + len(text)})
}

// leaf writes a one-line declaration.
func (source *corpusSource) leaf(identity indexer.Identity, identifier uir.Identifier, text, name string) storage.DocumentSymbol {
	start := source.open(text, name)
	extent := storage.Range{start.line, 1, start.line, len(text) + 1}
	return declared(identity, identifier, start.nameRange, start.nameBytes, extent, storage.ByteSpan{start.offset, start.offset + len(text)})
}

// use writes a line with one occurrence of symbol at word, enclosed by the declaration enclosing.
func (source *corpusSource) use(symbol indexer.Identity, role, text, word, enclosing string) storage.DocumentOccurrence {
	line, offset := source.write(text)
	occurrenceRange, occurrenceBytes := at(line, offset, text, word)
	id := indexer.SymbolID(symbol)
	return storage.DocumentOccurrence{Symbol: &id, Role: role, Range: occurrenceRange, Bytes: occurrenceBytes, Enclosing: &enclosing}
}

func (source *corpusSource) document(pathKey, packagePath string, symbols []storage.DocumentSymbol, occurrences []storage.DocumentOccurrence) indexer.PublishedDocument {
	text := source.text.String()
	return indexer.PublishedDocument{
		PathKey: pathKey, PackagePath: packagePath, ContentHash: sha256Hex(text), Source: text,
		Content: storage.DocumentContent{
			Version: storage.DocumentFormatVersion, PackagePath: packagePath, Symbols: symbols,
			Occurrences: append([]storage.DocumentOccurrence{}, occurrences...), Diagnostics: []storage.DocumentDiagnostic{},
		},
	}
}

func declared(identity indexer.Identity, identifier uir.Identifier, nameRange storage.Range, nameBytes storage.ByteSpan, extent storage.Range, extentBytes storage.ByteSpan) storage.DocumentSymbol {
	id := indexer.SymbolID(identity)
	shape := identity.Kind + " " + identity.Name
	return storage.DocumentSymbol{
		ID: &id, Key: identifier.IdentityKey(), Kind: identity.Kind, Visibility: "exported", Shape: shape, ShapeHash: sha256Hex(shape),
		BodyHash: sha256Hex(identity.ModuleKey + " " + shape), Name: nameRange, NameBytes: nameBytes, Extent: extent, ExtentBytes: extentBytes,
		Identifier: identifier,
	}
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
