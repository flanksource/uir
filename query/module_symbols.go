package query

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage"
)

type ModuleSymbol struct {
	ID             string       `json:"id"`
	ModuleKey      string       `json:"module_key"`
	PackagePath    string       `json:"package_path"`
	Kind           string       `json:"kind"`
	OwnerID        string       `json:"owner_id,omitempty"`
	Owner          string       `json:"owner,omitempty"`
	Name           string       `json:"name"`
	QueryName      string       `json:"query_name"`
	Visibility     string       `json:"visibility"`
	ParameterTypes storage.JSON `json:"parameter_types"`
}

func (index *indexContext) moduleSymbols(ctx context.Context, rows []storage.Symbol) ([]ModuleSymbol, error) {
	owners := map[string]string{}
	var ownerIDs []string
	for _, row := range rows {
		if err := index.register(ctx, row.Kind); err != nil {
			return nil, fmt.Errorf("symbol %s: %w", row.ID, err)
		}
		if row.OwnerID == nil {
			continue
		}
		if owner, kept := index.symbols.row(*row.OwnerID); kept {
			owners[owner.ID] = owner.Name
		} else {
			ownerIDs = append(ownerIDs, *row.OwnerID)
		}
	}
	for start := 0; start < len(ownerIDs); start += lookupBatch {
		var found []storage.Symbol
		if err := index.database.WithContext(ctx).Where("id IN ?", ownerIDs[start:min(start+lookupBatch, len(ownerIDs))]).Find(&found).Error; err != nil {
			return nil, fmt.Errorf("load symbol owners: %w", err)
		}
		if err := index.remember(found); err != nil {
			return nil, err
		}
		for _, owner := range found {
			owners[owner.ID] = owner.Name
		}
	}
	symbols := make([]ModuleSymbol, 0, len(rows))
	for _, row := range rows {
		symbol := ModuleSymbol{
			ID: row.ID, ModuleKey: row.ModuleKey, PackagePath: row.PackagePath, Kind: row.Kind, Name: row.Name,
			Visibility: row.Visibility, ParameterTypes: row.ParameterTypes,
		}
		if row.OwnerID != nil {
			name, found := owners[*row.OwnerID]
			if !found {
				return nil, fmt.Errorf("symbol %s names owner %s, which is not a symbol", row.ID, *row.OwnerID)
			}
			symbol.OwnerID, symbol.Owner = *row.OwnerID, name
		}
		symbols = append(symbols, symbol)
	}
	return symbols, nil
}

func symbolIDs(rows []storage.Symbol) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func sameParameters(left, right storage.JSON) (bool, error) {
	var leftTypes, rightTypes []string
	if err := json.Unmarshal(left, &leftTypes); err != nil {
		return false, fmt.Errorf("decode parameter types %s: %w", left, err)
	}
	if err := json.Unmarshal(right, &rightTypes); err != nil {
		return false, fmt.Errorf("decode parameter types %s: %w", right, err)
	}
	return slices.Equal(leftTypes, rightTypes), nil
}
