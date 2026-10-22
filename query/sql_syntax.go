package query

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/query/sqlsyntax"
	"github.com/flanksource/uir/storage"
)

func (pipeline *Pipeline) sqlSyntaxRows(ctx context.Context, parsed *sqlsyntax.Query, scopes []moduleScope, rows sqlRows, limit int, result ModuleQueryResult) (ModuleQueryResult, error) {
	selector, err := parseSelector(parsed.Source.Selector)
	if err != nil {
		return result, err
	}
	compiled, err := compileSelector(selector, nil)
	if err != nil {
		return result, err
	}
	rows.predicate, err = rows.bindPredicate(ctx, nil, parsed.Where)
	if err != nil {
		return result, err
	}
	var records []sqlRecord
	index, err := newIndexContext(ctx, pipeline, scopes)
	if err != nil {
		return result, err
	}
	postings := index.scopeDocumentPostings()
	kinds, err := selectorKinds(selector, index.kinds)
	if err != nil {
		return result, err
	}
	if err := index.prefetch(ctx, postings); err != nil {
		return result, err
	}
	for _, posting := range postings {
		scope := index.scopes[posting.scope]
		document, err := index.document(ctx, posting)
		if err != nil {
			return result, err
		}
		if document.coverage == storage.CoverageExcluded {
			continue
		}
		content := document.content
		for _, declaration := range content.Symbols {
			if !slices.Contains(kinds, declaration.Kind) {
				continue
			}
			name := declaration.Identifier.GetName()
			qualified := name
			if declaration.Kind == "method" && declaration.Identifier.Type != "" {
				qualified = declaration.Identifier.Type + "." + name
			}
			key := name
			if !selector.NameOnly && strings.ContainsAny(selector.Pattern, "./") {
				key = qualified
				if strings.Contains(selector.Pattern, "/") {
					key = content.PackagePath + "." + qualified
				}
			}
			if !compiled.glob.matches(key) {
				continue
			}
			if declaration.Metrics == nil {
				return result, fmt.Errorf("source %s declaration %s has no metrics", document.path, name)
			}
			match := ModuleMatch{Kind: "symbol", Role: "definition", NodeKind: declaration.Kind, RootKey: scope.root.RootKey, Location: scope.location.CanonicalPath, SnapshotID: scope.snapshot.ID.String(), Path: document.path, PackagePath: content.PackagePath, Identifier: declaration.Identifier, Coverage: string(document.coverage)}
			match.Line, match.Column, match.EndLine, match.EndColumn = rangePosition(declaration.Name)
			symbol := ModuleSymbol{Name: name, QueryName: content.PackagePath + "." + qualified, Kind: declaration.Kind, ModuleKey: scope.root.RootKey, PackagePath: content.PackagePath}
			if declaration.ID != nil {
				symbol.ID = *declaration.ID
			}
			values := map[string]any{"path": match.Path, "line": *match.Line, "column": *match.Column, "package": match.PackagePath, "module": match.RootKey, "snapshot": match.SnapshotID, "role": "definition",
				"symbol.kind": symbol.Kind, "symbol.name": name, "symbol.query_name": symbol.QueryName, "symbol.module": symbol.ModuleKey, "symbol.package": symbol.PackagePath,
				"symbol.metrics.param_count": declaration.Metrics.ParamCount, "symbol.metrics.body_lines": nil}
			if declaration.Metrics.BodyLines != nil {
				values["symbol.metrics.body_lines"] = *declaration.Metrics.BodyLines
			}
			declarationKey := fmt.Sprintf("%s:%s:%d:%d", match.RootKey, match.Path, declaration.NameBytes[0], declaration.NameBytes[1])
			symbol.DeclarationKey = declarationKey
			records = append(records, sqlRecord{match: match, witness: &symbol, values: values})
		}
	}
	sortSQLRecords(records)
	err = rows.project(records, limit, &result)
	return result, err
}
