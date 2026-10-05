package query_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// The insurance fixture is one producer-published root of rule, record, screen and database sources:
// the custom callable kinds acme.rule and acme.procedure, the custom type kind acme.screen, the builtin
// record/field and table/column kinds, and the rule Apply, which calls the procedure Calc, reads the
// field Policy.Amount and the table PREMIUM, while Calc writes Policy.Amount and the column
// PREMIUM.AMOUNT. The rule Quote includes the screen PolicyScreen, a call of a type, and reads it.
const (
	insRoot      = "Acme/Ins"
	insRules     = insRoot + "/rules"
	insRecords   = insRoot + "/records"
	insScreens   = insRoot + "/screens"
	insDatabase  = insRoot + "/Database"
	applyPath    = "rules/Apply.xml"
	calcPath     = "rules/Calc.xml"
	quotePath    = "rules/Quote.xml"
	applySource  = "<Rule name=\"Apply\">\n  <Call rule=\"Calc\"/>\n  <Read field=\"Policy.Amount\"/>\n  <Query table=\"PREMIUM\"/>\n</Rule>\n"
	calcSource   = "<Procedure name=\"Calc\">\n  <Set field=\"Policy.Amount\"/>\n  <Update column=\"PREMIUM.AMOUNT\"/>\n</Procedure>\n"
	quoteSource  = "<Rule name=\"Quote\">\n  <Include screen=\"PolicyScreen\"/>\n  <Show screen=\"PolicyScreen\"/>\n</Rule>\n"
	screenSource = "<Screen name=\"PolicyScreen\">\n</Screen>\n"
	policySource = "<Record name=\"Policy\">\n  <Field name=\"Amount\"/>\n</Record>\n"
	tableSource  = "CREATE TABLE PREMIUM (\n  AMOUNT DECIMAL\n)\n"
)

// applyGUID is the producer's id of the Apply rule, carried on its declaration's identifier.
var applyGUID = uuid.MustParse("6f1c2a4e-9b3d-4c8e-a1f2-3d4e5f6a7b8c")

// policyPayload is the producer's payload on the Policy record's declaration.
const policyPayload = `{"fieldCount":1}`

// insIdentities are the fixture's symbols by name.
type insIdentities struct {
	apply, calc, quote, screen, policy, amount, premium, premiumAmount indexer.Identity
}

func insuranceIdentities() insIdentities {
	policy := indexer.Identity{ModuleKey: insRoot, PackagePath: insRecords, Kind: "record", Name: "Policy"}
	premium := indexer.Identity{ModuleKey: insRoot, PackagePath: insDatabase, Kind: "table", Name: "PREMIUM"}
	return insIdentities{
		apply:         indexer.Identity{ModuleKey: insRoot, PackagePath: insRules, Kind: "acme.rule", Name: "Apply"},
		calc:          indexer.Identity{ModuleKey: insRoot, PackagePath: insRules, Kind: "acme.procedure", Name: "Calc"},
		quote:         indexer.Identity{ModuleKey: insRoot, PackagePath: insRules, Kind: "acme.rule", Name: "Quote"},
		screen:        indexer.Identity{ModuleKey: insRoot, PackagePath: insScreens, Kind: "acme.screen", Name: "PolicyScreen"},
		policy:        policy,
		amount:        indexer.Identity{ModuleKey: insRoot, PackagePath: insRecords, Kind: "field", OwnerID: indexer.SymbolID(policy), Name: "Amount"},
		premium:       premium,
		premiumAmount: indexer.Identity{ModuleKey: insRoot, PackagePath: insDatabase, Kind: "column", OwnerID: indexer.SymbolID(premium), Name: "AMOUNT"},
	}
}

// publishedText locates declarations and occurrences in one published source.
type publishedText string

func (source publishedText) span(context, text string) (storage.Range, storage.ByteSpan) {
	GinkgoHelper()
	start := strings.Index(string(source), context)
	Expect(start).To(BeNumerically(">=", 0), "context %q", context)
	offset := strings.Index(context, text)
	Expect(offset).To(BeNumerically(">=", 0), "text %q in %q", text, context)
	begin, end := start+offset, start+offset+len(text)
	return storage.Range{source.line(begin), source.column(begin), source.line(end), source.column(end)}, storage.ByteSpan{begin, end}
}

func (source publishedText) line(offset int) int {
	return 1 + strings.Count(string(source[:offset]), "\n")
}

func (source publishedText) column(offset int) int {
	return offset - strings.LastIndex(string(source[:offset]), "\n")
}

func sha256Text(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func (source publishedText) declaration(identity indexer.Identity, identifier uir.Identifier, extent, name string) storage.DocumentSymbol {
	GinkgoHelper()
	id, shape := indexer.SymbolID(identity), identity.Kind+" "+identity.Name
	nameRange, nameBytes := source.span(extent, name)
	extentRange, extentBytes := source.span(extent, extent)
	return storage.DocumentSymbol{
		ID: &id, Key: identifier.IdentityKey(), Kind: identity.Kind, Visibility: "exported", Shape: shape,
		ShapeHash: sha256Text(shape), BodyHash: sha256Text(extent), Name: nameRange, NameBytes: nameBytes,
		Extent: extentRange, ExtentBytes: extentBytes, Identifier: identifier,
	}
}

// use is a read or write of symbol at text inside context, enclosed by enclosing.
func (source publishedText) use(symbol indexer.Identity, role, context, text string, enclosing indexer.Identity) storage.DocumentOccurrence {
	GinkgoHelper()
	id, enclosingID := indexer.SymbolID(symbol), indexer.SymbolID(enclosing)
	occurrenceRange, occurrenceBytes := source.span(context, text)
	return storage.DocumentOccurrence{Symbol: &id, Role: role, Range: occurrenceRange, Bytes: occurrenceBytes, Enclosing: &enclosingID}
}

// call is a call of callee at text inside context from caller, carrying the callee as its target and
// the element as its text, as a producer records a call.
func (source publishedText) call(callee, caller indexer.Identity, identifiers map[string]uir.Identifier, context, text string) storage.DocumentOccurrence {
	GinkgoHelper()
	occurrence := source.use(callee, "call", context, text, caller)
	target := identifiers[callee.Name]
	occurrence.Target, occurrence.Resolvable, occurrence.Text = &target, true, context
	occurrence.EnclosingKey, occurrence.StatementPath = identifiers[caller.Name].IdentityKey(), "0"
	return occurrence
}

func (source publishedText) document(pathKey, packagePath string, symbols []storage.DocumentSymbol, occurrences []storage.DocumentOccurrence) indexer.PublishedDocument {
	return indexer.PublishedDocument{
		PathKey: pathKey, PackagePath: packagePath, ContentHash: sha256Text(string(source)), Source: string(source),
		Content: storage.DocumentContent{
			Version: storage.DocumentFormatVersion, PackagePath: packagePath, Symbols: symbols,
			Occurrences: append([]storage.DocumentOccurrence{}, occurrences...), Diagnostics: []storage.DocumentDiagnostic{},
		},
	}
}

// insuranceIdentifiers are the identifiers of the fixture's declarations by symbol name.
func insuranceIdentifiers() map[string]uir.Identifier {
	return map[string]uir.Identifier{
		"Apply":        {Module: insRoot, Package: insRules, Method: "Apply", NodeType: uir.NodeTypeFunction, Id: &applyGUID},
		"Calc":         {Module: insRoot, Package: insRules, Method: "Calc", NodeType: uir.NodeTypeFunction},
		"Quote":        {Module: insRoot, Package: insRules, Method: "Quote", NodeType: uir.NodeTypeFunction},
		"PolicyScreen": {Module: insRoot, Package: insScreens, Type: "PolicyScreen", NodeType: uir.NodeTypeType},
		"Policy":       {Module: insRoot, Package: insRecords, Type: "Policy", NodeType: uir.NodeTypeRecord},
		"Amount":       {Module: insRoot, Package: insRecords, Type: "Policy", Field: "Amount", NodeType: uir.NodeTypeField},
		"PREMIUM":      {Module: insRoot, Package: insDatabase, Type: "PREMIUM", NodeType: uir.NodeTypeType},
		"AMOUNT":       {Module: insRoot, Package: insDatabase, Type: "PREMIUM", Field: "AMOUNT", NodeType: uir.NodeTypeField},
	}
}

func insurancePublication() indexer.Publication {
	GinkgoHelper()
	ids := insuranceIdentities()
	identifiers := insuranceIdentifiers()
	apply, calc, policy, table := publishedText(applySource), publishedText(calcSource), publishedText(policySource), publishedText(tableSource)
	quote, screen := publishedText(quoteSource), publishedText(screenSource)
	record := policy.declaration(ids.policy, identifiers["Policy"], strings.TrimSuffix(policySource, "\n"), "Policy")
	record.Payload = storage.JSON(policyPayload)
	symbols := []indexer.PublishedSymbol{}
	for _, identity := range []indexer.Identity{ids.apply, ids.calc, ids.quote, ids.screen, ids.policy, ids.amount, ids.premium, ids.premiumAmount} {
		symbols = append(symbols, indexer.PublishedSymbol{Identity: identity, Visibility: "exported"})
	}
	return indexer.Publication{
		RootKey: insRoot, Name: "Ins", Location: indexer.ExternalLocation{URI: "test://lab/Acme/Ins"}, Revision: "r1",
		Kinds: []indexer.KindDeclaration{
			{Name: "acme.rule", Category: symbolhandle.CategoryCallable}, {Name: "acme.procedure", Category: symbolhandle.CategoryCallable},
			{Name: "acme.screen", Category: symbolhandle.CategoryType},
		},
		Symbols: symbols,
		Documents: []indexer.PublishedDocument{
			quote.document(quotePath, insRules, []storage.DocumentSymbol{
				quote.declaration(ids.quote, identifiers["Quote"], strings.TrimSuffix(quoteSource, "\n"), "Quote"),
			}, []storage.DocumentOccurrence{
				quote.call(ids.screen, ids.quote, identifiers, `<Include screen="PolicyScreen"/>`, "PolicyScreen"),
				quote.use(ids.screen, "reference", `<Show screen="PolicyScreen"/>`, "PolicyScreen", ids.quote),
			}),
			screen.document("screens/PolicyScreen.xml", insScreens, []storage.DocumentSymbol{
				screen.declaration(ids.screen, identifiers["PolicyScreen"], strings.TrimSuffix(screenSource, "\n"), "PolicyScreen"),
			}, nil),
			apply.document(applyPath, insRules, []storage.DocumentSymbol{
				apply.declaration(ids.apply, identifiers["Apply"], strings.TrimSuffix(applySource, "\n"), "Apply"),
			}, []storage.DocumentOccurrence{
				apply.call(ids.calc, ids.apply, identifiers, `<Call rule="Calc"/>`, "Calc"),
				apply.use(ids.amount, "reference", `<Read field="Policy.Amount"/>`, "Policy.Amount", ids.apply),
				apply.use(ids.premium, "reference", `<Query table="PREMIUM"/>`, "PREMIUM", ids.apply),
			}),
			calc.document(calcPath, insRules, []storage.DocumentSymbol{
				calc.declaration(ids.calc, identifiers["Calc"], strings.TrimSuffix(calcSource, "\n"), "Calc"),
			}, []storage.DocumentOccurrence{
				calc.use(ids.amount, "write", `<Set field="Policy.Amount"/>`, "Policy.Amount", ids.calc),
				calc.use(ids.premiumAmount, "write", `<Update column="PREMIUM.AMOUNT"/>`, "PREMIUM.AMOUNT", ids.calc),
			}),
			policy.document("records/Policy.xml", insRecords, []storage.DocumentSymbol{
				record,
				policy.declaration(ids.amount, identifiers["Amount"], `<Field name="Amount"/>`, "Amount"),
			}, nil),
			table.document("Database/PREMIUM.sql", insDatabase, []storage.DocumentSymbol{
				table.declaration(ids.premium, identifiers["PREMIUM"], strings.TrimSuffix(tableSource, "\n"), "PREMIUM"),
				table.declaration(ids.premiumAmount, identifiers["AMOUNT"], "AMOUNT DECIMAL", "AMOUNT"),
			}, nil),
		},
	}
}

// insurancePipeline publishes the insurance root and returns a pipeline over it with its scope.
func insurancePipeline(ctx context.Context, database *gorm.DB) (*query.Pipeline, query.ModuleScopeOptions) {
	GinkgoHelper()
	_, err := indexer.Publish(ctx, database, insurancePublication())
	Expect(err).ToNot(HaveOccurred())
	pipeline, err := query.NewPipeline(database)
	Expect(err).ToNot(HaveOccurred())
	return pipeline, query.ModuleScopeOptions{RootKey: insRoot}
}
