package indexer

import (
	"fmt"
	"strings"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

// declaredKinds is the registry a publication is validated against before anything is written: the
// database's kinds and each kind the publication declares that the database lacks, on a provisional
// free custom code (validation reads only names and categories; registration assigns the real code). A
// declared kind registered under another category is an error, as registration would make it.
func (publication Publication) declaredKinds(registered symbolhandle.Kinds) (symbolhandle.Kinds, error) {
	var custom []symbolhandle.KindSpec
	used := map[symbolhandle.Kind]bool{}
	for _, spec := range registered.All() {
		if spec.Code > symbolhandle.MaxBuiltinKind {
			custom, used[spec.Code] = append(custom, spec), true
		}
	}
	next := symbolhandle.MaxKind
	for _, kind := range publication.Kinds {
		if spec, err := registered.Lookup(kind.Name); err == nil {
			if spec.Category != kind.Category {
				return symbolhandle.Kinds{}, fmt.Errorf("kind %q is registered as %s, not %s", kind.Name, spec.Category, kind.Category)
			}
			continue
		}
		for used[next] && next > symbolhandle.MaxBuiltinKind {
			next--
		}
		if next <= symbolhandle.MaxBuiltinKind {
			return symbolhandle.Kinds{}, fmt.Errorf("kind %q: no custom symbol kind code is free", kind.Name)
		}
		custom, used[next] = append(custom, symbolhandle.KindSpec{Code: next, Name: kind.Name, Category: kind.Category}), true
	}
	return symbolhandle.NewKinds(custom)
}

// symbolRows is the symbols row of every published symbol, by id. Each kind must be in kinds and each
// owner must be published too, so owners are written before the symbols they own.
func (publication Publication) symbolRows(kinds symbolhandle.Kinds) (map[string]storage.Symbol, error) {
	rows := make(map[string]storage.Symbol, len(publication.Symbols))
	ordered := make([]storage.Symbol, 0, len(publication.Symbols))
	for _, symbol := range publication.Symbols {
		row, err := symbolRow(symbol.Identity, symbol.Visibility)
		if err != nil {
			return nil, err
		}
		if _, listed := rows[row.ID]; listed {
			return nil, fmt.Errorf("symbol %s (%s) is listed twice", publishedName(rows, row), row.ID)
		}
		rows[row.ID] = row
		ordered = append(ordered, row)
	}
	for _, row := range ordered {
		subject := fmt.Sprintf("symbol %s (%s)", publishedName(rows, row), row.ID)
		if _, err := kinds.Lookup(row.Kind); err != nil {
			return nil, fmt.Errorf("%s: %w", subject, err)
		}
		if _, err := symbolhandle.ParseVisibility(row.Visibility); err != nil {
			return nil, fmt.Errorf("%s: %w", subject, err)
		}
		switch {
		case row.Name == "":
			return nil, fmt.Errorf("%s: name is required", subject)
		case row.OwnerID != nil && rows[*row.OwnerID].ID == "":
			return nil, fmt.Errorf("%s: owner %s is not among the publication's symbols", subject, *row.OwnerID)
		}
	}
	return rows, nil
}

// publishedName is how errors name a published symbol: its package, its owners, and its name.
func publishedName(rows map[string]storage.Symbol, row storage.Symbol) string {
	names := []string{row.Name}
	for owner := row.OwnerID; owner != nil && rows[*owner].ID != ""; owner = rows[*owner].OwnerID {
		names = append([]string{rows[*owner].Name}, names...)
	}
	return row.PackagePath + "." + strings.Join(names, ".")
}
