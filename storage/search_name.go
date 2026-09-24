package storage

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// searchAlphabet is the alphabet of symbols.search_name in ascending order.
const searchAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// SearchName is a symbol name lowercased and restricted to [a-z0-9]; it is empty when the name has
// none of those characters.
func SearchName(name string) string {
	var search strings.Builder
	for _, character := range strings.ToLower(name) {
		if strings.ContainsRune(searchAlphabet, character) {
			search.WriteRune(character)
		}
	}
	return search.String()
}

// SearchPrefixUpperBound is the least string of the search alphabet greater than every string that
// starts with prefix: the last character below z increments within 0-9 then a-z, and the z characters
// after it are dropped. A prefix of only z characters has no upper bound.
func SearchPrefixUpperBound(prefix string) (string, bool, error) {
	if prefix == "" {
		return "", false, fmt.Errorf("search prefix is empty")
	}
	for _, character := range prefix {
		if !strings.ContainsRune(searchAlphabet, character) {
			return "", false, fmt.Errorf("search prefix %q contains %q, which is not in [a-z0-9]", prefix, character)
		}
	}
	for end := len(prefix) - 1; end >= 0; end-- {
		if prefix[end] != 'z' {
			return prefix[:end] + string(searchAlphabet[strings.IndexByte(searchAlphabet, prefix[end])+1]), true, nil
		}
	}
	return "", false, nil
}

// UpsertSymbols inserts symbol rows and, for an id that already exists, refreshes the columns derived
// from the name and declaration: search_name and visibility. The identity columns stay as stored, so
// callers still verify them against the rows they expected.
func UpsertSymbols(ctx context.Context, database *gorm.DB, rows []Symbol, batchSize int) error {
	upsert := clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"search_name", "visibility"}),
	}
	if err := database.WithContext(ctx).Clauses(upsert).CreateInBatches(rows, batchSize).Error; err != nil {
		return fmt.Errorf("upsert %d symbols: %w", len(rows), err)
	}
	return nil
}
