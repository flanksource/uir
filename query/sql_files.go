package query

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/flanksource/uir/internal/buildconstraint"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
)

var sqlFileFields = map[string]string{
	"path": "string", "module": "string", "package": "string", "snapshot": "string",
	"file.lines": "integer", "file.definitions": "integer",
	"file.build_constraint": "string",
}

var sqlFileWildcardFields = []string{"path", "module", "package", "snapshot", "file.lines", "file.definitions"}

type sqlFileOptions struct {
	scopes          []moduleScope
	lines           bool
	includeExcluded bool
	buildConstraint bool
	buildTags       []string
}

func (pipeline *Pipeline) sqlFileRecords(ctx context.Context, options sqlFileOptions) ([]sqlRecord, error) {
	var records []sqlRecord
	builds := fileBuildConstraints{headers: map[string]buildconstraint.Header{}, computed: map[string]map[string]bool{}, tags: options.buildTags}
	for _, scope := range options.scopes {
		start := len(records)
		facts, err := pipeline.cache.snapshot(ctx, pipeline.database, scope.snapshot.ID)
		if err != nil {
			return nil, err
		}
		active := make(map[string]storage.ActiveDocument, len(facts.documents))
		for _, document := range facts.documents {
			if options.includeExcluded || document.Document.Coverage != storage.CoverageExcluded {
				active[document.Source.PathKey] = document
			}
		}
		lines := map[uuid.UUID]int{}
		if options.lines {
			lines, err = pipeline.sqlSourceLines(ctx, active)
			if err != nil {
				return nil, fmt.Errorf("source metrics of snapshot %s: %w", scope.snapshot.ID, err)
			}
		}
		for _, path := range slices.Sorted(maps.Keys(active)) {
			document := active[path]
			match := ModuleMatch{Kind: "file", RootKey: scope.root.RootKey, Location: scope.location.CanonicalPath,
				SnapshotID: scope.snapshot.ID.String(), Path: path, PackagePath: document.Source.PackagePath,
				SourceID: document.Source.ID.String(), Coverage: string(document.Document.Coverage)}
			values := map[string]any{"path": path, "module": match.RootKey, "package": match.PackagePath,
				"snapshot": match.SnapshotID, "file.definitions": document.Document.SymbolCount}
			if options.lines {
				count, found := lines[document.Source.ID]
				if !found {
					return nil, fmt.Errorf("snapshot %s source %q has no recorded file.lines; reindex or republish this revision and query its new snapshot", scope.snapshot.ID, path)
				}
				values["file.lines"] = count
			}
			records = append(records, sqlRecord{match: match, values: values})
		}
		if options.buildConstraint {
			if err := pipeline.sqlFileBuildConstraints(ctx, scope, records[start:], active, &builds); err != nil {
				return nil, err
			}
		}
	}
	return records, nil
}

func (pipeline *Pipeline) sqlSourceLines(ctx context.Context, documents map[string]storage.ActiveDocument) (map[uuid.UUID]int, error) {
	if !pipeline.database.WithContext(ctx).Migrator().HasTable(&storage.SourceMetrics{}) {
		return nil, fmt.Errorf("source metric schema is unavailable; open the index read-write to upgrade it, then reindex or republish")
	}
	ids := make([]uuid.UUID, 0, len(documents))
	for _, document := range documents {
		ids = append(ids, document.Source.ID)
	}
	lines := make(map[uuid.UUID]int, len(ids))
	for start := 0; start < len(ids); start += lookupBatch {
		var rows []storage.SourceMetrics
		if err := pipeline.database.WithContext(ctx).Where("source_revision_id IN ? AND version = ?", ids[start:min(start+lookupBatch, len(ids))], storage.MetricsVersion).Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load stored source metrics: %w", err)
		}
		for _, row := range rows {
			lines[row.SourceRevisionID] = row.Lines
		}
	}
	return lines, nil
}
