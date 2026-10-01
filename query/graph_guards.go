package query

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/golang"
	"github.com/flanksource/uir/storage"
	"golang.org/x/tools/go/ast/astutil"
	"gorm.io/gorm"
)

// guardFile is one parsed source file; guards are read from its syntax alone.
type guardFile struct {
	fset   *token.FileSet
	tokens *token.File
	syntax *ast.File
	src    []byte
}

func parseGuardFile(path string, src []byte) (*guardFile, error) {
	fset := token.NewFileSet()
	syntax, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return &guardFile{fset: fset, tokens: fset.File(syntax.FileStart), syntax: syntax, src: src}, nil
}

// siteContext is what the source says about one call site: the call as written and its guards.
type siteContext struct {
	Guards []uir.ConditionStmt
	// Call is the source text of the whole call, arguments included, exactly as written.
	Call string
}

// siteAt reads the call site whose callee is at span, which is the callee's identifier or its whole
// callee expression. The call is the innermost call expression whose callee covers span. The guards
// are the conditions that must hold to reach it, outermost first: the condition of each enclosing if
// and conditional for, its negation in an else, and the match of each enclosing switch or select
// clause. A condition, init, switch tag, or case list is not guarded by its own statement. Guards
// outside a function literal still apply inside it.
func (file *guardFile) siteAt(span storage.ByteSpan) (siteContext, error) {
	if span[0] < 0 || span[1] < span[0] || span[1] > file.tokens.Size() {
		return siteContext{}, fmt.Errorf("bytes %v lie outside the %d bytes of %q", span, file.tokens.Size(), file.tokens.Name())
	}
	start, end := file.tokens.Pos(span[0]), file.tokens.Pos(span[1])
	path, _ := astutil.PathEnclosingInterval(file.syntax, start, end)
	callAt := slices.IndexFunc(path, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		return isCall && call.Fun.Pos() <= start && end <= call.Fun.End()
	})
	if callAt < 0 {
		return siteContext{}, fmt.Errorf("no call in %q has its callee at bytes %v (line %d)", file.tokens.Name(), span, file.tokens.Line(start))
	}
	site := siteContext{Call: string(file.src[file.tokens.Offset(path[callAt].Pos()):file.tokens.Offset(path[callAt].End())])}
	for position := len(path) - 1; position > 0; position-- {
		if guard, guarded := file.guardEntering(path, position); guarded {
			site.Guards = append(site.Guards, uir.NewCondition(guard))
		}
	}
	return site, nil
}

// guardEntering is what must hold to step from path[position] into its child path[position-1].
func (file *guardFile) guardEntering(path []ast.Node, position int) (uir.ExprStmt, bool) {
	child := path[position-1]
	switch parent := path[position].(type) {
	case *ast.IfStmt:
		switch child {
		case ast.Node(parent.Body):
			return file.lower(parent.Cond), true
		case parent.Else:
			return negate(file.lower(parent.Cond)), true
		}
	case *ast.ForStmt:
		if parent.Cond != nil && (child == ast.Node(parent.Body) || child == parent.Post) {
			return file.lower(parent.Cond), true
		}
	case *ast.CaseClause:
		if slices.ContainsFunc(parent.Body, func(statement ast.Stmt) bool { return statement == child }) {
			return file.caseGuard(parent, path[position+2])
		}
	case *ast.CommClause:
		if parent.Comm == nil {
			return sourceText("default"), true
		}
		if child != parent.Comm {
			return golang.SourceExpr(file.fset, file.src, parent.Comm), true
		}
	}
	return uir.ExprStmt{}, false
}

func (file *guardFile) lower(expression ast.Expr) uir.ExprStmt {
	return golang.LowerExpr(file.fset, file.src, expression)
}

// caseGuard is the guard of a clause's body in its switch: a tagged switch compares the tag to each
// case value, a tagless one takes the case expressions as they are, and a type switch compares the
// switched value's type, kept as source text.
func (file *guardFile) caseGuard(clause *ast.CaseClause, owner ast.Node) (uir.ExprStmt, bool) {
	var clauses []ast.Stmt
	match := file.lower
	switch statement := owner.(type) {
	case *ast.SwitchStmt:
		clauses = statement.Body.List
		if statement.Tag != nil {
			tag := file.lower(statement.Tag)
			match = func(value ast.Expr) uir.ExprStmt {
				return uir.BinaryExpr(tag, uir.BinaryOpEqual, file.lower(value))
			}
		}
	case *ast.TypeSwitchStmt:
		clauses = statement.Body.List
		subject := golang.SourceExpr(file.fset, file.src, typeSwitchSubject(statement))
		match = func(value ast.Expr) uir.ExprStmt {
			return uir.BinaryExpr(subject, uir.BinaryOpEqual, golang.SourceExpr(file.fset, file.src, value))
		}
	default:
		panic(fmt.Sprintf("query: case clause at %s belongs to a %T", file.fset.Position(clause.Pos()), owner))
	}
	position := slices.Index(clauses, ast.Stmt(clause))
	if position < 0 {
		panic(fmt.Sprintf("query: case clause at %s is not in its switch", file.fset.Position(clause.Pos())))
	}
	return clauseCondition(clauses, position, match)
}

// typeSwitchSubject is the x.(type) of `switch x.(type)` and of `switch v := x.(type)`.
func typeSwitchSubject(statement *ast.TypeSwitchStmt) ast.Expr {
	switch assign := statement.Assign.(type) {
	case *ast.ExprStmt:
		return assign.X
	case *ast.AssignStmt:
		if len(assign.Rhs) == 1 {
			return assign.Rhs[0]
		}
	}
	panic(fmt.Sprintf("query: type switch guard is a %T, not x.(type) or v := x.(type)", statement.Assign))
}

// clauseCondition is what must hold to run the clause at position: one of its values matches, or for
// the default none of the other clauses' values does. A clause the one before falls through into
// also runs when that one does. No condition is reported when the clause always runs.
func clauseCondition(clauses []ast.Stmt, position int, match func(ast.Expr) uir.ExprStmt) (uir.ExprStmt, bool) {
	clause := clauses[position].(*ast.CaseClause)
	condition, conditional := matchesAny(clause.List, match)
	if clause.List == nil {
		var others []ast.Expr
		for _, sibling := range clauses {
			others = append(others, sibling.(*ast.CaseClause).List...)
		}
		if condition, conditional = matchesAny(others, match); conditional {
			condition = negate(condition)
		}
	}
	if !conditional || position == 0 || !fallsThrough(clauses[position-1].(*ast.CaseClause)) {
		return condition, conditional
	}
	previous, conditional := clauseCondition(clauses, position-1, match)
	if !conditional {
		return uir.ExprStmt{}, false
	}
	return uir.BinaryExpr(previous, uir.BinaryOpOr, condition), true
}

func matchesAny(values []ast.Expr, match func(ast.Expr) uir.ExprStmt) (uir.ExprStmt, bool) {
	if len(values) == 0 {
		return uir.ExprStmt{}, false
	}
	either := match(values[0])
	for _, value := range values[1:] {
		either = uir.BinaryExpr(either, uir.BinaryOpOr, match(value))
	}
	return either, true
}

func fallsThrough(clause *ast.CaseClause) bool {
	if len(clause.Body) == 0 {
		return false
	}
	branch, ok := clause.Body[len(clause.Body)-1].(*ast.BranchStmt)
	return ok && branch.Tok == token.FALLTHROUGH
}

func negate(expression uir.ExprStmt) uir.ExprStmt {
	return uir.UnaryExpr(uir.UnaryOpNot, expression)
}

func sourceText(text string) uir.ExprStmt {
	expression := uir.ExprStmt{}
	expression.Type, expression.Content = uir.ASTStatementTypeExpression, &text
	return expression
}

func guardTexts(guards []uir.ConditionStmt) []string {
	var texts []string
	for _, guard := range guards {
		texts = append(texts, guard.Pretty().String())
	}
	return texts
}

// guardReader reads call sites from hash-verified source, parsing each file once. A file whose
// indexed bytes cannot be recovered or parsed is remembered in unreadable, and its sites are not read.
type guardReader struct {
	database   *gorm.DB
	sources    *scopeSources
	files      map[guardFileKey]*guardFile
	unreadable map[string]error
}

type guardFileKey struct{ snapshot, path string }

func newGuardReader(database *gorm.DB, sources *scopeSources) *guardReader {
	return &guardReader{database: database, sources: sources, files: map[guardFileKey]*guardFile{}, unreadable: map[string]error{}}
}

// site reads the call site of one call occurrence row. It reports false when the row's file is
// unreadable.
func (reader *guardReader) site(ctx context.Context, match ModuleMatch) (siteContext, bool, error) {
	file, err := reader.file(ctx, guardFileKey{snapshot: match.SnapshotID, path: match.Path})
	if err != nil || file == nil {
		return siteContext{}, false, err
	}
	site, err := file.siteAt(match.span)
	return site, err == nil, err
}

// file is the parsed source at key, or nil when it is unreadable. The only error is the context's.
func (reader *guardReader) file(ctx context.Context, key guardFileKey) (*guardFile, error) {
	if file, parsed := reader.files[key]; parsed {
		return file, nil
	}
	file, err := reader.parse(ctx, key)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		if _, reported := reader.unreadable[key.path]; !reported {
			reader.unreadable[key.path] = err
		}
		file = nil
	}
	reader.files[key] = file
	return file, nil
}

func (reader *guardReader) parse(ctx context.Context, key guardFileKey) (*guardFile, error) {
	scope, document, err := reader.sources.document(key.snapshot, key.path)
	if err != nil {
		return nil, err
	}
	content, _, err := readVerifiedSource(ctx, reader.database, scope, verifiedSource{path: key.path, hash: document.Source.ContentHash, snapshot: key.snapshot})
	if err != nil {
		return nil, err
	}
	return parseGuardFile(key.path, content)
}

// unreadablePaths are the files whose guards could not be derived, sorted.
func (reader *guardReader) unreadablePaths() []string {
	paths := make([]string, 0, len(reader.unreadable))
	for path := range reader.unreadable {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}
