package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	workspace, err := discoverWorkspace(ctx, options)
	if err != nil {
		return Result{}, err
	}
	project, err := indexer.ensureProject(ctx, options)
	if err != nil {
		return Result{}, err
	}
	previous, err := indexer.loadPrevious(ctx, project)
	if err != nil {
		return Result{}, err
	}
	configurationHash := indexConfigurationHash(options)
	if unchanged(previous, workspace, configurationHash, options.Force) {
		return indexer.unchangedResult(ctx, project, previous, workspace)
	}
	reparseAll := options.Force || previous == nil || previous.Snapshot.ConfigurationHash != configurationHash ||
		previous.Snapshot.ExtractorVersion != ExtractorVersion
	files, parsed, reused, err := indexer.prepareFiles(ctx, workspace, previous, reparseAll)
	if err != nil {
		return Result{}, err
	}
	return indexer.persist(ctx, persistRequest{
		Project: project, Previous: previous, Workspace: workspace, Files: files,
		ConfigurationHash: configurationHash, ParsedFiles: parsed, ReusedFiles: reused,
	})
}

func unchanged(previous *previousSnapshot, workspace discoveredWorkspace, configurationHash string, force bool) bool {
	return previous != nil && !force && previous.Snapshot.RevisionSetHash == workspace.RevisionSetHash &&
		previous.Snapshot.ConfigurationHash == configurationHash && previous.Snapshot.ExtractorVersion == ExtractorVersion
}

func (indexer *Indexer) prepareFiles(ctx context.Context, workspace discoveredWorkspace, previous *previousSnapshot, force bool) ([]indexedFile, int, int, error) {
	files := make([]indexedFile, 0, workspaceFileCount(workspace))
	parsed, reused := 0, 0
	for _, root := range workspace.Roots {
		for _, file := range root.Files {
			indexed, wasReused, err := indexer.prepareFile(ctx, root, file, previous, force)
			if err != nil {
				return nil, 0, 0, err
			}
			files = append(files, indexed)
			if wasReused {
				reused++
			} else {
				parsed++
			}
		}
	}
	return files, parsed, reused, nil
}

func (indexer *Indexer) prepareFile(ctx context.Context, root discoveredRoot, file discoveredFile, previous *previousSnapshot, force bool) (indexedFile, bool, error) {
	if !force && previous != nil {
		if oldRoot, ok := previous.Roots[root.RootKey]; ok {
			if oldSource, ok := oldRoot.Sources[file.PathKey]; ok && oldSource.ContentHash == file.ContentHash {
				cached, err := indexer.copyFile(ctx, oldSource)
				if err != nil {
					return indexedFile{}, false, err
				}
				if samePackagePath(cached, file.PackagePath) {
					return indexedFile{Root: root, File: file, UIR: cached}, true, nil
				}
			}
		}
	}
	extracted, err := extractGoFile(file.PathKey, file.PackagePath, file.Content)
	if err != nil {
		return indexedFile{}, false, err
	}
	return indexedFile{Root: root, File: file, UIR: extracted}, false, nil
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
