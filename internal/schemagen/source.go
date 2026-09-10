package schemagen

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strings"
)

// sourceFacts is what the schema needs and reflection cannot see: doc comments,
// and the value of every typed string constant in the package.
type sourceFacts struct {
	typeDocs  map[string]string
	fieldDocs map[string]map[string]string
	enums     map[string][]string
}

func (s *sourceFacts) fieldDoc(owner, field string) string {
	return s.fieldDocs[owner][field]
}

func loadSource(dir, pkgPath string) (*sourceFacts, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", dir, err)
	}

	pkg, ok := parsed[packageName]
	if !ok {
		return nil, fmt.Errorf("no package %q in %s", packageName, dir)
	}

	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	files := make([]*ast.File, 0, len(names))
	for _, name := range names {
		files = append(files, pkg.Files[name])
	}

	facts := &sourceFacts{
		typeDocs:  map[string]string{},
		fieldDocs: map[string]map[string]string{},
	}
	collectDocs(files, facts)
	if facts.enums, err = collectEnums(fset, files, pkgPath); err != nil {
		return nil, err
	}
	return facts, nil
}

func collectDocs(files []*ast.File, facts *sourceFacts) {
	for _, f := range files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if doc := docText(ts.Doc, gen.Doc); doc != "" {
					facts.typeDocs[ts.Name.Name] = doc
				}
				if st, ok := ts.Type.(*ast.StructType); ok {
					facts.fieldDocs[ts.Name.Name] = fieldDocs(st)
				}
			}
		}
	}
}

func fieldDocs(st *ast.StructType) map[string]string {
	docs := map[string]string{}
	for _, field := range st.Fields.List {
		doc := docText(field.Doc, field.Comment)
		if doc == "" {
			continue
		}
		for _, name := range field.Names {
			docs[name.Name] = doc
		}
	}
	return docs
}

// docText takes the first non-empty comment group, flattened to a single line.
func docText(groups ...*ast.CommentGroup) string {
	for _, g := range groups {
		if g == nil {
			continue
		}
		var parts []string
		for _, line := range strings.Split(g.Text(), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				parts = append(parts, line)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	return ""
}

type enumConst struct {
	pos   token.Pos
	value string
}

// collectEnums evaluates every typed string constant in the package, keyed by
// the name of its type. This is a full type-check rather than a read of the
// literals because most StatementType values are built by concatenating other
// constants — ASTStatementTypeLoopFor is ASTStatementTypeLoop + ":for", which
// no amount of AST reading resolves to "control:loop:for".
//
// Imports are left unresolved on purpose: enum constants only ever reference
// their own package, so the resulting type errors are noise. A genuine failure
// would show up as an empty result, which is rejected below.
func collectEnums(fset *token.FileSet, files []*ast.File, pkgPath string) (map[string][]string, error) {
	cfg := &types.Config{
		Error:    func(error) {},
		Importer: unresolvedImports{},
	}
	pkg, _ := cfg.Check(pkgPath, fset, files, nil)
	if pkg == nil {
		return nil, fmt.Errorf("type-checking %s produced no package", pkgPath)
	}

	byType := map[string][]enumConst{}
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		c, ok := scope.Lookup(name).(*types.Const)
		if !ok || c.Val().Kind() != constant.String {
			continue
		}
		named, ok := c.Type().(*types.Named)
		if !ok {
			continue
		}
		owner := named.Obj().Name()
		byType[owner] = append(byType[owner], enumConst{pos: c.Pos(), value: constant.StringVal(c.Val())})
	}
	if len(byType) == 0 {
		return nil, fmt.Errorf("no typed string constants found in %s: the type-check resolved nothing", pkgPath)
	}

	enums := make(map[string][]string, len(byType))
	for owner, consts := range byType {
		enums[owner] = orderedValues(consts, fset)
	}
	return enums, nil
}

// orderedValues lists each distinct value once, in declaration order, so the
// schema reads like the const block it came from and regenerates identically.
func orderedValues(consts []enumConst, fset *token.FileSet) []string {
	sort.Slice(consts, func(i, j int) bool {
		a, b := fset.Position(consts[i].pos), fset.Position(consts[j].pos)
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Offset < b.Offset
	})

	seen := map[string]bool{}
	values := make([]string, 0, len(consts))
	for _, c := range consts {
		if seen[c.value] {
			continue
		}
		seen[c.value] = true
		values = append(values, c.value)
	}
	return values
}

// unresolvedImports fails every import, which keeps the type-check local to the
// package being described. See collectEnums.
type unresolvedImports struct{}

func (unresolvedImports) Import(path string) (*types.Package, error) {
	return nil, fmt.Errorf("import %q deliberately not resolved", path)
}
