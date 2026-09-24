package indexer

import (
	"encoding/json"
	"strings"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const workerSource = "package worker\n" +
	"\n" +
	"// Service runs jobs.\n" +
	"type Service struct {\n" +
	"\tName string `json:\"name\"`\n" +
	"\tcount int\n" +
	"}\n" +
	"\n" +
	"func (s *Service) Run(value string) error {\n" +
	"\t/* é𝄞 */ s.stop()\n" +
	"\treturn nil\n" +
	"}\n" +
	"\n" +
	"type helper struct{ Visible int }\n" +
	"\n" +
	"func (h helper) Exported() {}\n" +
	"\n" +
	"func Build() *Service { return &Service{} }\n"

type symbolSummary struct {
	Kind       string
	Visibility string
	Shape      string
}

var _ = Describe("syntax documents", func() {
	var content storage.DocumentContent

	BeforeEach(func() {
		indexed, err := extractGoFile("worker/service.go", "example.org/acme/worker", []byte(workerSource))
		Expect(err).ToNot(HaveOccurred())
		content = indexed.syntaxDocument()
	})

	It("records each declaration's kind, visibility through its owner chain, and source-rendered shape", func() {
		summaries := map[string]symbolSummary{}
		for _, symbol := range content.Symbols {
			Expect(symbol.ID).To(BeNil())
			Expect(symbol.ShapeHash).To(BeEmpty())
			Expect(symbol.BodyHash).To(HaveLen(64))
			name := strings.Trim(symbol.Identifier.Type+"."+symbol.Identifier.Method+symbol.Identifier.Field, ".")
			summaries[name] = symbolSummary{Kind: symbol.Kind, Visibility: symbol.Visibility, Shape: symbol.Shape}
		}
		Expect(summaries).To(Equal(map[string]symbolSummary{
			"Service":         {"type", "exported", "type Service struct {\n\tName  string `json:\"name\"`\n\tcount int\n}"},
			"Service.Name":    {"field", "exported", "Name string `json:\"name\"`"},
			"Service.count":   {"field", "internal", "count int"},
			"Service.Run":     {"method", "exported", "func (s *Service) Run(value string) error"},
			"helper":          {"type", "internal", "type helper struct{ Visible int }"},
			"helper.Visible":  {"field", "internal", "Visible int"},
			"helper.Exported": {"method", "internal", "func (h helper) Exported()"},
			"Build":           {"func", "exported", "func Build() *Service"},
		}))
	})

	It("spans a declaration's doc comment and nests fields inside their type", func() {
		service, name := content.Symbols[0], content.Symbols[1]
		Expect(service.Identifier.Type).To(Equal("Service"))
		Expect(service.Extent).To(Equal(storage.Range{3, 1, 7, 2}))
		Expect(service.Name).To(Equal(storage.Range{4, 6, 4, 13}))
		Expect(workerSource[service.NameBytes[0]:service.NameBytes[1]]).To(Equal("Service"))
		Expect(workerSource[service.ExtentBytes[0]:service.ExtentBytes[1]]).To(HavePrefix("// Service runs jobs.\ntype Service struct {"))
		Expect(name.Identifier.Field).To(Equal("Name"))
		Expect(name.ExtentBytes[0]).To(BeNumerically(">", service.ExtentBytes[0]))
		Expect(name.ExtentBytes[1]).To(BeNumerically("<", service.ExtentBytes[1]))
	})

	It("counts columns in UTF-16 code units and ties each call to its enclosing declaration", func() {
		Expect(content.Occurrences).To(HaveLen(1))
		stop := content.Occurrences[0]
		Expect(stop.Role).To(Equal("call"))
		Expect(stop.Symbol).To(BeNil())
		Expect(stop.Target.Method).To(Equal("stop"))
		Expect(stop.Text).To(Equal("s.stop"))
		Expect(stop.Range).To(Equal(storage.Range{10, 12, 10, 18}))
		Expect(workerSource[stop.Bytes[0]:stop.Bytes[1]]).To(Equal("s.stop"))
		Expect(stop.EnclosingKey).To(Equal(content.Symbols[3].Key))
		Expect(content.Symbols[3].Identifier.Method).To(Equal("Run"))
	})

	It("passes the publisher's document validation", func() {
		encoded, err := json.Marshal(content)
		Expect(err).ToNot(HaveOccurred())
		source := storage.SourceRevision{ID: uuid.New(), RootID: uuid.New(), PathKey: "worker/service.go", PackagePath: "example.org/acme/worker"}
		document := storage.Document{
			ID: uuid.New(), RootID: source.RootID, PathKey: source.PathKey, SourceRevisionID: source.ID,
			PackagePath: source.PackagePath, InputHash: hashBytes([]byte("input")), Coverage: storage.CoverageSyntax,
			SymbolCount: len(content.Symbols), OccurrenceCount: len(content.Occurrences), Content: storage.JSON(encoded),
		}
		decoded, err := storage.DecodeDocument(document, source)
		Expect(err).ToNot(HaveOccurred())
		Expect(decoded.Symbols).To(HaveLen(8))
	})

	DescribeTable("hashes a body's tokens, ignoring comments and layout",
		func(left, right string, equal bool) {
			leftHash, err := tokenStreamHash([]byte(left))
			Expect(err).ToNot(HaveOccurred())
			rightHash, err := tokenStreamHash([]byte(right))
			Expect(err).ToNot(HaveOccurred())
			Expect(leftHash == rightHash).To(Equal(equal))
		},
		Entry("reflowed whitespace", "func A() {\n\treturn\n}", "func A() {   return }", true),
		Entry("an added comment", "func A() { return }", "// A returns.\nfunc A() { /* now */ return }", true),
		Entry("a changed statement", "func A() { return }", "func A() { panic(1) }", false),
		Entry("a changed literal", `func A() string { return "a" }`, `func A() string { return "b" }`, false),
	)
})
