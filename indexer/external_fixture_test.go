package indexer

import (
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The external fixture is two producer-published roots, Co/Prod and Co/Prod/PlanA, of rule, screen,
// and record sources: the custom kinds test.rule (callable) and test.screen (type), the builtin record
// and field kinds, and a call, a reference, and writes, one of them across the roots. It is also the
// docs example (docs/examples/import), which a spec keeps equal to it.
const (
	prodRoot  = "Co/Prod"
	planARoot = "Co/Prod/PlanA"
	prodURI   = "test://lab/Co/Prod"
	planAURI  = "test://lab/Co/Prod/PlanA"
)

var externalKinds = []KindDeclaration{{Name: "test.rule", Category: symbolhandle.CategoryCallable}, {Name: "test.screen", Category: symbolhandle.CategoryType}}

// externalIdentities are the fixture's symbols by name.
type externalIdentities struct{ policy, amount, calc, entry, premium, apply Identity }

func fixtureIdentities() externalIdentities {
	policy := Identity{ModuleKey: prodRoot, PackagePath: prodRoot + "/records", Kind: "record", Name: "Policy"}
	entry := Identity{ModuleKey: planARoot, PackagePath: planARoot + "/screens", Kind: "test.screen", Name: "Entry"}
	return externalIdentities{
		policy:  policy,
		amount:  Identity{ModuleKey: prodRoot, PackagePath: policy.PackagePath, Kind: "field", OwnerID: SymbolID(policy), Name: "Amount"},
		calc:    Identity{ModuleKey: prodRoot, PackagePath: prodRoot + "/rules", Kind: "test.rule", Name: "Calc"},
		entry:   entry,
		premium: Identity{ModuleKey: planARoot, PackagePath: entry.PackagePath, Kind: "field", OwnerID: SymbolID(entry), Name: "Premium"},
		apply:   Identity{ModuleKey: planARoot, PackagePath: planARoot + "/rules", Kind: "test.rule", Name: "Apply"},
	}
}

// sourceText locates the declarations and occurrences of one published document in its source.
type sourceText string

// span is the range and bytes of text inside the first occurrence of context.
func (source sourceText) span(context, text string) (storage.Range, storage.ByteSpan) {
	GinkgoHelper()
	start := strings.Index(string(source), context)
	Expect(start).To(BeNumerically(">=", 0), "context %q", context)
	offset := strings.Index(context, text)
	Expect(offset).To(BeNumerically(">=", 0), "text %q in %q", text, context)
	begin, end := start+offset, start+offset+len(text)
	return storage.Range{source.line(begin), source.column(begin), source.line(end), source.column(end)}, storage.ByteSpan{begin, end}
}

func (source sourceText) line(offset int) int {
	return 1 + strings.Count(string(source[:offset]), "\n")
}

func (source sourceText) column(offset int) int {
	return offset - strings.LastIndex(string(source[:offset]), "\n")
}

// declaration is one declared symbol: its identifier, the text of its extent, and its name in it.
func (source sourceText) declaration(identity Identity, identifier uir.Identifier, extent, name string) storage.DocumentSymbol {
	GinkgoHelper()
	id := SymbolID(identity)
	shape := identity.Kind + " " + identity.Name
	nameRange, nameBytes := source.span(extent, name)
	extentRange, extentBytes := source.span(extent, extent)
	return storage.DocumentSymbol{
		ID: &id, Key: identifier.IdentityKey(), Kind: identity.Kind, Visibility: "exported", Shape: shape,
		ShapeHash: hashBytes([]byte(shape)), BodyHash: hashBytes([]byte(extent)), Name: nameRange, NameBytes: nameBytes,
		Extent: extentRange, ExtentBytes: extentBytes, Identifier: identifier,
	}
}

// occurrence is a use of symbol at text inside context, enclosed by the declaration enclosing.
func (source sourceText) occurrence(symbol Identity, role string, context, text string, enclosing Identity) storage.DocumentOccurrence {
	GinkgoHelper()
	id, enclosingID := SymbolID(symbol), SymbolID(enclosing)
	occurrenceRange, occurrenceBytes := source.span(context, text)
	return storage.DocumentOccurrence{Symbol: &id, Role: role, Range: occurrenceRange, Bytes: occurrenceBytes, Enclosing: &enclosingID}
}

func (source sourceText) document(pathKey, packagePath string, symbols []storage.DocumentSymbol, occurrences []storage.DocumentOccurrence) PublishedDocument {
	return PublishedDocument{
		PathKey: pathKey, PackagePath: packagePath, ContentHash: hashBytes([]byte(source)), Source: string(source),
		Content: storage.DocumentContent{
			Version: storage.DocumentFormatVersion, PackagePath: packagePath, Symbols: symbols,
			Occurrences: append([]storage.DocumentOccurrence{}, occurrences...), Diagnostics: []storage.DocumentDiagnostic{},
		},
	}
}

func published(identities ...Identity) []PublishedSymbol {
	symbols := make([]PublishedSymbol, len(identities))
	for i, identity := range identities {
		symbols[i] = PublishedSymbol{Identity: identity, Visibility: "exported"}
	}
	return symbols
}

// prodPublication is root Co/Prod at revision, whose Calc rule sets Policy.Amount to calcValue.
func prodPublication(revision, calcValue string) Publication {
	GinkgoHelper()
	ids := fixtureIdentities()
	records := sourceText("<Record name=\"Policy\">\n  <Field name=\"Amount\"/>\n</Record>\n")
	recordExtent := strings.TrimSuffix(string(records), "\n")
	rules := sourceText("<Rule name=\"Calc\">\n  <Set field=\"Policy.Amount\" value=\"" + calcValue + "\"/>\n</Rule>\n")
	return Publication{
		RootKey: prodRoot, Name: "Prod", Location: ExternalLocation{URI: prodURI}, Revision: revision, Kinds: externalKinds,
		Symbols: published(ids.policy, ids.amount, ids.calc),
		Documents: []PublishedDocument{
			records.document("records/Policy.xml", ids.policy.PackagePath, []storage.DocumentSymbol{
				records.declaration(ids.policy, uir.Identifier{Module: prodRoot, Package: ids.policy.PackagePath, Type: "Policy", NodeType: uir.NodeTypeRecord}, recordExtent, "Policy"),
				records.declaration(ids.amount, uir.Identifier{Module: prodRoot, Package: ids.policy.PackagePath, Type: "Policy", Field: "Amount", NodeType: uir.NodeTypeField}, `<Field name="Amount"/>`, "Amount"),
			}, nil),
			rules.document("rules/Calc.xml", ids.calc.PackagePath, []storage.DocumentSymbol{
				rules.declaration(ids.calc, uir.Identifier{Module: prodRoot, Package: ids.calc.PackagePath, Method: "Calc", NodeType: uir.NodeTypeFunction}, strings.TrimSuffix(string(rules), "\n"), "Calc"),
			}, []storage.DocumentOccurrence{rules.occurrence(ids.amount, "write", `field="Policy.Amount"`, "Policy.Amount", ids.calc)}),
		},
	}
}

// planAPublication is root Co/Prod/PlanA: its Apply rule calls Co/Prod's Calc, reads Policy.Amount, and
// writes its own screen's Premium field.
func planAPublication() Publication {
	GinkgoHelper()
	ids := fixtureIdentities()
	screens := sourceText("<Screen name=\"Entry\">\n  <Field name=\"Premium\"/>\n</Screen>\n")
	rules := sourceText("<Rule name=\"Apply\">\n  <Call rule=\"Calc\"/>\n  <Read field=\"Policy.Amount\"/>\n  <Set field=\"Entry.Premium\" value=\"Policy.Amount\"/>\n</Rule>\n")
	apply := uir.Identifier{Module: planARoot, Package: ids.apply.PackagePath, Method: "Apply", NodeType: uir.NodeTypeFunction}
	call := rules.occurrence(ids.calc, "call", `<Call rule="Calc"/>`, "Calc", ids.apply)
	call.EnclosingKey, call.StatementPath, call.Resolvable, call.Text = apply.IdentityKey(), "0", true, `<Call rule="Calc"/>`
	call.Target = &uir.Identifier{Module: prodRoot, Package: ids.calc.PackagePath, Method: "Calc", NodeType: uir.NodeTypeFunction}
	return Publication{
		RootKey: planARoot, Name: "PlanA", Location: ExternalLocation{URI: planAURI}, Revision: "r1", Kinds: externalKinds,
		Symbols: published(ids.entry, ids.premium, ids.apply, ids.calc, ids.policy, ids.amount),
		Documents: []PublishedDocument{
			rules.document("rules/Apply.xml", ids.apply.PackagePath, []storage.DocumentSymbol{
				rules.declaration(ids.apply, apply, strings.TrimSuffix(string(rules), "\n"), "Apply"),
			}, []storage.DocumentOccurrence{
				call,
				rules.occurrence(ids.amount, "reference", `<Read field="Policy.Amount"/>`, "Policy.Amount", ids.apply),
				rules.occurrence(ids.premium, "write", `field="Entry.Premium"`, "Entry.Premium", ids.apply),
			}),
			screens.document("screens/Entry.xml", ids.entry.PackagePath, []storage.DocumentSymbol{
				screens.declaration(ids.entry, uir.Identifier{Module: planARoot, Package: ids.entry.PackagePath, Type: "Entry", NodeType: uir.NodeTypeType}, strings.TrimSuffix(string(screens), "\n"), "Entry"),
				screens.declaration(ids.premium, uir.Identifier{Module: planARoot, Package: ids.entry.PackagePath, Type: "Entry", Field: "Premium", NodeType: uir.NodeTypeField}, `<Field name="Premium"/>`, "Premium"),
			}, nil),
		},
	}
}
