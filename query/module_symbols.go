package query

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/flanksource/uir"
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

func (symbol ModuleSymbol) identifier() uir.Identifier {
	identifier := uir.Identifier{Module: symbol.ModuleKey, Package: symbol.PackagePath}
	switch symbol.Kind {
	case "type":
		identifier.Type, identifier.NodeType = symbol.Name, uir.NodeTypeType
	case "func":
		identifier.Method, identifier.NodeType = symbol.Name, uir.NodeTypeMethod
	case "method":
		identifier.Type, identifier.Method, identifier.NodeType = symbol.Owner, symbol.Name, uir.NodeTypeMethod
	case "field", "var", "const":
		identifier.Type, identifier.Field, identifier.NodeType = symbol.Owner, symbol.Name, uir.NodeTypeField
	default:
		identifier.NodeType = uir.NodeTypePackage
	}
	return identifier
}

func (index *indexContext) moduleSymbols(ctx context.Context, rows []storage.Symbol) ([]ModuleSymbol, error) {
	var ownerIDs []string
	for _, row := range rows {
		if row.OwnerID != nil {
			ownerIDs = append(ownerIDs, *row.OwnerID)
		}
	}
	owners := map[string]string{}
	for start := 0; start < len(ownerIDs); start += lookupBatch {
		var found []storage.Symbol
		if err := index.database.WithContext(ctx).Select("id", "name").Where("id IN ?", ownerIDs[start:min(start+lookupBatch, len(ownerIDs))]).Find(&found).Error; err != nil {
			return nil, fmt.Errorf("load symbol owners: %w", err)
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
