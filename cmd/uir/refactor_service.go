package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/entity"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/spf13/cobra"
)

type refactorPreview struct {
	Diff        string   `json:"diff"`
	PreviewHash string   `json:"preview_hash"`
	Files       []string `json:"files"`
}

type refactorApplyResult struct {
	Applied   bool                   `json:"applied"`
	Files     []string               `json:"files"`
	Snapshots []indexer.ModuleResult `json:"snapshots"`
	RunID     string                 `json:"run_id"`
}

// gopatchTimeout bounds one gopatch run. Gopatch runs detached from the request: a client that goes
// away must not kill it halfway through rewriting files.
const gopatchTimeout = 5 * time.Minute

type preparedRefactor struct {
	args     []string
	location string
	dsn      string
	diff     string
	hash     string
	files    []string
}

var refactorHunkHeader = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@`)

func registerRefactorCommands(root *cobra.Command, runtime *commandRuntime) {
	preview := clicky.AddNamedCommandWithContext("refactor-preview", root, refactorOptions{}, func(ctx context.Context, options refactorOptions) (refactorPreview, error) {
		prepared, err := prepareRefactor(ctx, runtime, options)
		if err != nil {
			return refactorPreview{}, err
		}
		return refactorPreview{Diff: prepared.diff, PreviewHash: prepared.hash, Files: prepared.files}, nil
	})
	preview.Short = "Preview a gopatch rename or move from the explorer"
	setModuleRoute(preview, "modules/refactor/preview")
	preview.Annotations["clicky/operation-method"] = http.MethodPost

	apply := clicky.AddNamedCommandWithContext("refactor-apply", root, refactorOptions{}, func(ctx context.Context, options refactorOptions) (refactorApplyResult, error) {
		if options.PreviewHash == "" {
			return refactorApplyResult{}, entity.NewStatusError(http.StatusBadRequest, "missing_preview", "Preview the refactor before applying it")
		}
		prepared, err := prepareRefactor(ctx, runtime, options)
		if err != nil {
			return refactorApplyResult{}, err
		}
		if prepared.hash != options.PreviewHash {
			return refactorApplyResult{}, entity.NewStatusError(http.StatusConflict, "stale_preview", "Source or refactor inputs changed; preview again")
		}
		if _, err := runGopatch(ctx, runtime.GopatchBin, prepared.location, prepared.args, prepared.dsn); err != nil {
			return refactorApplyResult{}, err
		}
		runID, snapshots, err := reindexRefactorFiles(ctx, runtime, prepared.files)
		if err != nil {
			return refactorApplyResult{}, entity.NewStatusErrorf(http.StatusInternalServerError, "reindex_failed",
				"Gopatch applied the refactor to %s, but reindexing failed: %v", strings.Join(prepared.files, ", "), err)
		}
		return refactorApplyResult{Applied: true, Files: prepared.files, Snapshots: snapshots, RunID: runID}, nil
	})
	apply.Short = "Apply a reviewed gopatch rename or move"
	setModuleRoute(apply, "modules/refactor/apply")
	apply.Annotations["clicky/operation-method"] = http.MethodPost
}

func prepareRefactor(ctx context.Context, runtime *commandRuntime, options refactorOptions) (preparedRefactor, error) {
	database, err := runtime.Database(ctx)
	if err != nil {
		return preparedRefactor{}, err
	}
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return preparedRefactor{}, err
	}
	browse, err := pipeline.BrowseModules(ctx, query.ModuleScopeOptions{SnapshotID: options.Snapshot})
	if err != nil {
		return preparedRefactor{}, err
	}
	var source *query.ModuleSourceView
	for index := range browse.Sources {
		if browse.Sources[index].ID == options.Source {
			source = &browse.Sources[index]
			break
		}
	}
	if source == nil {
		return preparedRefactor{}, entity.NewStatusError(http.StatusBadRequest, "source_not_found", "Selected source is not in this snapshot")
	}
	engine, err := indexer.New(database)
	if err != nil {
		return preparedRefactor{}, err
	}
	includeTests, err := engine.HeadIncludesTests(ctx, source.Location)
	if err != nil {
		return preparedRefactor{}, fmt.Errorf("refactor requires a current, fully indexed checkout: %w", err)
	}
	current, err := engine.CheckCurrent(ctx, source.Location, includeTests)
	if err != nil {
		return preparedRefactor{}, fmt.Errorf("refactor requires a current, fully indexed checkout: %w", err)
	}
	if current.SnapshotID != options.Snapshot {
		return preparedRefactor{}, entity.NewStatusError(http.StatusConflict, "historical_snapshot", "Select the current checkout head before refactoring")
	}
	var node *query.ModuleNodeView
	if options.Node != "" {
		for index := range browse.Nodes {
			if browse.Nodes[index].ID == options.Node && browse.Nodes[index].SourceID == source.ID {
				node = &browse.Nodes[index]
				break
			}
		}
		if node == nil {
			return preparedRefactor{}, entity.NewStatusError(http.StatusBadRequest, "symbol_not_found", "Selected symbol is not in this source")
		}
	}
	dsn, err := refactorIndexDSN(runtime.DSN)
	if err != nil {
		return preparedRefactor{}, err
	}
	args, err := refactorArgs(options, *source, node, dsn, runtime.Schema)
	if err != nil {
		return preparedRefactor{}, err
	}
	previewArgs := append(append([]string{}, args...), "--diff")
	diff, err := runGopatch(ctx, runtime.GopatchBin, source.Location, previewArgs, dsn)
	if err != nil {
		return preparedRefactor{}, err
	}
	files, err := refactorDiffFiles(diff, source.Location)
	if err != nil {
		return preparedRefactor{}, err
	}
	fingerprint, err := json.Marshal(struct {
		Snapshot string
		Args     []string
		Diff     string
	}{options.Snapshot, args, diff})
	if err != nil {
		return preparedRefactor{}, err
	}
	hash := sha256.Sum256(fingerprint)
	return preparedRefactor{args: args, location: source.Location, dsn: dsn, diff: diff, hash: hex.EncodeToString(hash[:]), files: files}, nil
}

func refactorIndexDSN(dsn string) (string, error) {
	prefix := ""
	if strings.HasPrefix(strings.ToLower(dsn), "sqlite://") {
		prefix, dsn = dsn[:len("sqlite://")], dsn[len("sqlite://"):]
	} else if !strings.EqualFold(filepath.Ext(strings.Split(dsn, "?")[0]), ".db") {
		return dsn, nil
	}
	path, query, hasQuery := strings.Cut(dsn, "?")
	if strings.HasPrefix(path, "file:") {
		parsed, err := url.Parse(path)
		if err != nil {
			return "", fmt.Errorf("parse UIR SQLite DSN: %w", err)
		}
		path = parsed.Path
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve UIR SQLite DSN: %w", err)
	}
	if hasQuery {
		return prefix + absolute + "?" + query, nil
	}
	return prefix + absolute, nil
}

func runGopatch(ctx context.Context, binary, directory string, args []string, dsn string) (string, error) {
	if binary == "" {
		return "", errors.New("gopatch binary is not configured; set serve --gopatch-bin")
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gopatchTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = directory
	output, err := command.Output()
	if err == nil {
		return string(output), nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		diagnostic := string(exit.Stderr)
		if dsn != "" {
			diagnostic = strings.ReplaceAll(diagnostic, dsn, "[UIR DSN]")
		}
		diagnostic = strings.TrimSpace(diagnostic)
		return "", fmt.Errorf("gopatch failed: %s: %w", diagnostic, err)
	}
	return "", fmt.Errorf("run gopatch binary %q: %w", binary, err)
}

func refactorDiffFiles(diff, directory string) ([]string, error) {
	files := map[string]bool{}
	oldLines, newLines := 0, 0
	for _, line := range strings.Split(diff, "\n") {
		if oldLines > 0 || newLines > 0 {
			if line == "" {
				return nil, errors.New("gopatch diff ended inside a hunk")
			}
			switch line[0] {
			case ' ':
				oldLines--
				newLines--
			case '-':
				oldLines--
			case '+':
				newLines--
			case '\\':
				continue
			default:
				return nil, fmt.Errorf("invalid gopatch diff hunk line %q", line)
			}
			if oldLines < 0 || newLines < 0 {
				return nil, errors.New("gopatch diff hunk exceeds its declared line count")
			}
			continue
		}
		if match := refactorHunkHeader.FindStringSubmatch(line); match != nil {
			oldLines, newLines = 1, 1
			var err error
			if match[1] != "" {
				oldLines, err = strconv.Atoi(match[1])
				if err != nil {
					return nil, fmt.Errorf("invalid gopatch diff hunk size: %w", err)
				}
			}
			if match[2] != "" {
				newLines, err = strconv.Atoi(match[2])
				if err != nil {
					return nil, fmt.Errorf("invalid gopatch diff hunk size: %w", err)
				}
			}
			continue
		}
		if !strings.HasPrefix(line, "--- ") && !strings.HasPrefix(line, "+++ ") {
			continue
		}
		path := strings.TrimSpace(line[4:])
		if path == "/dev/null" {
			continue
		}
		if path == "" || filepath.Ext(path) != ".go" {
			return nil, fmt.Errorf("gopatch diff has an invalid path %q", path)
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(directory, path)
		}
		files[filepath.Clean(path)] = true
	}
	if oldLines != 0 || newLines != 0 {
		return nil, errors.New("gopatch diff ended inside a hunk")
	}
	if len(files) == 0 {
		return nil, errors.New("gopatch diff has no changed Go files")
	}
	ordered := make([]string, 0, len(files))
	for path := range files {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	return ordered, nil
}

// reindexRefactorFiles reindexes, as one run, every registered checkout holding a refactored file,
// each under the test setting its head was indexed with. The run starts under runContext, so a client
// that goes away does not stop it, and the apply waits for it under its own context.
func reindexRefactorFiles(ctx context.Context, runtime *commandRuntime, files []string) (string, []indexer.ModuleResult, error) {
	database, err := runtime.Database(ctx)
	if err != nil {
		return "", nil, err
	}
	engine, err := indexer.New(database)
	if err != nil {
		return "", nil, err
	}
	var locations []storage.ModuleLocation
	if err := database.WithContext(ctx).Order("canonical_path").Find(&locations).Error; err != nil {
		return "", nil, fmt.Errorf("list checkout locations after refactor: %w", err)
	}
	var requests []indexer.ModuleOptions
	for _, location := range locations {
		if !locationContainsAny(location.CanonicalPath, files) {
			continue
		}
		includeTests, err := engine.HeadIncludesTests(ctx, location.CanonicalPath)
		if err != nil {
			return "", nil, err
		}
		requests = append(requests, indexer.ModuleOptions{Path: location.CanonicalPath, ExactLocation: location.CanonicalPath,
			IncludeTests: includeTests, ExistingOnly: true, Reason: storage.ReasonRefactor})
	}
	if len(requests) == 0 {
		return "", nil, fmt.Errorf("no registered checkout holds the refactored files %s", strings.Join(files, ", "))
	}
	runCtx, err := runContext(ctx)
	if err != nil {
		return "", nil, err
	}
	run, err := indexer.StartModulesTask(runCtx, engine, requests)
	if err != nil {
		return "", nil, err
	}
	results, err := run.Wait(ctx)
	return run.ID(), results, err
}

func locationContainsAny(root string, files []string) bool {
	for _, file := range files {
		relative, err := filepath.Rel(root, file)
		if err == nil && filepath.IsLocal(relative) {
			return true
		}
	}
	return false
}
