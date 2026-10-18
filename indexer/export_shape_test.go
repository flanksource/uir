package indexer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	apiPackage   = "example.org/acme/api"
	fooClientPkg = "example.org/foo/v1"
	barClientPkg = "example.org/bar/v1"
	clientSource = "package v1\n\ntype Client struct{ Name string }\n"
)

// sourceImporter type-checks in-memory packages on import.
type sourceImporter struct {
	fileSet *token.FileSet
	sources map[string]string
	checked map[string]*types.Package
}

func (importer *sourceImporter) Import(path string) (*types.Package, error) {
	if pkg, found := importer.checked[path]; found {
		return pkg, nil
	}
	source, found := importer.sources[path]
	if !found {
		return nil, fmt.Errorf("no source for %q", path)
	}
	file, err := parser.ParseFile(importer.fileSet, path+".go", source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	pkg, err := (&types.Config{Importer: importer}).Check(path, importer.fileSet, []*ast.File{file}, nil)
	if err != nil {
		return nil, err
	}
	importer.checked[path] = pkg
	return pkg, nil
}

// apiExport type-checks source as apiPackage, with foo/v1 and bar/v1 importable, and returns its
// exported (id, api shape) pairs and the public_hash of the symbol named Name or Type.Name.
func apiExport(source, symbol string) ([][2]string, string) {
	GinkgoHelper()
	importer := &sourceImporter{
		fileSet: token.NewFileSet(), sources: map[string]string{fooClientPkg: clientSource, barClientPkg: clientSource, apiPackage: source},
		checked: map[string]*types.Package{},
	}
	pkg, err := importer.Import(apiPackage)
	Expect(err).ToNot(HaveOccurred())
	origins := map[*types.Package]packageOrigin{}
	for _, checked := range importer.checked {
		origins[checked] = packageOrigin{Class: workspacePackage, ModulePath: checked.Path()}
	}
	shapes := &exportShapes{resolver: newSymbolResolver(origins)}
	pairs, err := shapes.exportedSymbols(pkg)
	Expect(err).ToNot(HaveOccurred())
	object := pkg.Scope().Lookup(symbol)
	if owner, method, found := strings.Cut(symbol, "."); found {
		object = lookupMethod(pkg, owner, method)
	}
	rendered, err := shapes.resolver.shapes(object)
	Expect(err).ToNot(HaveOccurred())
	public, _ := layerHashes(rendered)
	return pairs, public
}

var _ = Describe("export shapes", func() {
	DescribeTable("key importers on a name-free API qualified by import path, while public_hash keeps names",
		func(left, right, symbol string, sameShape, samePublic bool) {
			leftShape, leftPublic := apiExport(left, symbol)
			rightShape, rightPublic := apiExport(right, symbol)
			Expect([]bool{fmt.Sprint(leftShape) == fmt.Sprint(rightShape), leftPublic == rightPublic}).
				To(Equal([]bool{sameShape, samePublic}), "export shape equal, public_hash equal")
		},
		Entry("a parameter rename",
			"package api\n\nfunc Do(count int, label string) error { return nil }\n",
			"package api\n\nfunc Do(n int, name string) error { return nil }\n",
			"Do", true, false),
		Entry("a receiver rename",
			"package api\n\ntype S struct{}\n\nfunc (s *S) Run(n int) {}\n",
			"package api\n\ntype S struct{}\n\nfunc (self *S) Run(n int) {}\n",
			"S.Run", true, false),
		Entry("a same-name import retarget in a result",
			"package api\n\nimport \"example.org/foo/v1\"\n\nfunc New() v1.Client { return v1.Client{} }\n",
			"package api\n\nimport \"example.org/bar/v1\"\n\nfunc New() v1.Client { return v1.Client{} }\n",
			"New", false, true),
		Entry("a same-name import retarget in a field",
			"package api\n\nimport \"example.org/foo/v1\"\n\ntype Config struct{ Client v1.Client }\n",
			"package api\n\nimport \"example.org/bar/v1\"\n\ntype Config struct{ Client v1.Client }\n",
			"Config", false, true),
		Entry("a parameter type change",
			"package api\n\nfunc Do(count int) {}\n",
			"package api\n\nfunc Do(count int64) {}\n",
			"Do", false, false),
		Entry("an added unexported field behind an existing one",
			"package api\n\ntype Config struct {\n\tName string\n\tnote string\n}\n",
			"package api\n\ntype Config struct {\n\tName string\n\tnote string\n\trate int\n}\n",
			"Config", true, true),
		Entry("a type parameter rename",
			"package api\n\nfunc Map[T any](values []T) []T { return values }\n",
			"package api\n\nfunc Map[E any](values []E) []E { return values }\n",
			"Map", true, false),
		Entry("a type parameter constraint change",
			"package api\n\nfunc Map[T any](values []T) []T { return values }\n",
			"package api\n\nfunc Map[T comparable](values []T) []T { return values }\n",
			"Map", false, false),
		Entry("a method promoted through an unexported embedded field",
			"package api\n\ntype inner struct{}\n\ntype S struct{ inner }\n",
			"package api\n\ntype inner struct{}\n\nfunc (inner) M() {}\n\ntype S struct{ inner }\n",
			"S", false, true),
		Entry("a constant value change",
			"package api\n\nconst Limit = 10\n",
			"package api\n\nconst Limit = 11\n",
			"Limit", false, false),
	)
})
