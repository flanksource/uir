package query

import (
	"context"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
)

// recordGeneration is the generation a record's symbol has in the snapshot that declares it.
type recordGeneration struct {
	snapshot   uuid.UUID
	handle     int64
	generation storage.SymbolGeneration
}

// recordGenerations resolves each record whose symbol a selected snapshot declares to that symbol's
// newest set along the declaring snapshot's base chain, by record index, without reading documents.
func (index *compactIndex) recordGenerations(ctx context.Context, records []sqlRecord) (map[int]recordGeneration, error) {
	ids := map[string]bool{}
	for i := range records {
		if id, ok := records[i].values["symbol.id"].(string); ok && id != "" {
			if _, known := index.symbols.handle(id); known {
				ids[id] = true
			}
		}
	}
	postings, err := index.postings(ctx, sortedKeys(ids), "definition")
	if err != nil {
		return nil, err
	}
	byID := map[string][]scopedPosting{}
	for _, posting := range postings {
		byID[posting.symbol] = append(byID[posting.symbol], posting)
	}
	resolved := map[int]recordGeneration{}
	handles := map[uuid.UUID][]int64{}
	for i, record := range records {
		id, _ := record.values["symbol.id"].(string)
		if len(byID[id]) == 0 {
			continue
		}
		posting, err := index.selectDeclaration(byID[id], record.match)
		if err != nil {
			return nil, err
		}
		snapshot := index.scopes[posting.scope].snapshot.ID
		handle, _ := index.symbols.handle(id)
		resolved[i] = recordGeneration{snapshot: snapshot, handle: handle}
		handles[snapshot] = append(handles[snapshot], handle)
	}
	generations := map[uuid.UUID]map[int64]storage.SymbolGeneration{}
	for snapshot, asked := range handles {
		if generations[snapshot], err = storage.SymbolGenerations(ctx, index.database, snapshot, asked); err != nil {
			return nil, err
		}
	}
	for i, record := range resolved {
		generation, found := generations[record.snapshot][record.handle]
		if !found || generation.ContentFP == nil {
			delete(resolved, i)
			continue
		}
		record.generation = generation
		resolved[i] = record
	}
	return resolved, nil
}

// sqlRecordGenerations sets symbol.generation on each record from the symbol deltas of the snapshot
// that declares its symbol. A symbol no scope declares, such as one outside the indexed roots, reads
// NULL.
func (index *compactIndex) sqlRecordGenerations(ctx context.Context, records []sqlRecord) error {
	resolved, err := index.recordGenerations(ctx, records)
	if err != nil {
		return err
	}
	for i := range records {
		records[i].values["symbol.generation"] = nil
		if record, found := resolved[i]; found {
			records[i].values["symbol.generation"] = int(record.generation.Generation)
		}
	}
	return nil
}

// coverageRow is the newest aggregate coverage of one symbol whose content fingerprint matches.
type coverageRow struct {
	SymbolHandle      int64
	ContentFP         int64
	ContentGeneration int64
	Statements        int
	Covered           int
}

// sqlRecordCoverage sets symbol.test_coverage, the share of the symbol's statements the newest run's
// aggregate coverage executed, and symbol.test_coverage_generation, the generation it was measured at.
// Coverage is reused on a later snapshot only while the symbol's content fingerprint there equals the
// one it was measured at; a symbol without such coverage reads NULL.
func (index *compactIndex) sqlRecordCoverage(ctx context.Context, records []sqlRecord) error {
	resolved, err := index.recordGenerations(ctx, records)
	if err != nil {
		return err
	}
	handles := map[int64]bool{}
	for _, record := range resolved {
		handles[record.handle] = true
	}
	measured := map[[2]int64]coverageRow{}
	asked := make([]int64, 0, len(handles))
	for handle := range handles {
		asked = append(asked, handle)
	}
	for start := 0; start < len(asked); start += 256 {
		var rows []coverageRow
		if err := index.database.WithContext(ctx).Table("test_coverage c").
			Select("c.symbol_handle, c.content_fp, c.content_generation, c.statements, c.covered").
			Joins("JOIN test_runs r ON r.id = c.run_id").
			Where("c.test_case_id IS NULL AND c.symbol_handle IN ?", asked[start:min(start+256, len(asked))]).
			Order("r.started_at DESC, r.id").Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			key := [2]int64{row.SymbolHandle, row.ContentFP}
			if _, newer := measured[key]; !newer {
				measured[key] = row
			}
		}
	}
	for i := range records {
		records[i].values["symbol.test_coverage"], records[i].values["symbol.test_coverage_generation"] = nil, nil
		record, found := resolved[i]
		if !found {
			continue
		}
		row, found := measured[[2]int64{record.handle, *record.generation.ContentFP}]
		if !found || row.Statements == 0 {
			continue
		}
		records[i].values["symbol.test_coverage"] = float64(row.Covered) / float64(row.Statements)
		records[i].values["symbol.test_coverage_generation"] = int(row.ContentGeneration)
	}
	return nil
}
