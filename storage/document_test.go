package storage_test

import (
	"encoding/json"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const workerPackage = "example.org/acme/worker"

func documentSymbol(identifier uir.Identifier, kind string, name storage.ByteSpan, extent storage.ByteSpan) storage.DocumentSymbol {
	return storage.DocumentSymbol{
		Key: identifier.IdentityKey(), Kind: kind, Visibility: "exported", Shape: kind + " shape", BodyHash: digest(kind),
		Name: storage.Range{3, 1, 3, 5}, NameBytes: name, Extent: storage.Range{2, 1, 9, 2}, ExtentBytes: extent,
		Identifier: identifier, ChildSlot: kind + "s", Payload: storage.JSON(`{}`), SemanticHash: digest("semantic " + kind),
	}
}

// workerDocument is a valid syntax document: a type with a nested field, a method, and one call inside the method.
func workerDocument() (storage.Document, storage.SourceRevision, storage.DocumentContent) {
	typeID := uir.Identifier{Package: workerPackage, Type: "Service", NodeType: uir.NodeTypeType}
	fieldID := uir.Identifier{Package: workerPackage, Type: "Service", Field: "Name", NodeType: uir.NodeTypeField}
	methodID := uir.Identifier{Package: workerPackage, Type: "Service", Method: "Run", Signature: "()", NodeType: uir.NodeTypeMethod}
	target := uir.Identifier{Package: workerPackage, Method: "Stop", NodeType: uir.NodeTypeMethod}
	content := storage.DocumentContent{
		Version: storage.DocumentFormatVersion, PackagePath: workerPackage,
		Symbols: []storage.DocumentSymbol{
			documentSymbol(typeID, "type", storage.ByteSpan{30, 37}, storage.ByteSpan{25, 60}),
			documentSymbol(fieldID, "field", storage.ByteSpan{40, 44}, storage.ByteSpan{40, 51}),
			documentSymbol(methodID, "method", storage.ByteSpan{80, 83}, storage.ByteSpan{62, 99}),
		},
		Occurrences: []storage.DocumentOccurrence{{
			Role: "call", Range: storage.Range{10, 2, 10, 6}, Bytes: storage.ByteSpan{88, 92},
			EnclosingKey: methodID.IdentityKey(), Target: &target, StatementPath: "calls/000000", Text: "Stop",
		}},
		Diagnostics: []storage.DocumentDiagnostic{},
	}
	source := storage.SourceRevision{ID: uuid.New(), RootID: uuid.New(), PathKey: "worker/service.go", ContentHash: digest("source"), PackagePath: workerPackage}
	document := storage.Document{
		ID: uuid.New(), RootID: source.RootID, PathKey: source.PathKey, SourceRevisionID: source.ID, PackagePath: workerPackage,
		InputHash: digest("input"), IndexerVersion: "test", Coverage: storage.CoverageSyntax, SymbolCount: 3, OccurrenceCount: 1,
	}
	return document, source, content
}

// typedWorkerDocument turns the syntax fixture into a valid indexed document: every declaration has
// an id and shape hash, the call names its symbol and enclosing method, and the type implements an
// interface declared elsewhere.
func typedWorkerDocument() (storage.Document, storage.SourceRevision, storage.DocumentContent) {
	document, source, content := workerDocument()
	document.Coverage = storage.CoverageIndexed
	for index := range content.Symbols {
		id := digest("symbol " + content.Symbols[index].Kind)
		content.Symbols[index].ID, content.Symbols[index].ShapeHash = &id, digest("shape "+content.Symbols[index].Kind)
	}
	content.Symbols[0].TypeForm = "struct"
	content.Symbols[0].Shape = "type Service struct{}"
	content.Symbols[0].Implements = []string{digest("interface")}
	target, method := digest("Stop"), *content.Symbols[2].ID
	content.Occurrences[0].Symbol, content.Occurrences[0].Enclosing = &target, &method
	content.Occurrences = append(content.Occurrences, storage.DocumentOccurrence{
		Role: "reference", Range: storage.Range{10, 8, 10, 9}, Bytes: storage.ByteSpan{94, 95}, Enclosing: &method, Note: "local binding",
	})
	document.OccurrenceCount = 2
	return document, source, content
}

func encodeDocument(document storage.Document, content storage.DocumentContent) storage.Document {
	encoded, err := json.Marshal(content)
	Expect(err).ToNot(HaveOccurred())
	document.Content = storage.JSON(encoded)
	return document
}

var _ = Describe("DecodeDocument", func() {
	DescribeTable("classifies immutable v1 type shapes", func(shape, expected string) {
		id := digest("legacy type")
		form, err := storage.TypeForm(1, storage.DocumentSymbol{ID: &id, Kind: "type", Key: "legacy", Shape: shape})
		Expect(err).ToNot(HaveOccurred())
		Expect(form).To(Equal(expected))
	},
		Entry("empty struct", "type Entity struct{}", "struct"),
		Entry("interface", "type Entity interface{ M() }", "interface"),
		Entry("alias", "type Entity = struct{}", "alias"),
		Entry("other defined type", "type Entity int", "other"),
	)
	It("rejects a malformed immutable v1 type shape", func() {
		id := digest("legacy type")
		_, err := storage.TypeForm(1, storage.DocumentSymbol{ID: &id, Kind: "type", Key: "legacy", Shape: "type Entity ???"})
		Expect(err).To(HaveOccurred())
	})
	It("round-trips a valid syntax document", func() {
		document, source, content := workerDocument()
		decoded, err := storage.DecodeDocument(encodeDocument(document, content), source)
		Expect(err).ToNot(HaveOccurred())
		Expect(decoded).To(Equal(content))
	})

	DescribeTable("rejects a document that does not describe its source or violates the format",
		func(mutate func(*storage.Document, *storage.DocumentContent), message string) {
			document, source, content := workerDocument()
			mutate(&document, &content)
			_, err := storage.DecodeDocument(encodeDocument(document, content), source)
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("a stale symbol_count", func(document *storage.Document, _ *storage.DocumentContent) { document.SymbolCount = 2 }, "symbol_count 2, content has 3 symbols"),
		Entry("a stale occurrence_count", func(document *storage.Document, _ *storage.DocumentContent) { document.OccurrenceCount = 0 }, "occurrence_count 0, content has 1 occurrences"),
		Entry("a syntax document labelled indexed", func(document *storage.Document, _ *storage.DocumentContent) {
			document.Coverage = storage.CoverageIndexed
		}, "only a partial document keeps an unproven declaration"),
		Entry("an excluded reason on a syntax document", func(_ *storage.Document, content *storage.DocumentContent) {
			content.Excluded = "build constraints"
		}, `"syntax" coverage with excluded reason`),
		Entry("an excluded document with symbols", func(document *storage.Document, content *storage.DocumentContent) {
			document.Coverage, content.Excluded = storage.CoverageExcluded, "build constraints"
		}, "an excluded document has no symbols or occurrences"),
		Entry("another package", func(document *storage.Document, _ *storage.DocumentContent) {
			document.PackagePath = "example.org/other"
		}, "source revision has"),
		Entry("another content package", func(_ *storage.Document, content *storage.DocumentContent) { content.PackagePath = "example.org/other" }, "content package"),
		Entry("extents that overlap without nesting", func(_ *storage.Document, content *storage.DocumentContent) {
			content.Symbols[2].ExtentBytes = storage.ByteSpan{55, 99}
		}, "other than by nesting"),
		Entry("a name range that does not end after it starts", func(_ *storage.Document, content *storage.DocumentContent) {
			content.Symbols[0].Name = storage.Range{3, 6, 3, 6}
		}, "does not end after it starts"),
		Entry("a byte span that does not end after it starts", func(_ *storage.Document, content *storage.DocumentContent) {
			content.Occurrences[0].Bytes = storage.ByteSpan{92, 88}
		}, "does not end after it starts"),
		Entry("a name outside its extent", func(_ *storage.Document, content *storage.DocumentContent) {
			content.Symbols[1].NameBytes = storage.ByteSpan{38, 44}
		}, "outside extent"),
		Entry("a syntax symbol with an id", func(_ *storage.Document, content *storage.DocumentContent) {
			id := digest("typed")
			content.Symbols[0].ID = &id
		}, "has no id"),
		Entry("a key that is not the identifier key", func(_ *storage.Document, content *storage.DocumentContent) { content.Symbols[0].Key = "v1:[]" }, "does not match identifier key"),
		Entry("an undeclared enclosing key", func(_ *storage.Document, content *storage.DocumentContent) {
			content.Occurrences[0].EnclosingKey = "v1:[]"
		}, "is not declared"),
		Entry("an unsupported format version", func(_ *storage.Document, content *storage.DocumentContent) { content.Version = 3 }, "format version 3"),
	)

	It("round-trips a valid typed document", func() {
		document, source, content := typedWorkerDocument()
		decoded, err := storage.DecodeDocument(encodeDocument(document, content), source)
		Expect(err).ToNot(HaveOccurred())
		Expect(decoded).To(Equal(content))
	})

	DescribeTable("rejects a typed document whose symbols or occurrences are not proven facts",
		func(coverage storage.Coverage, mutate func(*storage.DocumentContent), message string) {
			document, source, content := typedWorkerDocument()
			document.Coverage = coverage
			mutate(&content)
			_, err := storage.DecodeDocument(encodeDocument(document, content), source)
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("an unproven declaration in an indexed document", storage.CoverageIndexed, func(content *storage.DocumentContent) {
			content.Symbols[1].ID, content.Symbols[1].ShapeHash = nil, ""
		}, "only a partial document keeps an unproven declaration"),
		Entry("a type form that disagrees with its shape", storage.CoverageIndexed, func(content *storage.DocumentContent) {
			content.Symbols[0].TypeForm = "interface"
		}, "shape is \"struct\""),
		Entry("an unproven declaration with a shape hash in a partial document", storage.CoveragePartial, func(content *storage.DocumentContent) {
			content.Symbols[1].ID = nil
		}, "only a partial document keeps an unproven declaration"),
		Entry("a declared builtin", storage.CoverageIndexed, func(content *storage.DocumentContent) { content.Symbols[1].Kind = "builtin" }, `cannot declare a "builtin" symbol`),
		Entry("an enclosing symbol declared elsewhere", storage.CoverageIndexed, func(content *storage.DocumentContent) {
			elsewhere := digest("elsewhere")
			content.Occurrences[1].Enclosing = &elsewhere
		}, "is not declared in the document"),
		Entry("an occurrence without a symbol or a note", storage.CoverageIndexed, func(content *storage.DocumentContent) {
			content.Occurrences[1].Note = ""
		}, "states why in note"),
		Entry("a reference carrying a call target", storage.CoverageIndexed, func(content *storage.DocumentContent) {
			content.Occurrences[1].Target = content.Occurrences[0].Target
		}, "carries a call target only when it is a call"),
		Entry("an unknown role", storage.CoverageIndexed, func(content *storage.DocumentContent) { content.Occurrences[1].Role = "mention" }, `unknown role "mention"`),
	)

	It("accepts an excluded document that says why it is empty", func() {
		document, source, _ := workerDocument()
		document.Coverage, document.SymbolCount, document.OccurrenceCount = storage.CoverageExcluded, 0, 0
		content := storage.DocumentContent{
			Version: storage.DocumentFormatVersion, PackagePath: workerPackage, Symbols: []storage.DocumentSymbol{},
			Occurrences: []storage.DocumentOccurrence{}, Diagnostics: []storage.DocumentDiagnostic{}, Excluded: "build constraints",
		}
		decoded, err := storage.DecodeDocument(encodeDocument(document, content), source)
		Expect(err).ToNot(HaveOccurred())
		Expect(decoded.Excluded).To(Equal("build constraints"))
	})

	It("rejects fields the format does not define", func() {
		document, source, _ := workerDocument()
		document.SymbolCount, document.OccurrenceCount = 0, 0
		document.Content = storage.JSON(`{"version":1,"package_path":"example.org/acme/worker","symbols":[],"occurrences":[],"diagnostics":[],"projection":{}}`)
		_, err := storage.DecodeDocument(document, source)
		Expect(err).To(MatchError(ContainSubstring(`unknown field "projection"`)))
	})
})
