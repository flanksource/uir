package storage

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/flanksource/uir"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DocumentContents loads the content of the given documents; an unknown id is an error.
func DocumentContents(ctx context.Context, database *gorm.DB, ids []uuid.UUID) (map[uuid.UUID]JSON, error) {
	return lookupSurrogates[uuid.UUID, JSON](ctx, database, Document{}.TableName(), "id", "content", "document id", ids)
}

// DefinitionPostings lists the definition postings of the given documents, sorted by document ordinal
// then symbol handle: a prefix scan of the posting primary key per batch.
func DefinitionPostings(ctx context.Context, database *gorm.DB, ordinals []int64) ([]SymbolPosting, error) {
	if database == nil {
		return nil, fmt.Errorf("load definition postings: database is required")
	}
	var postings []SymbolPosting
	for start := 0; start < len(ordinals); start += surrogateLookupBatch {
		batch := ordinals[start:min(start+surrogateLookupBatch, len(ordinals))]
		var rows []SymbolPosting
		if err := database.WithContext(ctx).Where("document_ordinal IN ? AND role = ?", batch, RoleDefinition).
			Order("document_ordinal").Order("symbol_handle").Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load definition postings of %d documents: %w", len(batch), err)
		}
		postings = append(postings, rows...)
	}
	return postings, nil
}

// OwnedSymbol is a symbol row with the names of its owners, outermost first: a method's type, or a
// field's struct type followed by any enclosing fields.
type OwnedSymbol struct {
	Symbol
	Owners []string
}

// Identifier is the structured identifier the typed extractor gives this symbol's declaration, under
// the symbol's own package path; see DeclarationIdentifier.
func (symbol OwnedSymbol) Identifier() (uir.Identifier, error) {
	return DeclarationIdentifier(symbol.PackagePath, symbol.Symbol, symbol.Owners)
}

// DeclarationIdentifier is the one rule for the structured identifier of a typed declaration from its
// symbol row and owner names (outermost first), under packagePath: a method's Type is its owners
// joined with dots; a field's Type is its outermost owner and its Field the remaining owners and its
// name; a type sets Type, a func Method, and a var or const Field. A field without an owner is an
// error. The typed extractor uses it for declarations the AST extractor does not project, and diff
// readers use it to name a symbol from its row alone.
func DeclarationIdentifier(packagePath string, symbol Symbol, owners []string) (uir.Identifier, error) {
	identifier := uir.Identifier{Package: packagePath}
	switch symbol.Kind {
	case "method":
		identifier.Type, identifier.Method, identifier.NodeType = strings.Join(owners, "."), symbol.Name, uir.NodeTypeMethod
	case "field":
		if len(owners) == 0 {
			return uir.Identifier{}, fmt.Errorf("field symbol %s (%s) has no owner", symbol.ID, symbol.Name)
		}
		fields := append(slices.Clone(owners[1:]), symbol.Name)
		identifier.Type, identifier.Field, identifier.NodeType = owners[0], strings.Join(fields, "."), uir.NodeTypeField
	case "type":
		identifier.Type, identifier.NodeType = symbol.Name, uir.NodeTypeType
	case "func":
		identifier.Method, identifier.NodeType = symbol.Name, uir.NodeTypeMethod
	default:
		identifier.Field, identifier.NodeType = symbol.Name, uir.NodeTypePackageVariable
	}
	return identifier, nil
}

// OwnedSymbols loads the symbols of the given handles with their owner names, following owner_id
// until a row has none; an unknown handle or owner is an error.
func OwnedSymbols(ctx context.Context, database *gorm.DB, handles []int64) (map[int64]OwnedSymbol, error) {
	ids, err := SymbolIDs(ctx, database, handles)
	if err != nil {
		return nil, err
	}
	rows := map[string]Symbol{}
	pending := map[string]bool{}
	for _, id := range ids {
		pending[id] = true
	}
	for len(pending) > 0 {
		loaded, err := symbolRows(ctx, database, pending)
		if err != nil {
			return nil, err
		}
		pending = map[string]bool{}
		for _, row := range loaded {
			rows[row.ID] = row
		}
		for _, row := range loaded {
			if row.OwnerID == nil {
				continue
			}
			if _, known := rows[*row.OwnerID]; !known {
				pending[*row.OwnerID] = true
			}
		}
	}
	result := make(map[int64]OwnedSymbol, len(ids))
	for handle, id := range ids {
		owned := OwnedSymbol{Symbol: rows[id]}
		for owner := owned.OwnerID; owner != nil; owner = rows[*owner].OwnerID {
			owned.Owners = append([]string{rows[*owner].Name}, owned.Owners...)
		}
		result[handle] = owned
	}
	return result, nil
}

// symbolRows loads the rows of the given symbol ids in sorted batches; a missing id is an error.
func symbolRows(ctx context.Context, database *gorm.DB, pending map[string]bool) ([]Symbol, error) {
	ids := make([]string, 0, len(pending))
	for id := range pending {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var rows []Symbol
	for start := 0; start < len(ids); start += surrogateLookupBatch {
		batch := ids[start:min(start+surrogateLookupBatch, len(ids))]
		var loaded []Symbol
		if err := database.WithContext(ctx).Where("id IN ?", batch).Find(&loaded).Error; err != nil {
			return nil, fmt.Errorf("load %d symbols: %w", len(batch), err)
		}
		if len(loaded) != len(batch) {
			return nil, fmt.Errorf("load %d symbols: found %d rows", len(batch), len(loaded))
		}
		rows = append(rows, loaded...)
	}
	return rows, nil
}
