package lint

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/flanksource/gavel/models"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

func snapshots(ctx context.Context, database *gorm.DB, config *Config, options Options, mode storage.AnalysisMode) (query.ModuleScopeOptions, error) {
	if options.Snapshot != "" {
		return query.ModuleScopeOptions{SnapshotID: options.Snapshot}, nil
	}
	directory := options.Directory
	if directory == "" {
		directory = config.Root
	}
	locations := []storage.ModuleLocation{{CanonicalPath: directory}}
	if options.All {
		locations = nil
		if err := database.WithContext(ctx).Table("locations AS location").Select("location.*").Joins("JOIN primary_locations AS primary_location ON primary_location.location_id = location.id").Order("location.canonical_path").Find(&locations).Error; err != nil {
			return query.ModuleScopeOptions{}, err
		}
		if len(locations) == 0 {
			return query.ModuleScopeOptions{}, errors.New("--all requires registered modules; run uir add first")
		}
	}
	engine, err := indexer.New(database)
	if err != nil {
		return query.ModuleScopeOptions{}, err
	}
	scope := query.ModuleScopeOptions{}
	for _, location := range locations {
		if location.Kind == storage.LocationExternal {
			var head storage.ModuleLocationHead
			if err := database.WithContext(ctx).Where("location_id = ?", location.ID).Take(&head).Error; err != nil {
				return scope, err
			}
			scope.SnapshotIDs = append(scope.SnapshotIDs, head.SnapshotID.String())
			continue
		}
		result, err := engine.Analyze(ctx, indexer.AnalysisOptions{Directory: location.CanonicalPath, Mode: mode, Tags: options.Tags, IncludeTests: config.Lint.IncludeTests, NoIndex: options.NoIndex, Source: options.Source})
		if err != nil {
			return scope, fmt.Errorf("analyze %s: %w", location.CanonicalPath, err)
		}
		scope.SnapshotIDs = append(scope.SnapshotIDs, result.SnapshotID)
	}
	return scope, nil
}

func (result *Result) orderRules(rules []Rule) {
	order := map[string]int{}
	for i, rule := range rules {
		order[rule.ID] = i
	}
	order[indexer.RuleIndex], order[indexer.RuleCoverage] = len(rules), len(rules)
	slices.SortStableFunc(result.Violations, func(a, b models.Violation) int { return order[a.Rule.Method] - order[b.Rule.Method] })
}

func analysisSourceSets(ctx context.Context, database *gorm.DB, scope query.ModuleScopeOptions) (map[string]string, error) {
	ids := scope.SnapshotIDs
	if scope.SnapshotID != "" {
		ids = []string{scope.SnapshotID}
	}
	var snapshots []storage.ModuleSnapshot
	if err := database.WithContext(ctx).Where("id IN ?", ids).Find(&snapshots).Error; err != nil {
		return nil, err
	}
	if len(snapshots) != len(ids) {
		return nil, errors.New("analysis scope contains missing snapshots")
	}
	sets := map[string]string{}
	for _, snapshot := range snapshots {
		sets[snapshot.LocationID.String()] = snapshot.ContentSetHash + ":" + snapshot.Revision
	}
	return sets, nil
}
