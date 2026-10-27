package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/flanksource/uir/internal/buildconstraint"
)

// buildVariant is the single build configuration a snapshot is indexed under.
type buildVariant struct {
	GOOS       string
	GOARCH     string
	CGOEnabled string
	GoVersion  string
	GoFlags    string
	BuildTags  []string
	GoWorkOff  bool
}

// goEnvironment is what the Go toolchain that type-checks a module reports for its directory.
type goEnvironment struct {
	Variant  buildVariant
	WorkFile string
}

func normalizeBuildTags(tags []string) ([]string, error) {
	for _, tag := range tags {
		if err := buildconstraint.ValidateTag(tag); err != nil {
			return nil, fmt.Errorf("--tags: %w", err)
		}
	}
	tags = slices.Clone(tags)
	slices.Sort(tags)
	return slices.Compact(tags), nil
}

// manifestFile is a dependency manifest hashed into the content set but never stored as a source.
type manifestFile struct {
	Key         string
	ContentHash string
	ModifiedAt  time.Time
}

func readGoEnvironment(ctx context.Context, directory string) (goEnvironment, error) {
	return readGoEnvironmentWithWork(ctx, directory, false)
}

func readGoEnvironmentWithWork(ctx context.Context, directory string, workOff bool) (goEnvironment, error) {
	command := exec.CommandContext(ctx, "go", "env", "-json", "GOOS", "GOARCH", "CGO_ENABLED", "GOVERSION", "GOWORK", "GOFLAGS")
	command.Dir = directory
	if workOff {
		command.Env = append(os.Environ(), "GOWORK=off")
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return goEnvironment{}, fmt.Errorf("read go env in %q: %w: %s", directory, err, strings.TrimSpace(stderr.String()))
	}
	var values map[string]string
	if err := json.Unmarshal(output, &values); err != nil {
		return goEnvironment{}, fmt.Errorf("read go env in %q: decode %q: %w", directory, output, err)
	}
	for _, name := range []string{"GOOS", "GOARCH", "CGO_ENABLED", "GOVERSION"} {
		if values[name] == "" {
			return goEnvironment{}, fmt.Errorf("read go env in %q: %s is empty", directory, name)
		}
	}
	environment := goEnvironment{Variant: buildVariant{
		GOOS: values["GOOS"], GOARCH: values["GOARCH"], CGOEnabled: values["CGO_ENABLED"], GoVersion: values["GOVERSION"], GoFlags: values["GOFLAGS"],
	}}
	workFile := values["GOWORK"]
	if workFile == "" || workFile == "off" {
		return environment, nil
	}
	used, err := workspaceUsesDirectory(workFile, directory)
	if err != nil {
		return goEnvironment{}, fmt.Errorf("read go env in %q: %w", directory, err)
	}
	if !used {
		// The toolchain refuses to load a module its enclosing go.work does not use, so it builds alone.
		environment, err := readGoEnvironmentWithWork(ctx, directory, true)
		environment.Variant.GoWorkOff = true
		return environment, err
	}
	environment.WorkFile = workFile
	return environment, nil
}

// workspaceUsesDirectory reports whether a use of the go.work at workFile names the module at directory.
func workspaceUsesDirectory(workFile, directory string) (bool, error) {
	workspace, _, err := readWorkspace(workFile)
	if err != nil {
		return false, err
	}
	module, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return false, fmt.Errorf("canonicalize module directory %q: %w", directory, err)
	}
	for _, use := range workspace.Use {
		used, err := filepath.EvalSymlinks(rootedPath(filepath.Dir(workFile), use.Path))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("canonicalize go.work use %q: %w", use.Path, err)
		}
		if used == module {
			return true, nil
		}
	}
	return false, nil
}

// readManifests hashes the module's go.mod and go.sum under their module-relative paths and the
// enclosing workspace's go.work and go.work.sum, wherever they sit, under the fixed keys go.work
// and go.work.sum. go.mod and the go.work the toolchain reported must exist; the sums are optional.
func readManifests(moduleDirectory, workFile string) ([]manifestFile, error) {
	type candidate struct {
		path, key string
		required  bool
	}
	candidates := []candidate{
		{path: filepath.Join(moduleDirectory, "go.mod"), key: "go.mod", required: true},
		{path: filepath.Join(moduleDirectory, "go.sum"), key: "go.sum"},
	}
	if workFile != "" {
		candidates = append(candidates, candidate{path: workFile, key: "go.work", required: true}, candidate{path: workFile + ".sum", key: "go.work.sum"})
	}
	manifests := make([]manifestFile, 0, len(candidates))
	for _, manifest := range candidates {
		content, err := os.ReadFile(manifest.path)
		if !manifest.required && errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read dependency manifest %q: %w", manifest.path, err)
		}
		info, err := os.Stat(manifest.path)
		if err != nil {
			return nil, fmt.Errorf("stat dependency manifest %q: %w", manifest.path, err)
		}
		manifests = append(manifests, manifestFile{Key: manifest.key, ContentHash: hashBytes(content), ModifiedAt: info.ModTime().UTC()})
	}
	return manifests, nil
}
