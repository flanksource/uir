package query

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir/storage"
)

// SymbolSelector identifies one Go declaration independently of its index ID.
type SymbolSelector struct {
	ModuleKey   string
	PackagePath string
	Kind        string
	Owner       string
	Name        string
}

// ReferenceRequest scopes exact symbol references to published snapshots.
type ReferenceRequest struct {
	SnapshotIDs []string
	Symbols     []SymbolSelector
}

// ReferenceLocation is an indexed Go identifier in the selected checkout.
type ReferenceLocation struct {
	SymbolID    string
	Location    string
	SnapshotID  string
	PackagePath string
	Path        string
	SourceHash  string
	StartByte   int
	EndByte     int
	Role        string
}

// FindReferences returns every indexed definition and use of the exact selected symbols.
// Unlike RunModules, it does not impose an interactive result limit.
func (pipeline *Pipeline) FindReferences(ctx context.Context, request ReferenceRequest) ([]ReferenceLocation, error) {
	if pipeline == nil || pipeline.database == nil {
		return nil, fmt.Errorf("UIR query database is required")
	}
	if len(request.SnapshotIDs) == 0 || len(request.Symbols) == 0 {
		return nil, fmt.Errorf("reference search requires snapshots and symbols")
	}
	var scopes []moduleScope
	for _, id := range request.SnapshotIDs {
		selected, err := pipeline.moduleScopes(ctx, ModuleScopeOptions{SnapshotID: id}, false)
		if err != nil {
			return nil, err
		}
		scopes = append(scopes, selected...)
	}
	index, err := newIndexContext(ctx, pipeline.database, scopes)
	if err != nil {
		return nil, err
	}
	ids, err := pipeline.referenceSymbolIDs(ctx, request.Symbols)
	if err != nil {
		return nil, err
	}
	postings, err := index.postings(ctx, ids, "reference")
	if err != nil {
		return nil, err
	}
	return index.referenceLocations(postings)
}

func (pipeline *Pipeline) referenceSymbolIDs(ctx context.Context, selectors []SymbolSelector) ([]string, error) {
	ids := map[string]bool{}
	for _, selector := range selectors {
		if selector.PackagePath == "" || selector.Kind == "" || selector.Name == "" {
			return nil, fmt.Errorf("reference symbol requires package path, kind, and name: %+v", selector)
		}
		query := pipeline.database.WithContext(ctx).Where("package_path = ? AND kind = ? AND name = ?", selector.PackagePath, selector.Kind, selector.Name)
		if selector.ModuleKey != "" {
			query = query.Where("module_key = ?", selector.ModuleKey)
		}
		var rows []storage.Symbol
		if err := query.Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("resolve indexed symbol %s.%s: %w", selector.PackagePath, selector.Name, err)
		}
		for _, row := range rows {
			owner := ""
			if row.OwnerID != nil {
				var parent storage.Symbol
				if err := pipeline.database.WithContext(ctx).Where("id = ?", *row.OwnerID).First(&parent).Error; err != nil {
					return nil, fmt.Errorf("resolve owner of symbol %s: %w", row.ID, err)
				}
				owner = parent.Name
			}
			if owner == selector.Owner {
				ids[row.ID] = true
			}
		}
	}
	selected := make([]string, 0, len(ids))
	for id := range ids {
		selected = append(selected, id)
	}
	slices.Sort(selected)
	return selected, nil
}

func (index *indexContext) referenceLocations(postings []scopedPosting) ([]ReferenceLocation, error) {
	byPosition := map[string]ReferenceLocation{}
	for _, posting := range postings {
		document, err := index.document(posting)
		if err != nil {
			return nil, err
		}
		scope := index.scopes[posting.scope]
		active := scope.documents[posting.document]
		for _, occurrence := range document.content.Occurrences {
			if occurrence.Symbol == nil || *occurrence.Symbol != posting.symbol {
				continue
			}
			location := ReferenceLocation{
				SymbolID: posting.symbol, Location: scope.location.CanonicalPath, SnapshotID: scope.snapshot.ID.String(),
				PackagePath: document.content.PackagePath, Path: document.path, SourceHash: active.Source.ContentHash,
				StartByte: occurrence.Bytes[0], EndByte: occurrence.Bytes[1], Role: occurrence.Role,
			}
			key := strings.Join([]string{location.Location, location.Path, fmt.Sprint(location.StartByte), fmt.Sprint(location.EndByte)}, "\x00")
			if previous, found := byPosition[key]; !found || location.Role == "definition" && previous.Role != "definition" {
				byPosition[key] = location
			}
		}
	}
	locations := make([]ReferenceLocation, 0, len(byPosition))
	for _, location := range byPosition {
		locations = append(locations, location)
	}
	slices.SortFunc(locations, func(a, b ReferenceLocation) int {
		return strings.Compare(fmt.Sprintf("%s\x00%s\x00%012d", a.Location, a.Path, a.StartByte),
			fmt.Sprintf("%s\x00%s\x00%012d", b.Location, b.Path, b.StartByte))
	})
	return locations, nil
}
