package symboldiff

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

func snapshotRenames(ctx context.Context, database *gorm.DB, scope rootScope, files []changedFile, pairs []*pairing, sides [2]selectedSide) ([]*pairing, error) {
	if scope.external {
		return pairs, nil
	}
	groups := [2]map[string]bool{{}, {}}
	for _, pair := range pairs {
		if pair.class != ClassAdded && pair.class != ClassRemoved {
			continue
		}
		e := pair.reported()
		owner, _ := ownerAndName(e.symbol.Identifier)
		key := e.file.active.Source.PackagePath + "|" + e.symbol.Kind + "|" + owner
		side := 0
		if pair.class == ClassAdded {
			side = 1
		}
		groups[side][key] = true
	}
	candidates := map[*pairing]bool{}
	for _, pair := range pairs {
		if pair.class != ClassAdded && pair.class != ClassRemoved {
			continue
		}
		e := pair.reported()
		owner, _ := ownerAndName(e.symbol.Identifier)
		key := e.file.active.Source.PackagePath + "|" + e.symbol.Kind + "|" + owner
		if groups[0][key] && groups[1][key] {
			candidates[pair] = true
		}
	}
	if len(candidates) == 0 {
		return pairs, nil
	}
	unread := map[*fileSide]bool{}
	for _, pair := range pairs {
		if !candidates[pair] {
			continue
		}
		e := pair.reported()
		if strings.HasSuffix(e.file.path, ".go") && !e.decoded() {
			unread[e.file] = true
		}
	}
	if err := readDocuments(ctx, database, files, unread); err != nil {
		return nil, err
	}
	for _, pair := range pairs {
		if !candidates[pair] {
			continue
		}
		e := pair.reported()
		if !strings.HasSuffix(e.file.path, ".go") {
			continue
		}
		if !e.decoded() {
			for i, symbol := range e.file.content.Symbols {
				if symbol.ID != nil && e.symbol.ID != nil && *symbol.ID == *e.symbol.ID {
					e.symbol, e.index = symbol, i
					break
				}
			}
		}
		if !e.decoded() {
			return nil, fmt.Errorf("rename candidate %s has no decoded declaration", e.symbol.Key)
		}
		side := 0
		if pair.class == ClassAdded {
			side = 1
		}
		source := newBlobSource(ctx, database, scope, sides[side].commit)
		content, err := source.read(ctx, e.file)
		if err != nil {
			return nil, err
		}
		e.file.source = content
	}
	return pairRenames(files, pairs)
}

func snapshotPatch(ctx context.Context, database *gorm.DB, scope rootScope, files []changedFile, pairs []*pairing, markers coverageMarkers, sides [2]selectedSide, filter rowFilter) ([]byte, error) {
	if scope.external {
		return nil, scope.errExternal("patch output requires Git hunks")
	}
	for _, file := range files {
		for side, doc := range file.sides() {
			if doc == nil {
				continue
			}
			source := newBlobSource(ctx, database, scope, sides[side].commit)
			content, err := source.read(ctx, doc)
			if err != nil {
				return nil, err
			}
			doc.source = content
		}
	}
	checkout, err := scope.checkoutFor(ctx, sides[0].commit)
	if err != nil {
		return nil, err
	}
	raw, err := git(ctx, checkout, "diff", "--full-index", "--no-renames", "--no-ext-diff", "--no-textconv", sides[0].commit, sides[1].commit)
	if err != nil {
		return nil, err
	}
	rows, err := buildRows(files, pairs, nil, markers, filter, scope.tokenizer())
	if err != nil {
		return nil, err
	}
	return selectPatch(raw, files, rows)
}
