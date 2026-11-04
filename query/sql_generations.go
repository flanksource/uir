package query

import (
	"context"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
)

// sqlRecordGenerations sets symbol.generation on each record from the symbol deltas of the snapshot
// that declares its symbol, without reading documents: the content generation of the symbol's newest
// set along that snapshot's base chain. A symbol no scope declares, such as one outside the indexed
// roots, reads NULL.
func (index *compactIndex) sqlRecordGenerations(ctx context.Context, records []sqlRecord) error {
	ids := map[string]bool{}
	for i := range records {
		records[i].values["symbol.generation"] = nil
		if id, ok := records[i].values["symbol.id"].(string); ok && id != "" {
			if _, known := index.symbols.handle(id); known {
				ids[id] = true
			}
		}
	}
	postings, err := index.postings(ctx, sortedKeys(ids), "definition")
	if err != nil {
		return err
	}
	byID := map[string][]scopedPosting{}
	for _, posting := range postings {
		byID[posting.symbol] = append(byID[posting.symbol], posting)
	}
	declaring := map[int]uuid.UUID{}
	handles := map[uuid.UUID][]int64{}
	for i, record := range records {
		id, _ := record.values["symbol.id"].(string)
		if len(byID[id]) == 0 {
			continue
		}
		posting, err := index.selectDeclaration(byID[id], record.match)
		if err != nil {
			return err
		}
		snapshot := index.scopes[posting.scope].snapshot.ID
		handle, _ := index.symbols.handle(id)
		declaring[i] = snapshot
		handles[snapshot] = append(handles[snapshot], handle)
	}
	generations := map[uuid.UUID]map[int64]storage.SymbolGeneration{}
	for snapshot, asked := range handles {
		if generations[snapshot], err = storage.SymbolGenerations(ctx, index.database, snapshot, asked); err != nil {
			return err
		}
	}
	for i, snapshot := range declaring {
		handle, _ := index.symbols.handle(records[i].values["symbol.id"].(string))
		if generation, found := generations[snapshot][handle]; found && generation.ContentFP != nil {
			records[i].values["symbol.generation"] = int(generation.Generation)
		}
	}
	return nil
}
