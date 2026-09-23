package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/uir/storage"
)

type indexedFile struct {
	Root discoveredRoot
	File discoveredFile
	UIR  fileIndex
}

func (indexer *Indexer) Index(ctx context.Context, options Options) (Result, error) {
	if indexer == nil || indexer.database == nil {
		return Result{}, errors.New("UIR index database is required")
	}
	if err := validateOptions(&options); err != nil {
		return Result{}, err
	}
	started := time.Now()
	workspace, err := discoverWorkspace(ctx, options)
	if err != nil {
		return Result{}, err
	}
	timings := Timings{DiscoveryMS: millisecondsSince(started)}
	started = time.Now()
	project, err := indexer.ensureProject(ctx, options)
	if err != nil {
		return Result{}, err
	}
	previous, err := indexer.loadPrevious(ctx, project)
	if err != nil {
		return Result{}, err
	}
	timings.LoadMS = millisecondsSince(started)
	configurationHash := indexConfigurationHash(options)
	if unchanged(previous, workspace, configurationHash, options.Force) {
		result, err := indexer.unchangedResult(ctx, project, previous, workspace)
		result.Timings = timings
		return result, err
	}
	reparseAll := options.Force || previous == nil || previous.Snapshot.ConfigurationHash != configurationHash ||
		previous.Snapshot.ExtractorVersion != ExtractorVersion
	started = time.Now()
	files, parsed, reused, err := indexer.prepareFiles(ctx, workspace, previous, reparseAll)
	if err != nil {
		return Result{}, err
	}
	timings.PreparationMS = millisecondsSince(started)
	started = time.Now()
	result, err := indexer.persist(ctx, persistRequest{
		Project: project, Previous: previous, Workspace: workspace, Files: files,
		ConfigurationHash: configurationHash, ParsedFiles: parsed, ReusedFiles: reused,
	})
	result.Timings = timings
	result.Timings.PublicationMS = millisecondsSince(started)
	return result, err
}

func millisecondsSince(started time.Time) float64 {
	return float64(time.Since(started).Microseconds()) / 1000
}

func unchanged(previous *previousSnapshot, workspace discoveredWorkspace, configurationHash string, force bool) bool {
	return previous != nil && !force && previous.Snapshot.RevisionSetHash == workspace.RevisionSetHash &&
		previous.Snapshot.ConfigurationHash == configurationHash && previous.Snapshot.ExtractorVersion == ExtractorVersion
}

func (indexer *Indexer) prepareFiles(ctx context.Context, workspace discoveredWorkspace, previous *previousSnapshot, force bool) ([]indexedFile, int, int, error) {
	files := make([]indexedFile, 0, workspaceFileCount(workspace))
	selected := make(map[string]storage.Source)
	var reusable []storage.Source
	if !force && previous != nil {
		for _, root := range workspace.Roots {
			oldRoot, found := previous.Roots[root.RootKey]
			if !found {
				continue
			}
			for _, file := range root.Files {
				source, found := oldRoot.Sources[file.PathKey]
				if !found || source.ContentHash != file.ContentHash {
					continue
				}
				metadata, err := cachedFileIndex(source)
				if err != nil {
					return nil, 0, 0, err
				}
				if samePackagePath(metadata, file.PackagePath) {
					selected[sourceMapKey(root.RootKey, file.PathKey)] = source
					reusable = append(reusable, source)
				}
			}
		}
	}
	cached, err := indexer.loadCachedFiles(ctx, reusable)
	if err != nil {
		return nil, 0, 0, err
	}
	parsed, reused := 0, 0
	for _, root := range workspace.Roots {
		for _, file := range root.Files {
			if source, found := selected[sourceMapKey(root.RootKey, file.PathKey)]; found {
				files = append(files, indexedFile{Root: root, File: file, UIR: cached[source.ID]})
				reused++
				continue
			}
			extracted, err := extractGoFile(file.PathKey, file.PackagePath, file.Content)
			if err != nil {
				return nil, 0, 0, err
			}
			files = append(files, indexedFile{Root: root, File: file, UIR: extracted})
			parsed++
		}
	}
	return files, parsed, reused, nil
}

func samePackagePath(cached fileIndex, discovered string) bool {
	return cached.PackagePath == discovered ||
		(strings.HasSuffix(cached.PackageName, "_test") && cached.PackagePath == discovered+"_test")
}

func indexConfigurationHash(options Options) string {
	encoded, err := json.Marshal(struct {
		ExtractorVersion string `json:"extractor_version"`
		IncludeTests     bool   `json:"include_tests"`
		RootKey          string `json:"root_key"`
	}{ExtractorVersion: ExtractorVersion, IncludeTests: options.IncludeTests, RootKey: options.RootKey})
	if err != nil {
		panic(err)
	}
	return hashBytes(encoded)
}

func (indexer *Indexer) unchangedResult(ctx context.Context, project storage.Project, previous *previousSnapshot, workspace discoveredWorkspace) (Result, error) {
	fileCount := workspaceFileCount(workspace)
	result := Result{
		ProjectKey: project.ProjectKey, SnapshotID: previous.Snapshot.ID.String(), HeadVersion: previous.Head.Version,
		Roots: len(workspace.Roots), Files: fileCount, ReusedFiles: fileCount, Unchanged: true,
	}
	var count int64
	if err := indexer.database.WithContext(ctx).Model(&storage.Node{}).Where("snapshot_id = ?", previous.Snapshot.ID).Count(&count).Error; err != nil {
		return Result{}, fmt.Errorf("count unchanged nodes: %w", err)
	}
	result.Nodes = int(count)
	if err := indexer.database.WithContext(ctx).Model(&storage.Relationship{}).Where("snapshot_id = ?", previous.Snapshot.ID).Count(&count).Error; err != nil {
		return Result{}, fmt.Errorf("count unchanged relationships: %w", err)
	}
	result.Relationships = int(count)
	return result, nil
}

func workspaceFileCount(workspace discoveredWorkspace) int {
	count := 0
	for _, root := range workspace.Roots {
		count += len(root.Files)
	}
	return count
}
