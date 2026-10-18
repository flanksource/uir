package query

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/flanksource/uir/graph"
)

// GraphFile is one source file a call graph is drawn from, read from disk or stdin rather than the
// index.
type GraphFile struct {
	// Path names the file; its extension picks the language, .go or .sql. For a graph backed by the
	// index it is the file's path relative to its checkout, as the snapshot holds it.
	Path    string
	Content []byte
	// Dialect is the dialect of a .sql file, tsql or postgres; a Go file takes none.
	Dialect string
}

// FileDiagnostic is a statement of a file graph's source that could not be lowered, so the graph
// draws none of its calls, reads or writes.
type FileDiagnostic struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

const (
	fileLanguageGo  = "go"
	fileLanguageSQL = "sql"
)

// sqlDialects are the dialects a .sql file graph is lowered in.
var sqlDialects = []string{"tsql", "postgres"}

func invalidFile(format string, arguments ...any) error {
	return &InvalidQueryError{Message: fmt.Sprintf(format, arguments...), Hint: "Give file a .go or .sql source and path naming it (required for stdin, and relative to its checkout when the graph is backed by the index), dialect tsql or postgres for SQL, and a selector when the file has several roots. symbol and since apply to indexed roots, not to a file."}
}

// language is the language the file's extension names, checked against its dialect.
func (file GraphFile) language() (string, error) {
	if file.Path == "" {
		return "", invalidFile("a file graph requires the file's path, whose extension picks its language; pass --path with a file read from stdin")
	}
	switch extension := filepath.Ext(file.Path); extension {
	case ".go":
		if file.Dialect != "" {
			return "", invalidFile("dialect %q applies to SQL, not to the Go file %s", file.Dialect, file.Path)
		}
		return fileLanguageGo, nil
	case ".sql":
		if !slices.Contains(sqlDialects, file.Dialect) {
			return "", invalidFile("the SQL file %s requires dialect tsql or postgres, not %q", file.Path, file.Dialect)
		}
		return fileLanguageSQL, nil
	default:
		return "", invalidFile("file %s has extension %q, not .go or .sql", file.Path, extension)
	}
}

func (options GraphOptions) validateFile() error {
	switch {
	case options.Symbol != "":
		return invalidFile("a file graph is rooted at one of the file's routines or scripts by selector, not by symbol")
	case options.Since != nil:
		return invalidFile("a change query compares indexed snapshots, not a file")
	}
	_, err := options.File.language()
	return err
}

// StandaloneGraph draws the graph of options.File alone, opening no index: the file's functions and
// the calls, reads and writes between them, with each callee the file does not declare drawn as an
// unresolved leaf. A Go file's package path is its package clause name.
func StandaloneGraph(ctx context.Context, options GraphOptions) (GraphResult, error) {
	if options.File == nil {
		return GraphResult{}, errors.New("a standalone graph requires a file")
	}
	options, exclusions, err := options.normalized()
	if err != nil {
		return GraphResult{}, err
	}
	lowered, err := lowerGraphFile(*options.File, "")
	if err != nil {
		return GraphResult{}, err
	}
	source, err := newFileGraphSource(ctx, lowered, nil)
	if err != nil {
		return GraphResult{}, err
	}
	return buildFileGraph(ctx, source, options, exclusions, newGraphResult(options))
}

// indexedFileGraph draws the graph of options.File backed by the selected snapshots: the file's
// functions shadow their indexed copies, a callee it does not declare resolves by name into the
// index, where the walk continues, and the callers of a file function include the index callers of
// its indexed copy.
func (pipeline *Pipeline) indexedFileGraph(ctx context.Context, options GraphOptions, exclusions graphExclusions) (GraphResult, error) {
	session, trace := pipeline.session()
	clock := newStageClock()
	selection, err := session.moduleScopes(ctx, options.Scope, false)
	if err != nil {
		return GraphResult{}, err
	}
	if len(selection.scopes) == 0 {
		return GraphResult{}, &InvalidQueryError{
			Message: fmt.Sprintf("no indexed snapshot is selected to resolve the callees of %s in", options.File.Path),
			Hint:    "Select an indexed checkout with root, location or snapshot, or pass --index=false to draw the file alone.",
		}
	}
	coverage, err := session.scopeCoverage(ctx, selection.scopes)
	if err != nil {
		return GraphResult{}, err
	}
	base, err := newIndexContext(ctx, session, selection.scopes)
	if err != nil {
		return GraphResult{}, err
	}
	clock.lap("index")
	index := newCompactIndex(base)
	sources := newScopeSources(base)
	reader := newGuardReader(session.database, sources)
	indexSource := newIndexGraphSource(index, sources, reader, options.Access)
	source, err := indexedFileSource(ctx, base, indexSource, *options.File)
	if err != nil {
		return GraphResult{}, err
	}
	result := newGraphResult(options)
	result.Stages = append(result.Stages, ResolutionStage{Name: "scope", Value: fmt.Sprintf("%d snapshots", len(selection.scopes))}, coverageStage(coverage))
	if result, err = buildFileGraph(ctx, source, options, exclusions, result); err != nil {
		return GraphResult{}, err
	}
	result.Stages = append(result.Stages, indexSource.stages...)
	if unreadable := reader.unreadablePaths(); len(unreadable) > 0 {
		result.Omitted.UnreadableSource = unreadable
	}
	clock.lap("graph")
	result.Stages = append(result.Stages, clock.stages(trace)...)
	return result, nil
}

// indexedFileSource lowers the file in the package the selected snapshots hold it in, for Go, and
// serves it over the index.
func indexedFileSource(ctx context.Context, index *indexContext, indexSource *indexGraphSource, file GraphFile) (*fileGraphSource, error) {
	language, err := file.language()
	if err != nil {
		return nil, err
	}
	packagePath := ""
	if language == fileLanguageGo {
		if packagePath, err = indexedPackagePath(index, file.Path); err != nil {
			return nil, err
		}
	}
	lowered, err := lowerGraphFile(file, packagePath)
	if err != nil {
		return nil, err
	}
	return newFileGraphSource(ctx, lowered, indexSource)
}

// indexedPackagePath is the package path of the active document the selected snapshots hold at
// path. A path no snapshot holds, or that two hold in different packages, is refused rather than
// guessed at.
func indexedPackagePath(index *indexContext, path string) (string, error) {
	var packages []string
	for _, scope := range index.scopes {
		for _, document := range scope.documents {
			if document.Document.PathKey == path && !slices.Contains(packages, document.Document.PackagePath) {
				packages = append(packages, document.Document.PackagePath)
			}
		}
	}
	switch len(packages) {
	case 1:
		return packages[0], nil
	case 0:
		return "", &InvalidQueryError{
			Message: fmt.Sprintf("no selected snapshot holds a document at %s, so its package path is unknown", path),
			Hint:    "Pass --path with the file's path relative to its checkout, as it is indexed, or --index=false to draw the file alone.",
		}
	}
	return "", &InvalidQueryError{
		Message: fmt.Sprintf("the selected snapshots hold %s in %d packages: %v", path, len(packages), packages),
		Hint:    "Select one snapshot with root, location or snapshot.",
	}
}

func newGraphResult(options GraphOptions) GraphResult {
	return GraphResult{
		Graph:   graph.Graph{Roots: []string{}, Nodes: []graph.Node{}, Edges: []graph.Edge{}},
		Exclude: options.Exclude, Packages: []GraphPackage{}, Candidates: []ModuleSymbol{}, Stages: []ResolutionStage{},
	}
}

// buildFileGraph walks the graph around the file function the selector names.
func buildFileGraph(ctx context.Context, source *fileGraphSource, options GraphOptions, exclusions graphExclusions, result GraphResult) (GraphResult, error) {
	lowered := source.lowered
	root, err := lowered.root(options.Selector)
	if err != nil {
		return GraphResult{}, err
	}
	built, err := graph.Build(ctx, source, []string{root.identifier.IdentityKey()}, graph.Options{
		Direction: options.Direction, Depth: options.Depth, Limit: options.Limit, Access: options.Access,
		Exclude: func(node graph.Node) bool { return exclusions.excludes(source.notes.packageFacts(node.Group)) },
	})
	if err != nil {
		return GraphResult{}, err
	}
	result.Graph = *built
	result.Packages = graphPackages(built, source.facts, exclusions)
	result.Diagnostics = lowered.diagnostics
	result.Stages = append(result.Stages,
		ResolutionStage{Name: "file", Value: fmt.Sprintf("%s: %d roots, %d relationships, %d diagnostics", lowered.path, len(lowered.functions), len(lowered.relationships), len(lowered.diagnostics))},
		ResolutionStage{Name: "resolve", Value: root.name},
		ResolutionStage{Name: "graph", Value: fmt.Sprintf("%d nodes, %d edges", len(built.Nodes), len(built.Edges))})
	return result, nil
}
