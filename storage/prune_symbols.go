package storage

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// deleteOrphanSymbols deletes the candidate symbols that no posting and no symbol delta names, except an
// owner of a symbol that stays, such as a record whose field another document still reads. A symbol is
// deleted after every orphan it owns, since symbols.owner_id references it.
func deleteOrphanSymbols(ctx context.Context, tx *gorm.DB, candidates []int64, result *PruneResult) error {
	orphans, err := unreferencedSymbols(ctx, tx, candidates)
	if err != nil {
		return err
	}
	if err := keepOwners(ctx, tx, orphans); err != nil {
		return err
	}
	for len(orphans) > 0 {
		owners := map[string]bool{}
		for _, orphan := range orphans {
			if orphan.OwnerID != nil {
				owners[*orphan.OwnerID] = true
			}
		}
		var leaves []string
		for id := range orphans {
			if !owners[id] {
				leaves = append(leaves, id)
			}
		}
		if len(leaves) == 0 {
			return fmt.Errorf("the %d orphan symbols own one another in a cycle", len(orphans))
		}
		if err := inBatches(leaves, func(batch []string) error {
			deleted := tx.WithContext(ctx).Where("id IN ?", batch).Delete(&Symbol{})
			if deleted.Error != nil {
				return fmt.Errorf("delete orphan symbols: %w", deleted.Error)
			}
			result.Symbols += deleted.RowsAffected
			return nil
		}); err != nil {
			return err
		}
		for _, id := range leaves {
			delete(orphans, id)
		}
	}
	return nil
}

// unreferencedSymbols loads, by id, the symbols of handles that no posting and no symbol delta names.
func unreferencedSymbols(ctx context.Context, tx *gorm.DB, handles []int64) (map[string]Symbol, error) {
	orphans := map[string]Symbol{}
	err := inBatches(handles, func(batch []int64) error {
		referenced := map[int64]bool{}
		if err := collectHandles(ctx, tx, "symbol_postings", "symbol_handle", batch, referenced); err != nil {
			return err
		}
		if err := collectHandles(ctx, tx, "symbol_deltas", "symbol_handle", batch, referenced); err != nil {
			return err
		}
		var unreferenced []int64
		for _, handle := range batch {
			if !referenced[handle] {
				unreferenced = append(unreferenced, handle)
			}
		}
		if len(unreferenced) == 0 {
			return nil
		}
		var rows []Symbol
		if err := tx.WithContext(ctx).Select("id", "owner_id", "handle").Where("handle IN ?", unreferenced).Find(&rows).Error; err != nil {
			return fmt.Errorf("load orphan symbols: %w", err)
		}
		for _, row := range rows {
			orphans[row.ID] = row
		}
		return nil
	})
	return orphans, err
}

// keepOwners drops from orphans, until none is dropped, every orphan that owns a symbol which stays.
func keepOwners(ctx context.Context, tx *gorm.DB, orphans map[string]Symbol) error {
	for {
		dropped := false
		if err := inBatches(sortedSet(keySet(orphans)), func(batch []string) error {
			var owned []Symbol
			if err := tx.WithContext(ctx).Select("id", "owner_id").Where("owner_id IN ?", batch).Find(&owned).Error; err != nil {
				return fmt.Errorf("load the symbols orphans own: %w", err)
			}
			for _, symbol := range owned {
				if _, doomed := orphans[symbol.ID]; doomed {
					continue
				}
				if _, doomed := orphans[*symbol.OwnerID]; doomed {
					delete(orphans, *symbol.OwnerID)
					dropped = true
				}
			}
			return nil
		}); err != nil || !dropped {
			return err
		}
	}
}

func keySet[V any](values map[string]V) map[string]bool {
	keys := make(map[string]bool, len(values))
	for key := range values {
		keys[key] = true
	}
	return keys
}
