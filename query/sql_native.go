package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
)

func validateSQLCatalog(source *sqlSource, catalog storage.DomainCatalog) error {
	for _, name := range source.options.via {
		if _, ok := catalog.Relationships[name]; !ok {
			return sqlError(source.pos, "relationship kind %q is not registered", name)
		}
	}
	for _, argument := range source.arguments {
		if err := validateSQLCatalog(argument, catalog); err != nil {
			return err
		}
	}
	return nil
}

func (index *compactIndex) requireNativeFacts(ctx context.Context) error {
	postings := index.scopeDocumentPostings()
	if err := index.prefetch(ctx, postings); err != nil {
		return err
	}
	for _, posting := range postings {
		document, err := index.document(ctx, posting)
		if err != nil {
			return err
		}
		if document.coverage != storage.CoverageExcluded && document.content.Version < 4 {
			return fmt.Errorf("snapshot %s document %s has format %d without native relationship capability; reindex or reimport this snapshot using document format 4", index.scopes[posting.scope].snapshot.ID, document.path, document.content.Version)
		}
	}
	return nil
}

func (index *compactIndex) nativeMatches(ctx context.Context, targets []ModuleSymbol, incoming bool, via []string) ([]ModuleMatch, error) {
	if err := index.requireNativeFacts(ctx); err != nil {
		return nil, err
	}
	role := "relationship_source"
	if incoming {
		role = "relationship_target"
	}
	ids := map[string]bool{}
	for _, symbol := range targets {
		ids[symbol.ID] = true
	}
	postings, err := index.postings(ctx, sortedKeys(ids), role)
	if err != nil {
		return nil, err
	}
	if err := index.prefetch(ctx, postings); err != nil {
		return nil, err
	}
	var matches []ModuleMatch
	seen := map[string]bool{}
	for _, posting := range postings {
		key := fmt.Sprintf("%d:%s", posting.scope, posting.document)
		if seen[key] {
			continue
		}
		seen[key] = true
		document, err := index.document(ctx, posting)
		if err != nil {
			return nil, err
		}
		for _, relationship := range document.content.Relationships {
			if len(via) > 0 && !slices.Contains(via, relationship.Kind) {
				continue
			}
			if !incoming && !ids[relationship.Source] {
				continue
			}
			selected := relationship.Targets
			if len(selected) == 0 && !incoming {
				selected = []string{""}
			}
			for _, target := range selected {
				if incoming && !ids[target] {
					continue
				}
				match := index.match(posting, document, "relationship")
				match.Line, match.Column, match.EndLine, match.EndColumn = rangePosition(relationship.Evidence.Range)
				copy := relationship
				copy.Props = cloneProperties(relationship.Props)
				copy.Targets = slices.Clone(relationship.Targets)
				match.Relationship = &copy
				match.Role, match.Relation = relationship.Kind, relationship.Kind
				match.EnclosingID, match.SourceID, match.SymbolID = relationship.Source, relationship.Source, target
				match.span, match.text = relationship.Evidence.Bytes, relationship.Evidence.Text
				matches = append(matches, match)
			}
		}
	}
	sortMatches(matches)
	return matches, nil
}

func (index *compactIndex) sqlNativeStep(ctx context.Context, left compactValue, incoming bool, via []string, result *ModuleQueryResult) (compactValue, error) {
	matches, err := index.nativeMatches(ctx, left.symbols, incoming, via)
	if err != nil {
		return compactValue{}, err
	}
	relation := ""
	if incoming {
		relation = "<"
	}
	symbols, err := index.project(ctx, matches, relation, result)
	return compactValue{symbols: symbols, matches: matches}, err
}

func (index *compactIndex) nativePath(ctx context.Context, sources, targets []ModuleSymbol, options sqlOptions, result *ModuleQueryResult) (*CallPath, error) {
	goals := map[string]bool{}
	for _, target := range targets {
		goals[target.ID] = true
	}
	queue := make([]graphNode, 0, len(sources))
	seen := map[string]bool{}
	parents := map[string]graphParent{}
	for _, source := range sources {
		if seen[source.ID] {
			continue
		}
		if len(seen) == maxGraphSymbols {
			return nil, fmt.Errorf("native path exceeded %d symbols", maxGraphSymbols)
		}
		queue = append(queue, graphNode{symbol: source})
		seen[source.ID] = true
	}
	for position := 0; position < len(queue); position++ {
		current := queue[position]
		if goals[current.symbol.ID] {
			return index.buildPath(ctx, current.symbol.ID, parents)
		}
		if current.depth == options.depth {
			continue
		}
		step, err := index.sqlNativeStep(ctx, compactValue{symbols: []ModuleSymbol{current.symbol}}, false, options.via, result)
		if err != nil {
			return nil, err
		}
		for _, symbol := range step.symbols {
			if seen[symbol.ID] {
				continue
			}
			if len(seen) == maxGraphSymbols {
				return nil, fmt.Errorf("native path exceeded %d symbols", maxGraphSymbols)
			}
			seen[symbol.ID] = true
			for _, match := range step.matches {
				if match.SymbolID == symbol.ID {
					parents[symbol.ID] = graphParent{from: current.symbol.ID, call: match}
					break
				}
			}
			queue = append(queue, graphNode{symbol: symbol, depth: current.depth + 1})
		}
	}
	result.Stages = append(result.Stages, ResolutionStage{Name: "path", Value: fmt.Sprintf("no recorded witness within %d hops", options.depth)})
	return nil, nil
}
