package query

import (
	"context"
	"fmt"
)

func (index *indexContext) sqlWitnessKeys(ctx context.Context, records []sqlRecord) error {
	ids := map[string]bool{}
	for _, record := range records {
		if record.witness != nil {
			ids[record.witness.ID] = true
		}
	}
	postings, err := index.postings(ctx, sortedKeys(ids), "definition")
	if err != nil {
		return err
	}
	if err := index.prefetch(ctx, postings); err != nil {
		return err
	}
	keys := map[string]string{}
	for _, posting := range postings {
		document, err := index.document(ctx, posting)
		if err != nil {
			return err
		}
		entry, err := index.entry(posting, document, posting.symbol)
		if err != nil {
			return err
		}
		scope := index.scopes[posting.scope]
		keys[scope.snapshot.ID.String()+":"+posting.symbol] = fmt.Sprintf("%s:%s:%d:%d", scope.root.RootKey, document.path, entry.NameBytes[0], entry.NameBytes[1])
	}
	for i := range records {
		if records[i].witness != nil {
			records[i].witness.DeclarationKey = keys[records[i].match.SnapshotID+":"+records[i].witness.ID]
		}
	}
	return nil
}
