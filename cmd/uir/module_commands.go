package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/entity"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

// moduleRootRow is a registered root with its primary location's head; SnapshotID is empty and
// HeadVersion 0 while the root is registered but not indexed.
type moduleRootRow struct {
	RootKey     string `json:"root_key"`
	Name        string `json:"name"`
	Location    string `json:"location"`
	SnapshotID  string `json:"snapshot_id"`
	HeadVersion int64  `json:"head_version"`
}

func (moduleRootRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("root_key").Label("Root").Build(),
		api.Column("name").Label("Name").Build(),
		api.Column("location").Label("Primary location").Build(),
		api.Column("snapshot_id").Label("Snapshot").Build(),
		api.Column("head_version").Label("Version").Type("int").Build(),
	}
}

func (row moduleRootRow) Row() map[string]any {
	return map[string]any{
		"root_key": row.RootKey, "name": row.Name, "location": row.Location,
		"snapshot_id": row.SnapshotID, "head_version": row.HeadVersion,
	}
}

// moduleQueryRow is one query match or declaration. Source is the display form of Path, Line, and
// Column; the remaining fields are set by the index-backed operations.
type moduleQueryRow struct {
	Kind           string `json:"kind"`
	NodeKind       string `json:"node_kind,omitempty"`
	Root           string `json:"root"`
	Symbol         string `json:"symbol"`
	Location       string `json:"location"`
	Source         string `json:"source"`
	SnapshotID     string `json:"snapshot_id"`
	Path           string `json:"path,omitempty"`
	PackagePath    string `json:"package_path,omitempty"`
	Line           *int   `json:"line,omitempty"`
	Column         *int   `json:"column,omitempty"`
	EndLine        *int   `json:"end_line,omitempty"`
	EndColumn      *int   `json:"end_column,omitempty"`
	Role           string `json:"role,omitempty"`
	Relation       string `json:"relation,omitempty"`
	SourceID       string `json:"source_id,omitempty"`
	SourceName     string `json:"source_name,omitempty"`
	SymbolID       string `json:"symbol_id,omitempty"`
	EnclosingID    string `json:"enclosing_id,omitempty"`
	EnclosingKey   string `json:"enclosing_key,omitempty"`
	Coverage       string `json:"coverage,omitempty"`
	Dispatch       bool   `json:"dispatch,omitempty"`
	Depth          int    `json:"depth,omitempty"`
	identifier     uir.Identifier
	declaration    string
	definitionLine *int
	usage          string
}

type moduleCallPath struct {
	Symbols []query.ModuleSymbol `json:"symbols"`
	Calls   []moduleQueryRow     `json:"calls"`
}

// moduleQueryResult is the query envelope. JSON and YAML retain every field while human-readable
// formats render the symbol tree.
type moduleQueryResult struct {
	Operation    query.Operation            `json:"operation"`
	Total        int                        `json:"total"`
	Matches      []moduleQueryRow           `json:"matches"`
	Declarations []moduleQueryRow           `json:"declarations"`
	Symbols      []query.ModuleSymbol       `json:"symbols"`
	Coverage     []query.ModuleCoverage     `json:"coverage"`
	Warnings     []query.MissingHeadWarning `json:"warnings"`
	Stages       []query.ResolutionStage    `json:"stages"`
	Path         *moduleCallPath            `json:"path,omitempty"`
	limit        int
	groupBy      []string
}

func (result moduleQueryResult) PageMetadata() clicky.PageInfo {
	return clicky.PageInfo{Limit: result.limit, Total: int64(result.Total)}
}

func (result moduleQueryResult) PageRows() any {
	if result.Path == nil {
		return moduleQueryDisplay{result: result, rows: result.Matches}
	}
	names := make([]string, 0, len(result.Path.Symbols))
	for _, symbol := range result.Path.Symbols {
		names = append(names, symbol.QueryName)
	}
	return moduleQueryDisplay{result: result, rows: []map[string]any{{"path": strings.Join(names, " -> "), "hops": len(result.Path.Calls)}}}
}

type moduleGetOptions struct {
	Root string `args:"true" required:"true"`
}

type moduleLocationOptions struct {
	Root string `flag:"root" help:"Module path" required:"true"`
}

type moduleSnapshotOptions struct {
	Root     string `flag:"root" help:"Module path" required:"true"`
	Location string `flag:"location" help:"Canonical checkout path" required:"true"`
	Limit    int    `flag:"limit" help:"Maximum rows from 1 through 1000" default:"100"`
	Offset   int    `flag:"offset" help:"Rows to skip" default:"0"`
}

type moduleQueryOptions struct {
	Expression string   `args:"true"`
	Methods    bool     `flag:"methods" help:"Select methods"`
	Vars       bool     `flag:"vars" help:"Select variables"`
	Types      bool     `flag:"types" help:"Select types"`
	Modules    bool     `flag:"modules" help:"Select module nodes"`
	Packages   bool     `flag:"packages" help:"Select package nodes"`
	Callers    bool     `flag:"callers" help:"Find incoming calls"`
	Calls      bool     `flag:"calls" help:"Find outgoing calls"`
	Implements bool     `flag:"implements" help:"Find implementer types"`
	Inherits   bool     `flag:"inherits" help:"Find direct embedders"`
	Include    []string `flag:"include" help:"Include module, package, or symbol paths matching a glob"`
	Exclude    []string `flag:"exclude" help:"Exclude module, package, or symbol paths matching a glob"`
	GroupBy    string   `flag:"group-by" help:"Tree levels in order: module,package,file" default:"module,package,file"`
	RootKey    string   `flag:"root" help:"Module path to query"`
	Location   string   `flag:"location" help:"Registered checkout path"`
	SnapshotID string   `flag:"snapshot" help:"Explicit immutable snapshot UUID"`
	Limit      int      `flag:"limit" help:"Maximum rows from 1 through 1000" default:"100"`
}

type moduleSuggestOptions struct {
	Prefix     string `flag:"prefix" help:"Partial Go symbol spelling" required:"true"`
	RootKey    string `flag:"root" help:"Module path to query"`
	Location   string `flag:"location" help:"Registered checkout path"`
	SnapshotID string `flag:"snapshot" help:"Explicit immutable snapshot UUID"`
	Limit      int    `flag:"limit" help:"Maximum suggestions from 1 through 100" default:"20"`
}

func (moduleQueryRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("kind").Label("Kind").Build(),
		api.Column("root").Label("Root").Build(),
		api.Column("symbol").Label("Symbol").Build(),
		api.Column("location").Label("Checkout").Build(),
		api.Column("source").Label("Source").Build(),
		api.Column("snapshot_id").Label("Snapshot").Build(),
	}
}

func (row moduleQueryRow) Row() map[string]any {
	return map[string]any{
		"kind": row.Kind, "root": row.Root, "symbol": row.Symbol,
		"location": row.Location, "source": row.Source, "snapshot_id": row.SnapshotID,
	}
}

func registerModuleCommands(root *cobra.Command) {
	registerModuleBrowseCommands(root)
	registerModuleGraphCommand(root)
	registerModuleIndexCommands(root)

	list := clicky.AddNamedCommandWithContext("list", root, struct{}{}, func(ctx context.Context, _ struct{}) ([]moduleRootRow, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return nil, err
		}
		return listModuleRoots(ctx, database)
	})
	list.Short = "List indexed module roots"
	setModuleRoute(list, "modules")

	get := clicky.AddNamedCommandWithContext("get", root, moduleGetOptions{}, func(ctx context.Context, options moduleGetOptions) (moduleRootRow, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return moduleRootRow{}, err
		}
		return getModuleRoot(ctx, database, options.Root)
	})
	get.Use = "get <root>"
	get.Short = "Show one indexed module root"
	get.Args = cobra.ExactArgs(1)
	setModuleRoute(get, "modules/by-key/{root}")

	queryCommand := clicky.AddNamedCommandWithContext("query", root, moduleQueryOptions{}, func(ctx context.Context, options moduleQueryOptions) (moduleQueryResult, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return moduleQueryResult{}, err
		}
		return queryModules(ctx, database, options)
	})
	queryCommand.Use = "query [expression]"
	queryCommand.Short = "Run a compact symbol query against indexed modules"
	queryCommand.Args = cobra.MaximumNArgs(1)
	setModuleRoute(queryCommand, "modules/query")
	suggest := clicky.AddNamedCommandWithContext("suggest", root, moduleSuggestOptions{}, func(ctx context.Context, options moduleSuggestOptions) (query.ItemsWithWarnings[query.ModuleSymbol], error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return query.ItemsWithWarnings[query.ModuleSymbol]{}, err
		}
		pipeline, err := query.NewPipeline(database)
		if err != nil {
			return query.ItemsWithWarnings[query.ModuleSymbol]{}, err
		}
		result, err := pipeline.SuggestSymbols(ctx, options.Prefix, query.ModuleScopeOptions{
			RootKey: options.RootKey, Location: options.Location, SnapshotID: options.SnapshotID, Limit: options.Limit,
		})
		warnMissingHeads(ctx, result.Warnings)
		return result, err
	})
	suggest.Short = "Complete a Go symbol name from indexed snapshots"
	setModuleRoute(suggest, "modules/suggest")
	suggest.Annotations["clicky/operation-method"] = http.MethodGet
	selectors := clicky.AddNamedCommandWithContext("suggest-selectors", root, moduleSuggestOptions{}, func(ctx context.Context, options moduleSuggestOptions) (query.ItemsWithWarnings[string], error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return query.ItemsWithWarnings[string]{}, err
		}
		pipeline, err := query.NewPipeline(database)
		if err != nil {
			return query.ItemsWithWarnings[string]{}, err
		}
		result, err := pipeline.SuggestSelectors(ctx, options.Prefix, query.ModuleScopeOptions{
			RootKey: options.RootKey, Location: options.Location, SnapshotID: options.SnapshotID, Limit: options.Limit,
		})
		warnMissingHeads(ctx, result.Warnings)
		return result, err
	})
	selectors.Short = "Complete typed selectors from indexed snapshots"
	setModuleRoute(selectors, "modules/suggest-selectors")
	selectors.Annotations["clicky/operation-method"] = http.MethodGet
	locations := clicky.AddNamedCommandWithContext("locations", root, moduleLocationOptions{}, func(ctx context.Context, options moduleLocationOptions) ([]storage.ModuleLocationView, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return nil, err
		}
		return storage.ModuleLocations(ctx, database, options.Root)
	})
	locations.Short = "List registered checkouts for a module root"
	setModuleRoute(locations, "modules/locations")
	locations.Annotations["clicky/operation-method"] = http.MethodGet
	snapshots := clicky.AddNamedCommandWithContext("snapshots", root, moduleSnapshotOptions{}, func(ctx context.Context, options moduleSnapshotOptions) (clicky.PagedResult[storage.ModuleSnapshotView], error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return clicky.PagedResult[storage.ModuleSnapshotView]{}, err
		}
		rows, total, err := storage.ModuleSnapshots(ctx, database, storage.ModuleSnapshotListOptions{
			RootKey: options.Root, Location: options.Location, Limit: options.Limit, Offset: options.Offset,
		})
		if err != nil {
			return clicky.PagedResult[storage.ModuleSnapshotView]{}, err
		}
		return clicky.NewPagedResult(rows, options.Limit, options.Offset, total), nil
	})
	snapshots.Short = "List immutable snapshots for one registered checkout"
	setModuleRoute(snapshots, "modules/snapshots")
	snapshots.Annotations["clicky/operation-method"] = http.MethodGet
}

func queryModules(ctx context.Context, database *gorm.DB, options moduleQueryOptions) (moduleQueryResult, error) {
	groups, err := parseQueryGroupBy(options.GroupBy)
	if err != nil {
		return moduleQueryResult{}, err
	}
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return moduleQueryResult{}, err
	}
	structured := query.StructuredOptions{Include: options.Include, Exclude: options.Exclude}
	for _, flag := range []struct {
		enabled bool
		name    string
	}{{options.Methods, "method"}, {options.Vars, "var"}, {options.Types, "type"}, {options.Modules, "module"}, {options.Packages, "package"}} {
		if flag.enabled {
			structured.Kinds = append(structured.Kinds, flag.name)
		}
	}
	for _, flag := range []struct {
		enabled bool
		name    string
	}{{options.Callers, "callers"}, {options.Calls, "calls"}, {options.Implements, "implements"}, {options.Inherits, "inherits"}} {
		if flag.enabled {
			structured.Relations = append(structured.Relations, flag.name)
		}
	}
	expression, err := query.ParseStructured(options.Expression, structured)
	var result query.ModuleQueryResult
	if err == nil {
		result, err = pipeline.RunExpr(ctx, expression, query.ModuleScopeOptions{
			RootKey: options.RootKey, Location: options.Location, SnapshotID: options.SnapshotID, Limit: options.Limit,
		})
	}
	if err != nil {
		return moduleQueryResult{}, queryStatusError(err)
	}
	warnMissingHeads(ctx, result.Warnings)
	symbols := result.Symbols
	if symbols == nil {
		symbols = []query.ModuleSymbol{}
	}
	var path *moduleCallPath
	if result.Path != nil {
		path = &moduleCallPath{Symbols: result.Path.Symbols, Calls: moduleQueryRows(result.Path.Calls)}
	}
	matches := moduleQueryRows(result.Matches)
	if err := populateQueryUsage(ctx, pipeline, matches); err != nil {
		return moduleQueryResult{}, err
	}
	return moduleQueryResult{
		Operation: result.Operation, Total: result.Total, Matches: matches,
		Declarations: moduleQueryRows(result.Declarations), Symbols: symbols, Coverage: result.Coverage,
		Warnings: result.Warnings, Stages: result.Stages, Path: path, limit: options.Limit, groupBy: groups,
	}, nil
}

func populateQueryUsage(ctx context.Context, pipeline *query.Pipeline, rows []moduleQueryRow) error {
	linesByFile := map[string][]string{}
	for index := range rows {
		row := &rows[index]
		if row.Line == nil {
			if row.Kind == "module" || row.Kind == "package" {
				continue
			}
			return fmt.Errorf("query match %s in %s has no source line", row.Symbol, row.Path)
		}
		key := row.SnapshotID + "\x00" + row.Path
		lines, found := linesByFile[key]
		if !found {
			source, err := pipeline.ReadModuleSource(ctx, row.SnapshotID, row.Path)
			if err != nil {
				return fmt.Errorf("read usage of %s at %s: %w", row.Symbol, row.Source, err)
			}
			lines = strings.Split(source.Content, "\n")
			linesByFile[key] = lines
		}
		if *row.Line < 1 || *row.Line > len(lines) {
			return fmt.Errorf("query match %s at %s has line %d outside source with %d lines", row.Symbol, row.Source, *row.Line, len(lines))
		}
		row.usage = strings.TrimSpace(lines[*row.Line-1])
		if row.usage == "" {
			return fmt.Errorf("query match %s at %s has an empty source line", row.Symbol, row.Source)
		}
	}
	return nil
}

func warnMissingHeads(ctx context.Context, warnings []query.MissingHeadWarning) {
	if entity.OperationSurfaceFromContext(ctx) != "cli" {
		return
	}
	for _, warning := range warnings {
		logger.Warnf("root %q at %q: %s", warning.RootKey, warning.Location, warning.Message)
	}
}

func moduleQueryRows(matches []query.ModuleMatch) []moduleQueryRow {
	rows := make([]moduleQueryRow, 0, len(matches))
	for _, match := range matches {
		source := match.Path
		if match.Line != nil {
			source += fmt.Sprintf(":%d", *match.Line)
			if match.Column != nil {
				source += fmt.Sprintf(":%d", *match.Column)
			}
		}
		rows = append(rows, moduleQueryRow{
			Kind: match.Kind, NodeKind: match.NodeKind, Root: match.RootKey, Symbol: match.Identifier.SymbolKey(),
			Location: match.Location, Source: source, SnapshotID: match.SnapshotID,
			Path: match.Path, Line: match.Line, Column: match.Column, EndLine: match.EndLine, EndColumn: match.EndColumn,
			PackagePath: match.PackagePath, Depth: match.Depth,
			Role: match.Role, Relation: match.Relation, SourceID: match.SourceID, SourceName: match.SourceName, SymbolID: match.SymbolID, EnclosingID: match.EnclosingID, EnclosingKey: match.EnclosingKey,
			Coverage: match.Coverage, Dispatch: match.Dispatch,
			identifier:  match.Identifier,
			declaration: match.Declaration, definitionLine: match.DefinitionLine,
		})
	}
	return rows
}

func setModuleRoute(command *cobra.Command, path string) {
	if command.Annotations == nil {
		command.Annotations = make(map[string]string)
	}
	command.Annotations["clicky/operation-path"] = path
}

func listModuleRoots(ctx context.Context, database *gorm.DB) ([]moduleRootRow, error) {
	var roots []storage.ModuleRoot
	if err := database.WithContext(ctx).Order("root_key").Find(&roots).Error; err != nil {
		return nil, fmt.Errorf("list module roots: %w", err)
	}
	rows := make([]moduleRootRow, 0, len(roots))
	for _, root := range roots {
		row, err := moduleRootDetails(ctx, database, root)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func getModuleRoot(ctx context.Context, database *gorm.DB, rootKey string) (moduleRootRow, error) {
	var root storage.ModuleRoot
	if err := database.WithContext(ctx).Where("root_key = ?", rootKey).First(&root).Error; err != nil {
		return moduleRootRow{}, fmt.Errorf("load module root %q: %w", rootKey, err)
	}
	return moduleRootDetails(ctx, database, root)
}

func moduleRootDetails(ctx context.Context, database *gorm.DB, root storage.ModuleRoot) (moduleRootRow, error) {
	var primary storage.ModulePrimary
	if err := database.WithContext(ctx).Where("root_id = ?", root.ID).First(&primary).Error; err != nil {
		return moduleRootRow{}, fmt.Errorf("load primary location for root %q: %w", root.RootKey, err)
	}
	var location storage.ModuleLocation
	if err := database.WithContext(ctx).Where("id = ?", primary.LocationID).First(&location).Error; err != nil {
		return moduleRootRow{}, fmt.Errorf("load primary location %s: %w", primary.LocationID, err)
	}
	row := moduleRootRow{RootKey: root.RootKey, Name: root.Name, Location: location.CanonicalPath}
	var head storage.ModuleLocationHead
	err := database.WithContext(ctx).Where("location_id = ?", location.ID).First(&head).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// A registration the pre-handle cutover kept has no head until `uir reindex`: it has no snapshot.
		return row, nil
	}
	if err != nil {
		return moduleRootRow{}, fmt.Errorf("load primary head for root %q: %w", root.RootKey, err)
	}
	row.SnapshotID, row.HeadVersion = head.SnapshotID.String(), head.Version
	return row, nil
}
