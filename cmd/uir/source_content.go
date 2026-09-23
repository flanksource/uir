package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/flanksource/clicky/entity"
	"github.com/flanksource/uir/storage"
)

type sourceContentOptions struct{}

func (sourceContentOptions) ClickyActionFlags() {}

type sourceContent struct {
	SourceID   string `json:"source_id"`
	Path       string `json:"path"`
	Content    string `json:"content,omitempty"`
	Repository string `json:"repository,omitempty"`
	Revision   string `json:"revision,omitempty"`
	Origin     string `json:"origin"`
}

var gitRevision = regexp.MustCompile(`^[a-fA-F0-9]{40,64}$`)

func readSourceContent(ctx context.Context, id string, _ sourceContentOptions) (sourceContent, error) {
	database, err := databaseFor(ctx)
	if err != nil {
		return sourceContent{}, err
	}
	var source storage.Source
	if err := database.WithContext(ctx).Where("id = ?", id).First(&source).Error; err != nil {
		return sourceContent{}, browseError(err, "source", id)
	}
	var root storage.Root
	if err := database.WithContext(ctx).Where("id = ?", source.RootID).First(&root).Error; err != nil {
		return sourceContent{}, browseError(err, "root", source.RootID.String())
	}
	if !filepath.IsLocal(source.PathKey) {
		return sourceContent{}, entity.NewStatusErrorf(http.StatusBadRequest, "invalid_source_path", "source %q is outside root", source.PathKey)
	}
	result := sourceContent{SourceID: id, Path: source.PathKey}
	if root.RepositoryURI != nil {
		result.Repository = *root.RepositoryURI
	}
	if root.Revision != nil {
		result.Revision = *root.Revision
	}
	if root.LocalPath == nil || *root.LocalPath == "" {
		if result.Repository == "" || result.Revision == "" {
			return sourceContent{}, entity.NewStatusErrorf(http.StatusNotFound, "source_unavailable", "source %q has no local path or Git ref", source.PathKey)
		}
		result.Origin = "remote"
		return result, nil
	}
	absolute := filepath.Join(*root.LocalPath, filepath.FromSlash(source.PathKey))
	if result.Revision != "" {
		if !gitRevision.MatchString(result.Revision) {
			return sourceContent{}, entity.NewStatusErrorf(http.StatusBadRequest, "invalid_revision", "source %q has an invalid Git revision", source.PathKey)
		}
		command := exec.CommandContext(ctx, "git", "-C", *root.LocalPath, "rev-parse", "--show-toplevel")
		repoTop, err := command.Output()
		if err != nil {
			return sourceContent{}, fmt.Errorf("locate Git root for %q: %w", source.PathKey, err)
		}
		resolvedTop, err := filepath.EvalSymlinks(strings.TrimSpace(string(repoTop)))
		if err != nil {
			return sourceContent{}, fmt.Errorf("resolve Git root for %q: %w", source.PathKey, err)
		}
		resolvedRoot, err := filepath.EvalSymlinks(*root.LocalPath)
		if err != nil {
			return sourceContent{}, fmt.Errorf("resolve local Git root for %q: %w", source.PathKey, err)
		}
		relative, err := filepath.Rel(resolvedTop, filepath.Join(resolvedRoot, filepath.FromSlash(source.PathKey)))
		if err != nil || !filepath.IsLocal(relative) {
			return sourceContent{}, entity.NewStatusErrorf(http.StatusBadRequest, "invalid_source_path", "source %q is outside root", source.PathKey)
		}
		command = exec.CommandContext(ctx, "git", "-C", resolvedTop, "cat-file", "blob", result.Revision+":"+filepath.ToSlash(relative))
		content, err := command.Output()
		if err != nil {
			return sourceContent{}, fmt.Errorf("read Git source %s at %s: %w", source.PathKey, result.Revision, err)
		}
		result.Origin, result.Content = "git", string(content)
		return result, nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(*root.LocalPath)
	if err != nil {
		return sourceContent{}, fmt.Errorf("resolve local root %q: %w", *root.LocalPath, err)
	}
	resolvedFile, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return sourceContent{}, fmt.Errorf("resolve source %q: %w", source.PathKey, err)
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedFile)
	if err != nil || !filepath.IsLocal(relative) {
		return sourceContent{}, entity.NewStatusErrorf(http.StatusBadRequest, "invalid_source_path", "source %q is outside root", source.PathKey)
	}
	content, err := os.ReadFile(resolvedFile)
	if err != nil {
		return sourceContent{}, fmt.Errorf("read local source %q: %w", source.PathKey, err)
	}
	result.Origin, result.Content = "local", string(content)
	return result, nil
}
