package query

import (
	"context"
	"errors"
	"strings"

	"github.com/flanksource/uir/query/sqlsyntax"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

// SQLClosureKind says how far the rows of a row query reach, which decides how a lint finding is reused.
type SQLClosureKind string

const (
	// SQLClosureSubject is a symbol selector source whose rows read only facts of the symbol each row is
	// about, its subject, so a subject's rows are reused while that symbol is unchanged.
	SQLClosureSubject SQLClosureKind = "subject"
	// SQLClosureFile is the files() source: a file's rows read only that file.
	SQLClosureFile SQLClosureKind = "file"
	// SQLClosureSnapshot is every other query: a navigation source or a membership condition reads symbols
	// the rows are not about, so its rows are reused only while every snapshot of the scope is unchanged.
	SQLClosureSnapshot SQLClosureKind = "snapshot"
	// SQLClosureLive reads facts recorded after the snapshot, such as test coverage, so it is never reused.
	SQLClosureLive SQLClosureKind = "live"
)

// SQLClosure is what the rows of a row query depend on.
type SQLClosure struct {
	Kind SQLClosureKind
	// Selector is the source selector of a subject closure.
	Selector string
	// Document says a subject closure reads facts of the symbol's declaring document beyond its layer
	// fingerprints: its location, physical body lines, producer metrics, resolved type or properties.
	Document bool
	// Snapshots says a snapshot closure reads facts of the snapshot itself (its id, or content
	// generations along its base chain), so it is reused only on the same snapshots.
	Snapshots bool
}

// sqlIdentityFields are facts of a symbol's identity, or of the token content its layer fingerprints
// digest: they change only with the symbol.
var sqlIdentityFields = map[string]bool{
	"symbol": true, "symbol.id": true, "symbol.kind": true, "symbol.name": true, "symbol.query_name": true,
	"symbol.module": true, "symbol.package": true, "symbol.owner": true, "symbol.visibility": true,
	"package": true, "module": true, "role": true, "symbol.metrics.param_count": true,
}

// sqlClosure classifies every field the query reads (projection, predicate and order) and its source.
func sqlClosure(parsed *sqlsyntax.Query) SQLClosure {
	file := strings.EqualFold(parsed.Source.Name, "files")
	closure := SQLClosure{Kind: SQLClosureSnapshot}
	switch {
	case file:
		closure.Kind = SQLClosureFile
	case parsed.Source.Selector != "":
		// A namespaced kind selector does not parse without the kind registry; it selects one kind.
		selector, _ := parseSelector(parsed.Source.Selector)
		switch selector.Kind {
		case "pkg", "mod", "all", "path":
		default:
			closure.Kind, closure.Selector = SQLClosureSubject, parsed.Source.Selector
		}
	}
	fields, members := sqlReadFields(parsed, file)
	if members {
		closure.Kind, closure.Selector = SQLClosureSnapshot, ""
	}
	for _, field := range fields {
		switch {
		case field == "symbol.test_coverage" || field == "symbol.test_coverage_generation":
			return SQLClosure{Kind: SQLClosureLive}
		case field == "snapshot" || field == "symbol.generation":
			closure.Snapshots = true
		case !sqlIdentityFields[field]:
			closure.Document = true
		}
	}
	if closure.Snapshots {
		closure.Kind, closure.Selector, closure.Document = SQLClosureSnapshot, "", false
	}
	if closure.Kind != SQLClosureSubject {
		closure.Document = false
	}
	return closure
}

// sqlReadFields lists the fields a query reads, with a source alias resolved, and whether it has a
// membership condition.
func sqlReadFields(parsed *sqlsyntax.Query, file bool) ([]string, bool) {
	var fields []string
	resolve := func(name string) string {
		if parsed.Alias != "" && strings.HasPrefix(name, parsed.Alias+".") {
			name = strings.TrimPrefix(name, parsed.Alias+".")
			if file {
				if _, known := sqlFileFields[name]; !known {
					name = "file." + name
				}
				return name
			}
			return "symbol.props." + name
		}
		return name
	}
	columns := map[string]string{}
	if parsed.Projection.Wildcard {
		wildcard := sqlWildcardFields
		if file {
			wildcard = sqlFileWildcardFields
		}
		fields = append(fields, wildcard...)
	}
	for _, column := range parsed.Projection.Columns {
		field := resolve(column.Field)
		fields = append(fields, field)
		if column.Alias != "" {
			columns[column.Alias] = field
		}
	}
	for _, order := range parsed.Order {
		if field, projected := columns[order.Field]; projected {
			fields = append(fields, field)
		} else {
			fields = append(fields, resolve(order.Field))
		}
	}
	members := false
	var visit func(*sqlsyntax.Predicate)
	visit = func(predicate *sqlsyntax.Predicate) {
		if predicate == nil {
			return
		}
		for _, term := range predicate.Terms {
			for _, condition := range term.Terms {
				for condition.Not != nil {
					condition = condition.Not
				}
				if condition.Group != nil {
					visit(condition.Group)
					continue
				}
				members = members || condition.Comparison.Member != nil
				fields = append(fields, resolve(condition.Comparison.Field))
			}
		}
	}
	visit(parsed.Where)
	return fields, members
}

// SQLSubjects restricts a row visit to some subjects of a subject or file closure, and reports which of
// those symbols the query's source selected before its WHERE clause.
type SQLSubjects struct {
	// Symbols are the symbol ids to keep; a row without a symbol is always kept. Nil keeps every row.
	Symbols map[string]bool
	// Files are the files to keep, each its snapshot id, a NUL and its path, for the files() source and
	// declarations of syntax analysis. Nil keeps every file.
	Files map[string]bool
	// Selected is called with the id of every kept symbol the source selected.
	Selected func(symbolID string)
}

func (subjects *SQLSubjects) keepSymbol(id string) bool {
	if subjects == nil || subjects.Symbols == nil || id == "" {
		return true
	}
	return subjects.Symbols[id]
}

func (subjects *SQLSubjects) selected(id string) {
	if subjects != nil && subjects.Selected != nil && id != "" {
		subjects.Selected(id)
	}
}

func (subjects *SQLSubjects) keepFile(snapshotID, path string) bool {
	return subjects == nil || subjects.Files == nil || subjects.Files[snapshotID+"\x00"+path]
}

// VisitSQLSubjects is VisitSQL restricted to subjects.
func (pipeline *Pipeline) VisitSQLSubjects(ctx context.Context, input string, scope ModuleScopeOptions, subjects *SQLSubjects, visit func(SQLRow) error) (ModuleQueryResult, error) {
	if visit == nil {
		return ModuleQueryResult{}, errors.New("SQL row visitor is required")
	}
	if err := ValidateSQLRows(input); err != nil {
		return ModuleQueryResult{}, err
	}
	return pipeline.executeSQL(ctx, input, scope, visit, subjects)
}

// SelectorKinds are the symbol kinds a source selector selects.
func (pipeline *Pipeline) SelectorKinds(ctx context.Context, text string) ([]symbolhandle.Kind, error) {
	kinds, err := storage.LoadSymbolKinds(ctx, pipeline.database)
	if err != nil {
		return nil, err
	}
	selector, err := sqlSelector(text, kinds)
	if err != nil {
		return nil, err
	}
	names, err := selectorKinds(selector, kinds)
	if err != nil {
		return nil, err
	}
	codes := make([]symbolhandle.Kind, 0, len(names))
	for _, name := range names {
		spec, err := kinds.Lookup(name)
		if err != nil {
			return nil, err
		}
		codes = append(codes, spec.Code)
	}
	return codes, nil
}

// ScopeCoverage lists the packages of a scope that are not fully indexed.
func (pipeline *Pipeline) ScopeCoverage(ctx context.Context, options ModuleScopeOptions) ([]ModuleCoverage, error) {
	selection, err := pipeline.moduleScopes(ctx, options, false)
	if err != nil {
		return nil, err
	}
	return pipeline.scopeCoverage(ctx, selection.scopes)
}
