package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/entity"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

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
	Kind         string `json:"kind"`
	Root         string `json:"root"`
	Symbol       string `json:"symbol"`
	Location     string `json:"location"`
	Source       string `json:"source"`
	SnapshotID   string `json:"snapshot_id"`
	Path         string `json:"path,omitempty"`
	Line         *int   `json:"line,omitempty"`
	Column       *int   `json:"column,omitempty"`
	EndLine      *int   `json:"end_line,omitempty"`
	EndColumn    *int   `json:"end_column,omitempty"`
	Role         string `json:"role,omitempty"`
	SymbolID     string `json:"symbol_id,omitempty"`
	EnclosingID  string `json:"enclosing_id,omitempty"`
	EnclosingKey string `json:"enclosing_key,omitempty"`
	Coverage     string `json:"coverage,omitempty"`
	Dispatch     bool   `json:"dispatch,omitempty"`
}

// moduleQueryResult is the query envelope. It is clicky.Paged, so JSON and YAML responses carry every
// field while tabular formats render Matches.
type moduleQueryResult struct {
	Operation    query.Operation         `json:"operation"`
	Total        int                     `json:"total"`
	Matches      []moduleQueryRow        `json:"matches"`
	Declarations []moduleQueryRow        `json:"declarations"`
	Symbols      []query.ModuleSymbol    `json:"symbols"`
	Coverage     []query.ModuleCoverage  `json:"coverage"`
	Stages       []query.ResolutionStage `json:"stages"`
	limit        int
}

func (result moduleQueryResult) PageMetadata() clicky.PageInfo {
	return clicky.PageInfo{Limit: result.limit, Total: int64(result.Total)}
}

func (result moduleQueryResult) PageRows() any { return result.Matches }

type moduleGetOptions struct {
	Root string `args:"true" required:"true"`
}

type moduleAddOptions struct {
	Path                 string `args:"true"`
	IncludeTests         bool   `flag:"include-tests" help:"Index Go test files"`
	Force                bool   `flag:"force" help:"Reparse all sources and publish a new snapshot"`
	IncludeWorkspaceUses bool   `flag:"include-workspace-uses" help:"Also add go.work use paths outside the requested directory"`
	NoWorkspaceUses      bool   `flag:"no-workspace-uses" help:"Do not add go.work use paths outside the requested directory"`
}

type moduleReindexOptions struct {
	Path         string `args:"true"`
	IncludeTests bool   `flag:"include-tests" help:"Index Go test files"`
	Force        bool   `flag:"force" help:"Reparse all sources and publish a new snapshot"`
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
	Expression string `args:"true" required:"true"`
	RootKey    string `flag:"root" help:"Module path to query"`
	Location   string `flag:"location" help:"Registered checkout path"`
	SnapshotID string `flag:"snapshot" help:"Explicit immutable snapshot UUID"`
	Limit      int    `flag:"limit" help:"Maximum rows from 1 through 1000" default:"100"`
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
	var add *cobra.Command
	add = clicky.AddNamedCommandWithContext("add", root, moduleAddOptions{}, func(ctx context.Context, options moduleAddOptions) ([]indexer.ModuleResult, error) {
		path := options.Path
		if path == "" {
			path = "."
		}
		pathOptions := addPathOptions{Path: path, IncludeWorkspaceUses: options.IncludeWorkspaceUses, NoWorkspaceUses: options.NoWorkspaceUses}
		if entity.OperationSurfaceFromContext(ctx) == "cli" {
			pathOptions.Input, pathOptions.Prompt = add.InOrStdin(), add.ErrOrStderr()
		}
		paths, err := addPaths(pathOptions)
		if err != nil {
			return nil, err
		}
		database, err := databaseFor(ctx)
		if err != nil {
			return nil, err
		}
		var results []indexer.ModuleResult
		for _, selected := range paths {
			indexed, err := indexModules(ctx, database, indexer.ModuleOptions{Path: selected, IncludeTests: options.IncludeTests, Force: options.Force})
			if err != nil {
				return nil, err
			}
			results = append(results, indexed...)
		}
		return results, nil
	})
	add.Use = "add [path]"
	add.Short = "Add Go modules below a directory and index them immediately"
	add.Args = cobra.MaximumNArgs(1)
	setModuleRoute(add, "modules/add")

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
	queryCommand.Use = "query <expression>"
	queryCommand.Short = "Run a PEG query against indexed modules"
	queryCommand.Args = cobra.ExactArgs(1)
	setModuleRoute(queryCommand, "modules/query")
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

	reindex := clicky.AddNamedCommandWithContext("reindex", root, moduleReindexOptions{}, func(ctx context.Context, options moduleReindexOptions) ([]indexer.ModuleResult, error) {
		path := options.Path
		if path == "" {
			path = "."
		}
		database, err := databaseFor(ctx)
		if err != nil {
			return nil, err
		}
		return indexModules(ctx, database, indexer.ModuleOptions{Path: path, IncludeTests: options.IncludeTests, Force: options.Force, ExistingOnly: true})
	})
	reindex.Use = "reindex [path]"
	reindex.Short = "Incrementally reindex registered Go modules"
	reindex.Args = cobra.MaximumNArgs(1)
	setModuleRoute(reindex, "modules/reindex")
}

func queryModules(ctx context.Context, database *gorm.DB, options moduleQueryOptions) (moduleQueryResult, error) {
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return moduleQueryResult{}, err
	}
	result, err := pipeline.RunModules(ctx, options.Expression, query.ModuleScopeOptions{
		RootKey: options.RootKey, Location: options.Location, SnapshotID: options.SnapshotID, Limit: options.Limit,
	})
	if err != nil {
		return moduleQueryResult{}, err
	}
	symbols := result.Symbols
	if symbols == nil {
		symbols = []query.ModuleSymbol{}
	}
	return moduleQueryResult{
		Operation: result.Operation, Total: result.Total, Matches: moduleQueryRows(result.Matches),
		Declarations: moduleQueryRows(result.Declarations), Symbols: symbols, Coverage: result.Coverage,
		Stages: result.Stages, limit: options.Limit,
	}, nil
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
			Kind: match.Kind, Root: match.RootKey, Symbol: match.Identifier.SymbolKey(),
			Location: match.Location, Source: source, SnapshotID: match.SnapshotID,
			Path: match.Path, Line: match.Line, Column: match.Column, EndLine: match.EndLine, EndColumn: match.EndColumn,
			Role: match.Role, SymbolID: match.SymbolID, EnclosingID: match.EnclosingID, EnclosingKey: match.EnclosingKey,
			Coverage: match.Coverage, Dispatch: match.Dispatch,
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

func addModules(ctx context.Context, database *gorm.DB, path string, force bool) ([]indexer.ModuleResult, error) {
	return indexModules(ctx, database, indexer.ModuleOptions{Path: path, Force: force})
}

func indexModules(ctx context.Context, database *gorm.DB, options indexer.ModuleOptions) ([]indexer.ModuleResult, error) {
	engine, err := indexer.New(database)
	if err != nil {
		return nil, err
	}
	return indexer.RunModulesTask(ctx, engine, options)
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
	var head storage.ModuleLocationHead
	if err := database.WithContext(ctx).Where("location_id = ?", location.ID).First(&head).Error; err != nil {
		return moduleRootRow{}, fmt.Errorf("load primary head for root %q: %w", root.RootKey, err)
	}
	return moduleRootRow{RootKey: root.RootKey, Name: root.Name, Location: location.CanonicalPath, SnapshotID: head.SnapshotID.String(), HeadVersion: head.Version}, nil
}
