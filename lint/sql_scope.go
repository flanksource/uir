package lint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/flanksource/commons-db/git"
	"github.com/flanksource/gavel/linters"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

const compatibilityRuleID = "sql-compatibility-level"

func compatibilityRule(level int) (Rule, error) {
	rules := []Rule{{
		ID:      compatibilityRuleID,
		Query:   fmt.Sprintf("SELECT symbol.name AS feature, symbol.props.min_compatibility_level AS required FROM sql.feature:* WHERE symbol.props.min_compatibility_level > %d", level),
		Message: fmt.Sprintf("{{.feature}} requires SQL Server compatibility level {{.required}} (configured %d)", level),
	}}
	err := compileRules(rules)
	return rules[0], err
}

func lintScope(ctx context.Context, database *gorm.DB, config *Config, options Options, mode storage.AnalysisMode, mapped bool, snapshot string) (query.ModuleScopeOptions, error) {
	if !mapped {
		return snapshots(ctx, database, config, options, mode)
	}
	scope := query.ModuleScopeOptions{}
	if snapshot != "" {
		scope.SnapshotIDs = append(scope.SnapshotIDs, snapshot)
	}
	if options.SQLOnly {
		return scope, nil
	}
	directory := config.Root
	if options.Directory != "" {
		directory = options.Directory
	}
	hasGo, err := hasGoModule(directory, config.Root)
	if err != nil || !hasGo && !options.All {
		return scope, err
	}
	goScope, err := snapshots(ctx, database, config, options, mode)
	for _, id := range goScope.SnapshotIDs {
		if !slices.Contains(scope.SnapshotIDs, id) {
			scope.SnapshotIDs = append(scope.SnapshotIDs, id)
		}
	}
	return scope, err
}

func hasGoModule(directory, root string) (bool, error) {
	for current := directory; ; current = filepath.Dir(current) {
		for _, name := range []string{"go.mod", "go.work"} {
			if _, err := os.Stat(filepath.Join(current, name)); !errors.Is(err, os.ErrNotExist) {
				return err == nil, err
			}
		}
		if current == root || current == filepath.Dir(current) || !insideSource(current, root) {
			return false, nil
		}
	}
}

func mappedSQLSnapshot(ctx context.Context, database *gorm.DB, config *Config, options Options, selection *Selection) (sqlPublication, string, error) {
	sources := options.sqlFiles
	if sources == nil {
		var err error
		sources, err = readMappedSources(config.Root, config.Sources)
		if err != nil {
			return sqlPublication{}, "", err
		}
	}
	var selected []mappedSource
	for _, source := range sources {
		if options.Dialect != "" {
			source.Dialect = options.Dialect
		}
		if !selection.MatchesPath(source.reportPath()) && !slices.ContainsFunc(source.Inputs, selection.MatchesPath) {
			continue
		}
		if options.Files != nil && !sourceMatches(source, options.WorkDir, options.Files) {
			continue
		}
		selected = append(selected, source)
	}
	workDir := options.WorkDir
	if workDir == "" {
		workDir = config.Root
	}
	publish := indexer.PublishOptions{Producer: "uir-migration-sql"}
	if options.Source != nil {
		info := options.Source.Info()
		publish.Historical = info.Kind != git.SourceWorktree
		publish.GitCommit = info.TargetCommit
		if info.Kind == git.SourceStaged {
			publish.GitCommit = info.TreeID
		}
	}
	return publishSQL(ctx, database, workDir, selected, publish)
}

func sourceMatches(source mappedSource, root string, scopes []string) bool {
	return slices.ContainsFunc(source.Inputs, func(input string) bool {
		return linters.PathMatchesAnyScope(filepath.Join(root, filepath.FromSlash(input)), scopes)
	})
}

func (pub sqlPublication) reportSources(root string) map[string]linters.SourceFile {
	sources := make(map[string]linters.SourceFile, len(pub.files))
	for _, source := range pub.files {
		path := filepath.Join(root, filepath.FromSlash(source.reportPath()))
		sources[path] = source.reportSource(root, pub.Options.Historical)
	}
	return sources
}
