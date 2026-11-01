package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/flanksource/uir/internal/buildconstraint"
	"github.com/flanksource/uir/query/sqlsyntax"
	"github.com/flanksource/uir/storage"
)

func (rows *sqlRows) bindBuildRequirement(comparison *sqlsyntax.Comparison, field string) (sqlPredicate, error) {
	if field != "file.build_constraint" {
		return nil, sqlError(comparison.Pos, "REQUIRES supports only file.build_constraint")
	}
	tag := string(comparison.Requires.Tag)
	if err := buildconstraint.ValidateTag(tag); err != nil {
		return nil, sqlError(comparison.Pos, "%s", err)
	}
	if rows.buildTags == nil {
		rows.buildTags = map[string]bool{}
	}
	rows.buildTags[tag] = true
	return func(record sqlRecord) sqlTruth {
		if record.values[field] == nil {
			return sqlUnknown
		}
		required, found := record.buildTags[tag]
		if !found {
			panic("missing precomputed build requirement for " + tag)
		}
		value := sqlBool(required)
		if comparison.Requires.Not {
			return sqlNot(value)
		}
		return value
	}, nil
}

type fileBuildConstraints struct {
	headers  map[string]buildconstraint.Header
	computed map[string]map[string]bool
	tags     []string
}

func (pipeline *Pipeline) sqlFileBuildConstraints(ctx context.Context, scope moduleScope, records []sqlRecord, documents map[string]storage.ActiveDocument, facts *fileBuildConstraints) error {
	pending := map[string]bool{}
	var sources []verifiedSource
	for _, record := range records {
		if !strings.HasSuffix(record.match.Path, ".go") {
			continue
		}
		hash := documents[record.match.Path].Source.ContentHash
		if _, found := facts.headers[hash]; found || pending[hash] {
			continue
		}
		if header, found := pipeline.cache.buildHeaders.get(hash); found {
			facts.headers[hash] = header
		} else {
			pending[hash] = true
			sources = append(sources, verifiedSource{path: record.match.Path, hash: hash, snapshot: scope.snapshot.ID.String()})
		}
	}
	var reads []sourceRead
	if len(sources) > 0 {
		reads = readVerifiedSources(ctx, pipeline.database, scope, sources, pipeline.options.gitReader)
	}
	for index, read := range reads {
		source := sources[index]
		if read.err != nil {
			return fmt.Errorf("build constraint of source %q in snapshot %s: %w", source.path, source.snapshot, read.err)
		}
		header, err := buildconstraint.Parse(read.content)
		if err != nil {
			return fmt.Errorf("build constraint of source %q in snapshot %s: %w", source.path, source.snapshot, err)
		}
		facts.headers[source.hash] = header
		pipeline.cache.buildHeaders.put(source.hash, header)
	}
	return facts.apply(ctx, scope, records, documents)
}

func (facts *fileBuildConstraints) apply(ctx context.Context, scope moduleScope, records []sqlRecord, documents map[string]storage.ActiveDocument) error {
	for index := range records {
		record := &records[index]
		if !strings.HasSuffix(record.match.Path, ".go") {
			record.values["file.build_constraint"] = nil
			continue
		}
		hash := documents[record.match.Path].Source.ContentHash
		header, found := facts.headers[hash]
		if !found {
			return fmt.Errorf("missing build header for source %q in snapshot %s", record.match.Path, scope.snapshot.ID)
		}
		if len(facts.tags) > 0 {
			if _, found := facts.computed[hash]; !found {
				required, err := header.Requires(ctx, facts.tags)
				if err != nil {
					return fmt.Errorf("build constraint of source %q in snapshot %s: %w", record.match.Path, scope.snapshot.ID, err)
				}
				facts.computed[hash] = required
			}
			record.buildTags = facts.computed[hash]
		}
		record.values["file.build_constraint"] = header.String()
	}
	return nil
}
