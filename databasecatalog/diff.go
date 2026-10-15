package databasecatalog

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/commons-db/models"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/diff"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Schemas lists the schema names captured in a database snapshot, including
// schemas whose last table has been removed.
func Schemas(ctx context.Context, database *gorm.DB, snapshotID string) ([]string, error) {
	if _, _, err := snapshotModule(ctx, database, snapshotID); err != nil {
		return nil, err
	}
	id, _ := uuid.Parse(snapshotID)
	active, err := storage.ActiveDocuments(ctx, database, id, storage.ActiveDocumentOptions{})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, document := range active {
		seen[document.Document.PackagePath] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

// Diff compares two captured catalogues. A same-module comparison includes all
// schemas; cross-module comparisons select one schema on each side and align
// objects by their exact names after removing the schema qualifier.
func Diff(ctx context.Context, database *gorm.DB, beforeID, afterID, beforeSchema, afterSchema string) (diff.DiffResult, error) {
	if (beforeSchema == "") != (afterSchema == "") {
		return diff.DiffResult{}, fmt.Errorf("select both schemas or neither")
	}
	beforeModule, beforeType, err := snapshotModule(ctx, database, beforeID)
	if err != nil {
		return diff.DiffResult{}, err
	}
	afterModule, afterType, err := snapshotModule(ctx, database, afterID)
	if err != nil {
		return diff.DiffResult{}, err
	}
	if beforeModule.ID != afterModule.ID && beforeSchema == "" {
		return diff.DiffResult{}, fmt.Errorf("cross-module comparison requires one schema on each side")
	}
	before, err := readTree(ctx, database, beforeID, beforeSchema)
	if err != nil {
		return diff.DiffResult{}, err
	}
	after, err := readTree(ctx, database, afterID, afterSchema)
	if err != nil {
		return diff.DiffResult{}, err
	}
	if beforeSchema != "" {
		alignSchema(before)
		alignSchema(after)
	}
	if limitedMetadata(beforeType) || limitedMetadata(afterType) {
		limitMetadata(before)
		limitMetadata(after)
	} else if beforeType == models.ConnectionTypeSQLite || afterType == models.ConnectionTypeSQLite {
		before.Functions, after.Functions = nil, nil
		for i := range before.Tables {
			for j := range before.Tables[i].Columns {
				before.Tables[i].Columns[j].AutoIncrement = false
			}
		}
		for i := range after.Tables {
			for j := range after.Tables[i].Columns {
				after.Tables[i].Columns[j].AutoIncrement = false
			}
		}
	}
	return diff.DiffTree(before, after, diff.TreeDiffOptions{NodeDiffOptions: diff.NodeDiffOptions{Statements: true}}), nil
}

func snapshotModule(ctx context.Context, database *gorm.DB, snapshotID string) (storage.DatabaseModule, string, error) {
	id, err := uuid.Parse(snapshotID)
	if err != nil {
		return storage.DatabaseModule{}, "", fmt.Errorf("invalid snapshot ID: %w", err)
	}
	var module storage.DatabaseModule
	err = database.WithContext(ctx).Model(&module).
		Joins("JOIN modules ON modules.root_key = database_modules.root_key").
		Joins("JOIN snapshots ON snapshots.root_id = modules.id").
		Where("snapshots.id = ?", id).First(&module).Error
	if err != nil {
		return storage.DatabaseModule{}, "", fmt.Errorf("database snapshot %s: %w", id, err)
	}
	connection, err := storage.GetConnection(ctx, database, module.ConnectionID)
	if err != nil {
		return storage.DatabaseModule{}, "", err
	}
	return module, connection.Type, nil
}

func readTree(ctx context.Context, database *gorm.DB, snapshotID, schema string) (*uir.UIR, error) {
	id, err := uuid.Parse(snapshotID)
	if err != nil {
		return nil, fmt.Errorf("invalid snapshot ID: %w", err)
	}
	active, err := storage.ActiveDocuments(ctx, database, id, storage.ActiveDocumentOptions{Content: true})
	if err != nil {
		return nil, err
	}
	tree := &uir.UIR{}
	matched := false
	for _, document := range active {
		if schema != "" && document.Document.PackagePath != schema {
			continue
		}
		matched = true
		content, err := storage.DecodeDocument(document.Document, document.Source)
		if err != nil {
			return nil, err
		}
		for _, symbol := range content.Symbols {
			if symbol.ParentKey != "" {
				continue
			}
			node, err := uir.UnmarshalNode(symbol.Payload)
			if err != nil {
				return nil, fmt.Errorf("decode %s: %w", symbol.Key, err)
			}
			tree.Add(node)
		}
	}
	if schema != "" && !matched {
		return nil, fmt.Errorf("snapshot %s has no schema %q", snapshotID, schema)
	}
	if len(tree.Warnings) > 0 {
		return nil, fmt.Errorf("snapshot %s contains unsupported UIR nodes: %s", snapshotID, tree.Warnings[0].Message)
	}
	return tree, nil
}

func alignSchema(tree *uir.UIR) {
	for i := range tree.Tables {
		table := &tree.Tables[i]
		table.Package = ""
		for j := range table.Columns {
			table.Columns[j].Package = ""
		}
		for j := range table.Indexes {
			table.Indexes[j].Package = ""
		}
		for j := range table.ForeignKeys {
			table.ForeignKeys[j].Package = ""
		}
		for j := range table.Triggers {
			table.Triggers[j].Package = ""
		}
	}
	for i := range tree.Functions {
		tree.Functions[i].Package = ""
	}
}

func limitedMetadata(connectionType string) bool {
	return connectionType == models.ConnectionTypeMySQL
}

// The basic MySQL inspector currently supplies names and SQL types but not
// the optional constraint and routine metadata. Compare only fields both
// sides captured, so a rich source does not report unsupported facts as drops.
func limitMetadata(tree *uir.UIR) {
	tree.Functions = nil
	for i := range tree.Tables {
		table := &tree.Tables[i]
		table.Indexes, table.ForeignKeys, table.Triggers, table.Definition, table.PrimaryKey = nil, nil, nil, nil, nil
		for j := range table.Columns {
			column := &table.Columns[j]
			column.Nullable, column.PrimaryKey, column.AutoIncrement, column.Unique = false, false, false, false
			column.MaxLength, column.Precision, column.Scale, column.DefaultValue, column.Enum = nil, nil, nil, nil, nil
		}
	}
}
