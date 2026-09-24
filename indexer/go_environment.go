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
	"strings"
)

// buildVariant is the single build configuration a snapshot is indexed under. BuildTags is empty
// until tags become selectable; it is hashed as a list so adding tags keeps the encoding.
type buildVariant struct {
	GOOS       string
	GOARCH     string
	CGOEnabled string
	GoVersion  string
	BuildTags  []string
}

// goEnvironment is what the Go toolchain that type-checks a module reports for its directory.
type goEnvironment struct {
	Variant  buildVariant
	WorkFile string
}

// manifestFile is a dependency manifest hashed into the content set but never stored as a source.
type manifestFile struct {
	Key         string
	ContentHash string
}

func readGoEnvironment(ctx context.Context, directory string) (goEnvironment, error) {
	command := exec.CommandContext(ctx, "go", "env", "-json", "GOOS", "GOARCH", "CGO_ENABLED", "GOVERSION", "GOWORK")
	command.Dir = directory
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
		GOOS: values["GOOS"], GOARCH: values["GOARCH"], CGOEnabled: values["CGO_ENABLED"], GoVersion: values["GOVERSION"],
	}}
	if workFile := values["GOWORK"]; workFile != "" && workFile != "off" {
		environment.WorkFile = workFile
	}
	return environment, nil
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
		manifests = append(manifests, manifestFile{Key: manifest.key, ContentHash: hashBytes(content)})
	}
	return manifests, nil
}
