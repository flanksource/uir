package query

import (
	"context"
	"fmt"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

// accessRelationships are the edge types the data access occurrence roles draw: a reference reads
// the symbol it names and a write writes it.
var accessRelationships = map[string]uir.RelationshipType{"reference": uir.RelationshipTypeRead, "write": uir.RelationshipTypeWrite}

// accessRoles are the occurrence roles of the data access edges the source's access follows.
func (source *indexGraphSource) accessRoles() map[string]bool {
	roles := map[string]bool{}
	for role, relationship := range accessRelationships {
		if graph.Follows(source.access, relationship) {
			roles[role] = true
		}
	}
	return roles
}

// accesses are the reads and writes the symbol's declarations make of other symbols, a read or write
// step to each. A use of a container, such as a Go package qualifying a name, is no data access.
func (source *indexGraphSource) accesses(ctx context.Context, symbol ModuleSymbol, steps []graph.Step) ([]graph.Step, error) {
	roles := source.accessRoles()
	if len(roles) == 0 {
		return steps, nil
	}
	uses, err := source.index.enclosedUses(ctx, symbol, roles)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, use := range uses {
		ids[use.SymbolID] = true
	}
	targets, err := source.index.symbolsByID(ctx, sortedKeys(ids))
	if err != nil {
		return nil, err
	}
	if err := source.remember(ctx, targets); err != nil {
		return nil, err
	}
	for _, use := range uses {
		if source.index.category(source.symbols[use.SymbolID].Kind) == symbolhandle.CategoryContainer {
			continue
		}
		if steps, err = source.accessStep(ctx, steps, source.nodes[use.SymbolID], use); err != nil {
			return nil, err
		}
	}
	return steps, nil
}

// dataUsers are the steps into a data symbol: its reads and writes (users) as the access follows them,
// and for a type, when the access follows calls, a call step from each declaration that calls it, such
// as a producer's rule including a screen.
func (source *indexGraphSource) dataUsers(ctx context.Context, symbol ModuleSymbol) ([]graph.Step, error) {
	steps, err := source.users(ctx, symbol)
	if err != nil || source.index.category(symbol.Kind) != symbolhandle.CategoryType || !graph.Follows(source.access, uir.RelationshipTypeCall) {
		return steps, err
	}
	rows, err := source.index.callers(ctx, symbol, false, nil, &ModuleQueryResult{})
	if err != nil {
		return nil, err
	}
	includes, err := source.enclosingSteps(ctx, rows, source.step)
	if err != nil {
		return nil, err
	}
	return append(steps, includes...), nil
}

// users are the reads and writes of a data symbol, a read or write step from each declaration
// enclosing one. A type's are those of itself and of its members, such as a table's columns or a
// record's fields.
func (source *indexGraphSource) users(ctx context.Context, symbol ModuleSymbol) ([]graph.Step, error) {
	roles := source.accessRoles()
	if len(roles) == 0 {
		return nil, nil
	}
	targets := map[string]string{symbol.ID: symbol.ID}
	if source.index.category(symbol.Kind) == symbolhandle.CategoryType {
		members, err := source.index.members(ctx, symbol)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			targets[member] = member
		}
	}
	rows, err := source.index.occurrences(ctx, occurrenceQuery{targets: targets, kind: "reference", keep: func(occurrence storage.DocumentOccurrence) bool {
		return roles[occurrence.Role]
	}})
	if err != nil {
		return nil, err
	}
	return source.enclosingSteps(ctx, rows, source.accessStep)
}

// accessStep appends the read or write step to a neighbour over one data access occurrence row. The
// site's text is the name as written in a Go source, or the text the producer recorded.
func (source *indexGraphSource) accessStep(ctx context.Context, steps []graph.Step, neighbour graph.Node, use ModuleMatch) ([]graph.Step, error) {
	relationship, known := accessRelationships[use.Role]
	if !known {
		return nil, fmt.Errorf("occurrence of %s at %s is a %s, not a read or write", use.SymbolID, use.Path, use.Role)
	}
	read, readable, err := source.guards.access(ctx, use)
	if err != nil {
		return nil, fmt.Errorf("%s site of %s at %s: %w", relationship, use.SymbolID, use.Path, err)
	}
	return appendStep(steps, neighbour, use, relationship, read, readable), nil
}

// appendStep appends the step to a neighbour over one occurrence row, its site read from source when
// readable.
func appendStep(steps []graph.Step, neighbour graph.Node, row ModuleMatch, kind uir.RelationshipType, read siteContext, readable bool) []graph.Step {
	site := graph.Site{Path: row.Path, Text: row.text}
	if row.Line != nil && row.Column != nil {
		site.Line, site.Column = *row.Line, *row.Column
	}
	if readable {
		site.Text, site.Guards = read.Text, guardTexts(read.Guards)
	}
	return append(steps, graph.Step{Node: neighbour, Edge: graph.Edge{Type: kind, Sites: []graph.Site{site}}})
}

// enclosedUses lists the occurrences of a role in roles whose enclosing declaration is the target and
// which name a symbol.
func (index *indexContext) enclosedUses(ctx context.Context, target ModuleSymbol, roles map[string]bool) ([]ModuleMatch, error) {
	postings, err := index.postings(ctx, []string{target.ID}, "definition")
	if err != nil {
		return nil, err
	}
	if err := index.prefetch(ctx, postings); err != nil {
		return nil, err
	}
	matches := []ModuleMatch{}
	for _, posting := range postings {
		document, err := index.document(ctx, posting)
		if err != nil {
			return nil, err
		}
		for _, occurrence := range document.content.Occurrences {
			if !roles[occurrence.Role] || occurrence.Symbol == nil || occurrence.Enclosing == nil || *occurrence.Enclosing != target.ID {
				continue
			}
			matches = append(matches, index.occurrenceMatch(posting, document, "access", occurrence))
		}
	}
	return matches, nil
}

// members are the ids of the member symbols the owner owns, such as a table's columns or a record's
// fields.
func (index *indexContext) members(ctx context.Context, owner ModuleSymbol) ([]string, error) {
	var rows []storage.Symbol
	if err := index.database.WithContext(ctx).Where("owner_id = ?", owner.ID).Order("id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load members of %s: %w", owner.QueryName, err)
	}
	if err := index.remember(rows); err != nil {
		return nil, err
	}
	var members []string
	for _, row := range rows {
		if err := index.register(ctx, row.Kind); err != nil {
			return nil, fmt.Errorf("member %s of %s: %w", row.ID, owner.QueryName, err)
		}
		if index.category(row.Kind) == symbolhandle.CategoryMember {
			members = append(members, row.ID)
		}
	}
	return members, nil
}
