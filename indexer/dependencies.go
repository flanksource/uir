package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flanksource/uir/storage"
	"golang.org/x/mod/modfile"
)

type selectedModule struct {
	Path    string          `json:"Path"`
	Version string          `json:"Version"`
	Dir     string          `json:"Dir"`
	Main    bool            `json:"Main"`
	Replace *selectedModule `json:"Replace"`
}

type dependencyObservation struct {
	Edge             storage.SnapshotDependency
	LocalDir         string
	TargetModulePath string
	TargetVersion    string
}

func readDependencies(ctx context.Context, root discoveredRoot) ([]dependencyObservation, error) {
	path := filepath.Join(root.LocalPath, "go.mod")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dependency manifest %q: %w", path, err)
	}
	manifest, err := modfile.Parse(path, content, nil)
	if err != nil {
		return nil, fmt.Errorf("parse dependency manifest %q: %w", path, err)
	}
	var workspace *modfile.WorkFile
	if root.WorkFile != "" {
		content, err := os.ReadFile(root.WorkFile)
		if err != nil {
			return nil, fmt.Errorf("read workspace %q: %w", root.WorkFile, err)
		}
		workspace, err = modfile.ParseWork(root.WorkFile, content, nil)
		if err != nil {
			return nil, fmt.Errorf("parse workspace %q: %w", root.WorkFile, err)
		}
	}
	selected, selectionError := selectedModules(ctx, root)
	dependencies := make([]dependencyObservation, 0, len(manifest.Require))
	for _, requirement := range manifest.Require {
		edge := storage.SnapshotDependency{
			ModulePath: requirement.Mod.Path, DeclaredVersion: requirement.Mod.Version, Indirect: requirement.Indirect,
		}
		if replacement := applicableReplace(manifest.Replace, requirement.Mod.Path, requirement.Mod.Version); replacement != nil {
			edge.ReplacePath, edge.ReplaceVersion = replacement.New.Path, replacement.New.Version
		}
		if workspace != nil {
			if replacement := applicableReplace(workspace.Replace, requirement.Mod.Path, requirement.Mod.Version); replacement != nil {
				edge.ReplacePath, edge.ReplaceVersion = replacement.New.Path, replacement.New.Version
			}
		}
		observation := dependencyObservation{Edge: edge}
		if module, found := selected[edge.ModulePath]; found {
			observation.Edge.SelectedVersion = module.Version
			effective := module
			if module.Replace != nil {
				effective = *module.Replace
			}
			observation.TargetModulePath, observation.TargetVersion = effective.Path, effective.Version
			if effective.Main || module.Replace != nil && effective.Version == "" {
				observation.LocalDir = effective.Dir
			}
			observation.Edge.UnresolvedReason = "snapshot for selected module is not indexed"
		} else if selectionError != "" {
			observation.Edge.UnresolvedReason = selectionError
		} else {
			observation.Edge.UnresolvedReason = "module is absent from the selected Go build list"
		}
		if local, localErr := localDependency(root, manifest, workspace, edge.ModulePath, edge.DeclaredVersion); localErr != nil {
			return nil, localErr
		} else if local != "" {
			observation.LocalDir = local
			observation.Edge.UnresolvedReason = "local dependency has not been indexed"
		}
		dependencies = append(dependencies, observation)
	}
	sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].Edge.ModulePath < dependencies[j].Edge.ModulePath })
	return dependencies, nil
}

func localDependency(root discoveredRoot, manifest *modfile.File, workspace *modfile.WorkFile, path, version string) (string, error) {
	if workspace != nil {
		if replacement := applicableReplace(workspace.Replace, path, version); replacement != nil && replacement.New.Version == "" {
			return rootedPath(filepath.Dir(root.WorkFile), replacement.New.Path), nil
		}
		for _, use := range workspace.Use {
			directory := rootedPath(filepath.Dir(root.WorkFile), use.Path)
			content, err := os.ReadFile(filepath.Join(directory, "go.mod"))
			if err != nil {
				return "", fmt.Errorf("read workspace module %q: %w", directory, err)
			}
			if modfile.ModulePath(content) == path {
				return directory, nil
			}
		}
	}
	if !root.Historical {
		if replacement := applicableReplace(manifest.Replace, path, version); replacement != nil && replacement.New.Version == "" {
			return rootedPath(root.LocalPath, replacement.New.Path), nil
		}
	}
	return "", nil
}

func rootedPath(base, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}

func applicableReplace(replacements []*modfile.Replace, path, version string) *modfile.Replace {
	var wildcard *modfile.Replace
	for _, replacement := range replacements {
		if replacement.Old.Path != path {
			continue
		}
		if replacement.Old.Version == version {
			return replacement
		}
		if replacement.Old.Version == "" {
			wildcard = replacement
		}
	}
	return wildcard
}

func selectedModules(ctx context.Context, root discoveredRoot) (map[string]selectedModule, string) {
	args := []string{"list", "-mod=readonly"}
	if root.ModFile != "" {
		args = append(args, "-modfile="+root.ModFile)
	}
	args = append(args, "-m", "-json", "all")
	command := exec.CommandContext(ctx, "go", args...)
	command.Dir = root.LocalPath
	if root.Variant.GoWorkOff {
		command.Env = append(os.Environ(), "GOWORK=off")
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	selected := map[string]selectedModule{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var module selectedModule
		if decodeErr := decoder.Decode(&module); decodeErr != nil {
			break
		}
		selected[module.Path] = module
	}
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return selected, "Go module selection failed: " + message
	}
	return selected, ""
}

func dependencyHash(dependencies []dependencyObservation) string {
	digest := newCanonicalHash("module-dependencies-v1")
	digest.count(len(dependencies))
	for _, observation := range dependencies {
		edge := observation.Edge
		digest.text(edge.ModulePath)
		digest.text(edge.DeclaredVersion)
		digest.text(fmt.Sprint(edge.Indirect))
		digest.text(edge.ReplacePath)
		digest.text(edge.ReplaceVersion)
		digest.text(edge.SelectedVersion)
		digest.text(observation.LocalDir)
		digest.text(observation.TargetModulePath)
		digest.text(observation.TargetVersion)
		if edge.TargetSnapshotID != nil {
			digest.text(edge.TargetSnapshotID.String())
		}
		digest.text(edge.UnresolvedReason)
	}
	return digest.sum()
}
