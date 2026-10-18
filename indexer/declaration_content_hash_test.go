package indexer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"

	"github.com/flanksource/uir/indexer/syntax"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("declaration content hashes", func() {
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
	indexed, err := syntax.Extract("calc/calc.go", "example.org/acme/calc", []byte(source))
	Expect(err).ToNot(HaveOccurred())
	fromSyntax := ""
	for _, node := range indexed.Nodes {
		if source[node.Name[0]:node.Name[1]] == name {
			fromSyntax = node.ContentHash
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
			typed, err := syntax.DeclarationContentHash([]byte(source), storage.ByteSpan{tokens.Offset(declared.start), tokens.Offset(declared.end)}, bindings)
			Expect(err).ToNot(HaveOccurred())
			return fromSyntax, typed
		}
	}
	Fail("no declaration named " + name)
	return "", ""
}
