package indexer

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
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
			Expect([]string{symbol.PublicHash, symbol.PrivateHash, symbol.PublicShape}).To(Equal([]string{"", "", ""}))
			Expect(symbol.ContentHash).To(HaveLen(64))
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

	DescribeTable("hashes a declaration's normalized tokens alike from syntax and from types",
		func(left, right, name string, equal bool) {
			leftSyntax, leftTyped := declarationContentHashes(left, name)
			rightSyntax, rightTyped := declarationContentHashes(right, name)
			Expect(leftTyped == rightTyped).To(Equal(equal), "typed content_hash equal")
			for _, agreement := range [][2]string{{leftSyntax, leftTyped}, {rightSyntax, rightTyped}} {
				if agreement[0] != "" {
					Expect(agreement[0]).To(Equal(agreement[1]), "syntax and typed content_hash of %s", name)
				}
			}
		},
		Entry("reflowed whitespace", "func A() {\n\treturn\n}", "func A() {   return }", "A", true),
		Entry("an added comment", "func A() { return }", "// A returns.\nfunc A() { /* now */ return }", "A", true),
		Entry("explicit semicolons", "func A() int {\n\tb := 1\n\treturn b\n}", "func A() int { b := 1; return b; }", "A", true),
		Entry("a parameter rename", "func Add(a, b int) int { return a + b }", "func Add(x, y int) int { return x + y }", "Add", true),
		Entry("a receiver rename", "type S struct{ n int }\n\nfunc (s *S) Get() int { return s.n }", "type S struct{ n int }\n\nfunc (self *S) Get() int { return self.n }", "Get", true),
		Entry("a local rename",
			"func Sum() int {\n\ttotal := 0\n\tfor i := range 3 {\n\t\ttotal += i\n\t}\n\treturn total\n}",
			"func Sum() int {\n\tacc := 0\n\tfor n := range 3 {\n\t\tacc += n\n\t}\n\treturn acc\n}", "Sum", true),
		Entry("renames through shadowing, redeclaration, and a type switch",
			"func T(v any) (n int, err error) {\n\tswitch x := v.(type) {\n\tcase int:\n\t\tn = x\n\t}\n\tif n, err := 2, error(nil); err == nil {\n\t\t_ = n\n\t}\n\tm, err := 3, err\n\treturn n + m, err\n}",
			"func T(in any) (count int, e error) {\n\tswitch value := in.(type) {\n\tcase int:\n\t\tcount = value\n\t}\n\tif count, e := 2, error(nil); e == nil {\n\t\t_ = count\n\t}\n\tk, e := 3, e\n\treturn count + k, e\n}",
			"T", true),
		Entry("a parameter rename beside a same-named composite literal key",
			"type P struct{ name string }\n\nfunc NewP(name string) P { return P{name: name} }",
			"type P struct{ name string }\n\nfunc NewP(label string) P { return P{name: label} }", "NewP", true),
		Entry("a call rewrapped with a trailing comma",
			"func A() []int { return append([]int{1, 2}, 3) }",
			"func A() []int {\n\treturn append(\n\t\t[]int{\n\t\t\t1,\n\t\t\t2,\n\t\t},\n\t\t3,\n\t)\n}", "A", true),
		Entry("a type moved into a group", "type A int", "type (\n\tA int\n)", "A", true),
		Entry("a var moved into a group", "var V = 1", "var (\n\tV = 1\n)", "V", true),
		Entry("swapped parameter uses", "func Sub(a, b int) int { return a - b }", "func Sub(a, b int) int { return b - a }", "Sub", false),
		Entry("a changed literal", `func A() string { return "a" }`, `func A() string { return "b" }`, "A", false),
		Entry("a changed callee", "func A() int { return min(1, 2) }", "func A() int { return max(1, 2) }", "A", false),
		Entry("a renamed package-level reference", "var x, y int\n\nfunc A() int { return x }", "var x, y int\n\nfunc A() int { return y }", "A", false),
	)
})

// declarationContentHashes is the content_hash of the declaration named name in a file of source,
// from its syntax document ("" when the syntax extraction does not describe it) and from its types.
func declarationContentHashes(body, name string) (string, string) {
	GinkgoHelper()
	source := "package calc\n\n" + body + "\n"
	indexed, err := extractGoFile("calc/calc.go", "example.org/acme/calc", []byte(source))
	Expect(err).ToNot(HaveOccurred())
	syntax := ""
	for _, node := range indexed.Nodes {
		if source[node.Name[0]:node.Name[1]] == name {
			syntax = node.ContentHash
		}
	}
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "calc.go", source, parser.ParseComments|parser.SkipObjectResolution)
	Expect(err).ToNot(HaveOccurred())
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	_, err = (&types.Config{}).Check("example.org/acme/calc", fileSet, []*ast.File{file}, info)
	Expect(err).ToNot(HaveOccurred())
	tokens := fileSet.File(file.Pos())
	bindings, err := typedBindings(file, tokens, info)
	Expect(err).ToNot(HaveOccurred())
	for _, declared := range fileDeclarations(file, info) {
		if declared.name.Name == name {
			typed, err := declarationContentHash([]byte(source), storage.ByteSpan{tokens.Offset(declared.start), tokens.Offset(declared.end)}, bindings)
			Expect(err).ToNot(HaveOccurred())
			return syntax, typed
		}
	}
	Fail("no declaration named " + name)
	return "", ""
}
