package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/flanksource/uir/storage"
)

// SuggestSymbols returns active canonical symbols whose qualified spelling starts with a partial
// subject. The subject is literal text, such as the unquoted content of a quoted symbol, so it may
// contain spaces and punctuation but no wildcard.
func (pipeline *Pipeline) SuggestSymbols(ctx context.Context, prefix string, options ModuleScopeOptions) (ItemsWithWarnings[ModuleSymbol], error) {
	if pipeline == nil || pipeline.database == nil {
		return ItemsWithWarnings[ModuleSymbol]{}, fmt.Errorf("UIR query database is required")
	}
	if !partialSymbol.MatchString(prefix) || strings.Contains(prefix, "..") || len(prefix) > maximumSelectorPattern {
		return ItemsWithWarnings[ModuleSymbol]{}, fmt.Errorf("invalid partial symbol %q: expected literal text without a wildcard, leading whitespace, or an empty name part", prefix)
	}
	if options.Limit == 0 {
		options.Limit = 20
	}
	if options.Limit < 1 || options.Limit > 100 {
		return ItemsWithWarnings[ModuleSymbol]{}, fmt.Errorf("suggestion limit must be between 1 and 100, got %d", options.Limit)
	}
	if options.Location != "" && options.SnapshotID != "" {
		return ItemsWithWarnings[ModuleSymbol]{}, fmt.Errorf("location and snapshot selectors are mutually exclusive")
	}
	session, _ := pipeline.session()
	selection, err := session.moduleScopes(ctx, options, false)
	if err != nil {
		return ItemsWithWarnings[ModuleSymbol]{}, err
	}
	if len(selection.scopes) == 0 {
		return ItemsWithWarnings[ModuleSymbol]{Items: []ModuleSymbol{}, Warnings: selection.warnings}, nil
	}
	base, err := newIndexContext(ctx, session, selection.scopes)
	if err != nil {
		return ItemsWithWarnings[ModuleSymbol]{}, err
	}
	index := newCompactIndex(base)
	matched, err := index.symbolsWithPrefix(ctx, prefix)
	if err != nil {
		return ItemsWithWarnings[ModuleSymbol]{}, err
	}
	symbols, err := index.activeSymbols(ctx, matched)
	if err != nil {
		return ItemsWithWarnings[ModuleSymbol]{}, err
	}
	if len(symbols) > options.Limit {
		symbols = symbols[:options.Limit]
	}
	if symbols == nil {
		symbols = []ModuleSymbol{}
	}
	return ItemsWithWarnings[ModuleSymbol]{Items: symbols, Warnings: selection.warnings}, nil
}

// partialSymbol is a partial symbol spelling: printable text that starts with no whitespace and has
// no wildcard.
var partialSymbol = regexp.MustCompile(`^[^\s*?[:cntrl:]][^*?[:cntrl:]]*$`)

// symbolsWithPrefix loads the symbols whose qualified spelling, short spelling, or a member of it
// starts with a partial subject, reading at most 10000 candidates by the search name of its last part.
func (index *compactIndex) symbolsWithPrefix(ctx context.Context, prefix string) ([]storage.Symbol, error) {
	last := prefix[max(strings.LastIndex(prefix, "."), strings.LastIndex(prefix, "/"))+1:]
	query := index.database.WithContext(ctx).Model(&storage.Symbol{})
	if last != "" {
		query = query.Where("search_name LIKE ?", storage.SearchName(last)+"%")
	}
	var rows []storage.Symbol
	if err := query.Order("package_path, name, id").Limit(10001).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load symbol suggestions: %w", err)
	}
	if len(rows) > 10000 {
		return nil, fmt.Errorf("symbol prefix %q is too broad; type more characters", prefix)
	}
	if err := index.prefetchOwners(ctx, rows); err != nil {
		return nil, err
	}
	matched := make([]storage.Symbol, 0, len(rows))
	for _, row := range rows {
		name, err := index.queryName(ctx, row)
		if err != nil {
			return nil, err
		}
		short := name[strings.LastIndex(name, "/")+1:]
		if strings.HasPrefix(name, prefix) || strings.HasPrefix(short, prefix) || strings.Contains(short, "."+prefix) {
			matched = append(matched, row)
		}
	}
	return matched, nil
}
