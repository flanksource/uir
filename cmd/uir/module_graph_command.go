package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/entity"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/symboldiff"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

type moduleGraphOptions struct {
	Selector   string   `flag:"selector" args:"true" help:"Symbol expression naming the function or method at the root of the graph; also the first argument"`
	Symbol     string   `flag:"symbol" help:"Canonical symbol id of the root, as a graph node or candidate carries it"`
	Direction  string   `flag:"direction" help:"Calls to follow from the root: callees, callers, or both (the default)"`
	Depth      int      `flag:"depth"`
	Limit      int      `flag:"limit"`
	Exclude    []string `flag:"exclude"`
	Access     []string `flag:"access" help:"Edge types to follow: call (calls and dispatches), read, write (repeatable or comma separated; SQL files default to all, others to call)"`
	RootKey    string   `flag:"root" help:"Module path to query; defaults to the module of the current checkout"`
	Location   string   `flag:"location" help:"Registered checkout path; defaults to the current checkout"`
	SnapshotID string   `flag:"snapshot" help:"Explicit immutable snapshot UUID"`
	Since      string   `flag:"since" help:"Revision to compare against, resolved as uir diff resolves one: reports which callees of the root changed since its newest clean snapshot, and the path to each"`
	// SinceSnapshot is named as uir diff names its snapshot overrides.
	SinceSnapshot string `flag:"since-snapshot" help:"Snapshot UUID to use for --since instead of its newest clean snapshot"`
	File          string `flag:"file" help:"Draw the graph of one .go or .sql source file instead of an indexed root: a path, or - for stdin (command line only)"`
	Path          string `flag:"path" help:"Path of the --file source relative to its checkout, as indexed; its extension picks the language (required with --file -)"`
	Dialect       string `flag:"dialect" help:"SQL dialect of a .sql --file: tsql or postgres"`
	Index         bool   `flag:"index" help:"Resolve the callees and callers of --file in the index of the selected checkout; false draws the file alone without opening the database" default:"true"`
	allRootsOption
}

// moduleGraphLong documents the change query and the edges it cannot see.
const moduleGraphLong = `With --since <rev>, the graph is a change query: it walks the callees of the root in both
snapshots, marks added, removed and changed symbols, and reports added or removed call edges.
Each change carries a shortest path from the root in the snapshot where it was reached. The selected
graph stays at the top level and since.before holds the earlier graph. "changed: false" means neither
bounded walk reached a change. A module-cache dependency whose selected version changed is also marked.

The answer is only as complete as the call edges the index records. Known false negatives:
  - an implementation outside the import closure of the interface it satisfies
  - dispatch through generic or type-set interfaces
  - calls through method values, and through func-typed fields or variables
  - anything beyond --depth or --limit, behind an unresolved call, or in another root of the scope

With --file <path> or --file - (stdin, with --path naming it), the graph is drawn from one source
file, edited or never indexed, rather than from an indexed root: a .go file from its syntax, a .sql
file of T-SQL statements or T-SQL/PostgreSQL procedures and functions with --dialect. The selector
names one of the file's routines (by name or schema.name) and may be left out when it has one root.
An anonymous T-SQL script has one root named for the file and needs no selector.
  - --index=false draws the file alone without opening the database: every callee the file does not
    declare is an unresolved leaf, and a Go file's package is its package clause name.
  - by default the graph is backed by the index of the selected checkout: the file's functions shadow
    their indexed copies, a callee the file does not declare resolves by its qualified name into the
    index and the walk continues there, a name several symbols match is drawn as ambiguous dispatches,
    and the callers of a file function are the index callers of its indexed copy. A Go file's package
    path is that of the indexed document at --path.
  - a SQL site is located at the line its statement starts on, with the statement as its text, and a
    statement that cannot be lowered is reported in diagnostics.
--symbol and --since apply to indexed roots and are refused with --file, as is --file over HTTP.`

// moduleGraphResult is the graph envelope: the graph's own fields at the top level beside the
// stages and candidates. JSON and YAML retain every field while human-readable formats
// render the tree of callers and callees.
type moduleGraphResult struct {
	query.GraphResult
}

func registerModuleGraphCommand(root *cobra.Command) {
	var command *cobra.Command
	command = clicky.AddNamedCommandWithContext("graph", root, moduleGraphOptions{}, func(ctx context.Context, options moduleGraphOptions) (moduleGraphResult, error) {
		if options.File != "" || options.Path != "" || options.Dialect != "" || !options.Index {
			return graphFile(ctx, options, command.InOrStdin())
		}
		database, err := databaseFor(ctx)
		if err != nil {
			return moduleGraphResult{}, err
		}
		return graphModules(ctx, database, options)
	})
	// Use names no argument: the API generator would publish a second selector parameter beside the flag.
	command.Short = "Draw the callers and callees of one function or method, or the readers and writers of a data symbol"
	command.Long = moduleGraphLong
	command.Example = "  uir graph example.org/service/orders.Submit --direction callers --depth 3\n  uir graph example.org/service/orders.Submit --exclude external\n  uir graph example.org/service/orders.Order.Total --direction callers --access read,write\n  uir graph --symbol <id> --exclude none --format json\n  uir graph example.org/service/orders.Submit --since v1.4.0 --depth 6\n  cat orders.go | uir graph --file - --path service/orders/orders.go Submit --depth 3\n  uir graph --file service/orders/orders.go Submit --index=false\n  uir graph --file procs.sql --dialect tsql dbo.SubmitOrder --access call,read,write --index=false"
	command.Args = cobra.MaximumNArgs(1)
	command.Flags().Lookup("depth").Usage = fmt.Sprintf("Calls away from the root, from 1 through %d (default %d)", graph.MaxDepth, graph.DefaultDepth)
	command.Flags().Lookup("limit").Usage = fmt.Sprintf("Maximum nodes from 1 through %d (default %d)", graph.MaxLimit, graph.DefaultLimit)
	command.Flags().Lookup("exclude").Usage = fmt.Sprintf(
		"Comma-separated package patterns whose nodes are left out: an import path, a path ending in /..., or %s, %s, %s; %s alone excludes nothing (default %s)",
		query.ExcludeStd, query.ExcludeBuiltin, query.ExcludeExternal, query.ExcludeNone, strings.Join(query.DefaultGraphExclusions, ","))
	setModuleRoute(command, "modules/graph")
	command.Annotations["clicky/operation-method"] = http.MethodGet
}

func graphModules(ctx context.Context, database *gorm.DB, options moduleGraphOptions) (moduleGraphResult, error) {
	access, err := graph.ParseAccess(options.Access)
	if err != nil {
		return moduleGraphResult{}, entity.NewStatusError(http.StatusBadRequest, "invalid_query", err.Error())
	}
	scope, err := moduleScope(ctx, database, checkoutDefaults{
		Root: options.RootKey, Location: options.Location, Snapshot: options.SnapshotID, All: options.All, Required: options.Since != "",
	})
	if err != nil {
		return moduleGraphResult{}, err
	}
	since, err := graphSince(ctx, database, options, scope)
	if err != nil {
		return moduleGraphResult{}, err
	}
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return moduleGraphResult{}, err
	}
	result, err := pipeline.Graph(ctx, query.GraphOptions{
		Selector: options.Selector, Symbol: options.Symbol, Access: access,
		Direction: graph.Direction(options.Direction), Depth: options.Depth, Limit: options.Limit, Exclude: options.Exclude,
		Since: since, Scope: scope,
	})
	if err != nil {
		return moduleGraphResult{}, queryStatusError(err)
	}
	return moduleGraphResult{GraphResult: result}, nil
}

// graphFile draws the graph of --file: alone with --index=false, else backed by the index of the
// selected checkout.
func graphFile(ctx context.Context, options moduleGraphOptions, stdin io.Reader) (moduleGraphResult, error) {
	file, err := readGraphFile(ctx, options, stdin)
	if err != nil {
		return moduleGraphResult{}, err
	}
	access, err := graph.ParseAccess(options.Access)
	if err != nil {
		return moduleGraphResult{}, entity.NewStatusError(http.StatusBadRequest, "invalid_query", err.Error())
	}
	request := query.GraphOptions{
		Selector: options.Selector, Symbol: options.Symbol, Access: access, File: &file,
		Direction: graph.Direction(options.Direction), Depth: options.Depth, Limit: options.Limit, Exclude: options.Exclude,
	}
	if !options.Index {
		result, err := query.StandaloneGraph(ctx, request)
		return moduleGraphResult{GraphResult: result}, queryStatusError(err)
	}
	database, err := databaseFor(ctx)
	if err != nil {
		return moduleGraphResult{}, err
	}
	if request.Scope, err = moduleScope(ctx, database, checkoutDefaults{
		Root: options.RootKey, Location: options.Location, Snapshot: options.SnapshotID, All: options.All,
	}); err != nil {
		return moduleGraphResult{}, err
	}
	if options.Path == "" {
		if file.Path, err = checkoutRelative(request.Scope.Location, file.Path); err != nil {
			return moduleGraphResult{}, err
		}
	}
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return moduleGraphResult{}, err
	}
	result, err := pipeline.Graph(ctx, request)
	return moduleGraphResult{GraphResult: result}, queryStatusError(err)
}

// readGraphFile reads --file from disk, or from stdin for -, refusing an HTTP request, whose file
// would be read from the server's disk. Its path is --path, else the file argument.
func readGraphFile(ctx context.Context, options moduleGraphOptions, stdin io.Reader) (query.GraphFile, error) {
	invalid := func(message string) error {
		failure := entity.NewStatusError(http.StatusBadRequest, "invalid_query", message)
		failure.Hint = "Pass --file with a .go or .sql source, or - for stdin with --path naming it; --path, --dialect and --index describe --file."
		return failure
	}
	switch {
	case detachedRequest(ctx):
		return query.GraphFile{}, invalid("an HTTP graph cannot read file, which names a path on the server; draw a file graph on the command line")
	case options.File == "":
		return query.GraphFile{}, invalid("--path, --dialect and --index=false describe --file, which is missing")
	case options.Since != "" || options.SinceSnapshot != "":
		return query.GraphFile{}, invalid("--since compares indexed snapshots of a root, not a file")
	}
	file := query.GraphFile{Path: options.Path, Dialect: options.Dialect}
	var err error
	if options.File == "-" {
		if file.Content, err = io.ReadAll(stdin); err != nil {
			return query.GraphFile{}, fmt.Errorf("read the graph file from stdin: %w", err)
		}
		return file, nil
	}
	if file.Content, err = os.ReadFile(options.File); err != nil {
		return query.GraphFile{}, fmt.Errorf("read the graph file %q: %w", options.File, err)
	}
	if file.Path == "" {
		file.Path = options.File
	}
	return file, nil
}

// checkoutRelative is path relative to the checkout it lies in, as the index keys its documents, or
// path as given when it lies outside the checkout or no checkout is selected.
func checkoutRelative(checkout, path string) (string, error) {
	if checkout == "" {
		return path, nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve the graph file %q: %w", path, err)
	}
	if absolute, err = filepath.EvalSymlinks(absolute); err != nil {
		return "", fmt.Errorf("canonicalize the graph file %q: %w", path, err)
	}
	relative, err := filepath.Rel(checkout, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return path, nil
	}
	return filepath.ToSlash(relative), nil
}

// graphSince selects the snapshot of --since in the scope's root, as uir diff selects one side of a
// range; nil without --since.
func graphSince(ctx context.Context, database *gorm.DB, options moduleGraphOptions, scope query.ModuleScopeOptions) (*query.GraphSince, error) {
	var failure *entity.StatusError
	switch {
	case options.Since == "" && options.SinceSnapshot != "":
		failure = entity.NewStatusError(http.StatusBadRequest, "invalid_query", "since-snapshot overrides the snapshot of since, which is missing")
	case options.Since == "":
		return nil, nil
	case scope.RootKey == "":
		failure = entity.NewStatusError(http.StatusBadRequest, "missing_root", "since resolves its revision in one root, and none is selected")
	}
	if failure != nil {
		failure.Hint = "Pass since with the revision to compare against, and root with the module it belongs to."
		return nil, failure
	}
	snapshot, err := symboldiff.RevisionSnapshot(ctx, database, symboldiff.RevisionSnapshotOptions{
		RootKey: scope.RootKey, Location: scope.Location, Revision: options.Since, Snapshot: options.SinceSnapshot, SnapshotFlag: "--since-snapshot",
	})
	if err != nil {
		return nil, err
	}
	return &query.GraphSince{Revision: options.Since, SnapshotID: snapshot.ID}, nil
}

// queryStatusError gives a query failure the HTTP status it answers with: 400 for a query that
// cannot be run as written, 404 for a symbol that is not indexed. Any other error is returned as is.
func queryStatusError(err error) error {
	var invalid *query.InvalidQueryError
	if errors.As(err, &invalid) {
		failure := entity.NewStatusError(http.StatusBadRequest, "invalid_query", invalid.Message)
		failure.Hint = invalid.Hint
		if invalid.Line > 0 {
			failure.Context = map[string]any{"line": invalid.Line, "column": invalid.Column}
		}
		return failure
	}
	var unresolved *query.UnresolvedSymbolError
	if errors.As(err, &unresolved) {
		failure := entity.NewStatusError(http.StatusNotFound, "symbol_not_found", unresolved.Error())
		failure.Hint = "Check the symbol spelling or select a snapshot where it is indexed."
		return failure
	}
	return err
}
