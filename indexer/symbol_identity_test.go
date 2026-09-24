package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	storeModule  = "example.org/acme"
	storePackage = "example.org/acme/store"
	storeSource  = "package store\n" +
		"\n" +
		"type Invoice struct {\n" +
		"\tID string\n" +
		"\tTotal int `json:\"total\"`\n" +
		"\tNotes []string\n" +
		"}\n" +
		"\n" +
		"type Loader interface {\n" +
		"\tSave(ctx any, inv Invoice) error\n" +
		"\tLoad(id string) (Invoice, error)\n" +
		"}\n" +
		"\n" +
		"type List[T any] struct{ items []T }\n" +
		"\n" +
		"func (l *List[T]) Push(value T, more ...T) {}\n" +
		"\n" +
		"type hidden struct{}\n" +
		"\n" +
		"func (hidden) Visible() {}\n" +
		"\n" +
		"const Limit = 10\n" +
		"\n" +
		"var Default = Invoice{}\n" +
		"\n" +
		"func Sum(values ...int) (total int, err error) {\n" +
		"\tcount := len(values)\n" +
		"\treturn count, nil\n" +
		"}\n"
)

// checkedStore type-checks storeSource as a workspace package of storeModule.
func checkedStore() (*types.Package, *types.Info, *symbolResolver) {
	GinkgoHelper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "store.go", storeSource, parser.ParseComments)
	Expect(err).ToNot(HaveOccurred())
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	pkg, err := (&types.Config{}).Check(storePackage, fileSet, []*ast.File{file}, info)
	Expect(err).ToNot(HaveOccurred())
	resolver := newSymbolResolver(map[*types.Package]packageOrigin{pkg: {Class: workspacePackage, ModulePath: storeModule}})
	return pkg, info, resolver
}

func lookupMethod(pkg *types.Package, typeName, method string) types.Object {
	GinkgoHelper()
	object, _, _ := types.LookupFieldOrMethod(pkg.Scope().Lookup(typeName).Type(), true, pkg, method)
	Expect(object).ToNot(BeNil())
	return object
}

func resolvedRow(resolver *symbolResolver, object types.Object) symbolRowSummary {
	GinkgoHelper()
	resolved, err := resolver.resolve(object)
	Expect(err).ToNot(HaveOccurred())
	Expect(resolved.Note).To(BeEmpty())
	row := resolver.rows[resolved.ID]
	owner := ""
	if row.OwnerID != nil {
		owner = resolver.rows[*row.OwnerID].Name
	}
	return symbolRowSummary{Kind: row.Kind, Owner: owner, Visibility: row.Visibility, ModuleKey: row.ModuleKey, Parameters: row.ParameterTypes.String()}
}

type symbolRowSummary struct {
	Kind, Owner, Visibility, ModuleKey, Parameters string
}

var _ = Describe("canonical symbol identity", func() {
	It("encodes every identity field length-delimited and digests the encoding", func() {
		identity := symbolIdentity{
			ModuleKey: storeModule, PackagePath: storePackage, Kind: "method", OwnerID: "ab",
			Name: "Save", ParameterTypes: []string{"context.Context", "example.org/acme/store.Invoice"},
		}
		key := identity.canonicalKey()
		Expect(key).To(Equal("10:uir-symbol,1:1,16:example.org/acme,22:example.org/acme/store,6:method,2:ab,4:Save,1:2,15:context.Context,30:example.org/acme/store.Invoice,"))
		digest := sha256.Sum256([]byte(key))
		Expect(identity.id()).To(Equal(hex.EncodeToString(digest[:])))
	})

	It("derives kind, owner, visibility, and ordered parameter types from the type checker", func() {
		pkg, _, resolver := checkedStore()
		scope := pkg.Scope()
		Expect(map[string]symbolRowSummary{
			"Invoice":        resolvedRow(resolver, scope.Lookup("Invoice")),
			"Invoice.Total":  resolvedRow(resolver, lookupMethod(pkg, "Invoice", "Total")),
			"Loader.Save":    resolvedRow(resolver, lookupMethod(pkg, "Loader", "Save")),
			"List.items":     resolvedRow(resolver, lookupMethod(pkg, "List", "items")),
			"List.Push":      resolvedRow(resolver, lookupMethod(pkg, "List", "Push")),
			"hidden.Visible": resolvedRow(resolver, lookupMethod(pkg, "hidden", "Visible")),
			"Limit":          resolvedRow(resolver, scope.Lookup("Limit")),
			"Default":        resolvedRow(resolver, scope.Lookup("Default")),
			"Sum":            resolvedRow(resolver, scope.Lookup("Sum")),
			"len":            resolvedRow(resolver, types.Universe.Lookup("len")),
		}).To(Equal(map[string]symbolRowSummary{
			"Invoice":        {Kind: "type", Visibility: "exported", ModuleKey: storeModule, Parameters: `[]`},
			"Invoice.Total":  {Kind: "field", Owner: "Invoice", Visibility: "exported", ModuleKey: storeModule, Parameters: `[]`},
			"Loader.Save":    {Kind: "method", Owner: "Loader", Visibility: "exported", ModuleKey: storeModule, Parameters: `["interface{}","example.org/acme/store.Invoice"]`},
			"List.items":     {Kind: "field", Owner: "List", Visibility: "internal", ModuleKey: storeModule, Parameters: `[]`},
			"List.Push":      {Kind: "method", Owner: "List", Visibility: "exported", ModuleKey: storeModule, Parameters: `["$0","...$0"]`},
			"hidden.Visible": {Kind: "method", Owner: "hidden", Visibility: "internal", ModuleKey: storeModule, Parameters: `[]`},
			"Limit":          {Kind: "const", Visibility: "exported", ModuleKey: storeModule, Parameters: `[]`},
			"Default":        {Kind: "var", Visibility: "exported", ModuleKey: storeModule, Parameters: `[]`},
			"Sum":            {Kind: "func", Visibility: "exported", ModuleKey: storeModule, Parameters: `["...int"]`},
			"len":            {Kind: "builtin", Visibility: "exported", Parameters: `[]`},
		}))
	})

	It("gives parameters and locals no canonical symbol", func() {
		_, info, resolver := checkedStore()
		notes := map[string]string{}
		for ident, object := range info.Defs {
			if object == nil || (ident.Name != "count" && ident.Name != "values" && ident.Name != "T") {
				continue
			}
			resolved, err := resolver.resolve(object)
			Expect(err).ToNot(HaveOccurred())
			Expect(resolved.ID).To(BeEmpty())
			notes[ident.Name] = resolved.Note
		}
		Expect(notes).To(Equal(map[string]string{"count": "local binding", "values": "local binding", "T": "type parameter"}))
	})
})

var _ = Describe("symbol shapes", func() {
	It("renders the canonical multi-line layout and hashes it", func() {
		pkg, _, resolver := checkedStore()
		scope := pkg.Scope()
		shapes := map[string]string{}
		for name, object := range map[string]types.Object{
			"Invoice": scope.Lookup("Invoice"), "Invoice.Total": lookupMethod(pkg, "Invoice", "Total"),
			"Loader": scope.Lookup("Loader"), "Loader.Save": lookupMethod(pkg, "Loader", "Save"),
			"List": scope.Lookup("List"), "List.Push": lookupMethod(pkg, "List", "Push"),
			"Limit": scope.Lookup("Limit"), "Default": scope.Lookup("Default"), "Sum": scope.Lookup("Sum"),
		} {
			shape, err := resolver.shape(object)
			Expect(err).ToNot(HaveOccurred(), name)
			shapes[name] = shape
		}
		Expect(shapes).To(Equal(map[string]string{
			"Invoice":       "type Invoice struct {\n\tID    string\n\tTotal int `json:\"total\"`\n\tNotes []string\n}",
			"Invoice.Total": "Total int `json:\"total\"`",
			"Loader":        "type Loader interface {\n\tLoad(id string) (Invoice, error)\n\tSave(ctx any, inv Invoice) error\n}",
			"Loader.Save":   "func (Loader) Save(ctx any, inv Invoice) error",
			"List":          "type List[T any] struct {\n\titems []T\n}",
			"List.Push":     "func (l *List[T]) Push(value T, more ...T)",
			"Limit":         "const Limit untyped int = 10",
			"Default":       "var Default Invoice",
			"Sum":           "func Sum(values ...int) (total int, err error)",
		}))
		Expect(shapeHash(shapes["Sum"])).To(HaveLen(64))
		Expect(shapeHash(shapes["Sum"])).ToNot(Equal(shapeHash(shapes["Default"])))
	})
})
