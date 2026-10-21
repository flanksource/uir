//go:build hack

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func run(ctx context.Context, args []string) (returnErr error) {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	out := flags.String("out", "", "new run directory under .tmp/snapshot-benchmark")
	oipa := flags.String("oipa", "/Users/moshe/work/om/oma/oipa-cli/.bin/oipa-cli", "make-built OIPA executable")
	selectedCorpus := flags.String("corpus", "all", "all, go, or rules")
	selectedPath := flags.String("path", "all", "all, head, or historical")
	transitions := flags.Int("transitions", 20, "first-parent transitions; 20 for the full profile")
	reuseGo := flags.String("reuse-go", "", "retain the complete Go observations and provenance from an identical pinned run")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *transitions < 1 || *transitions > 20 || !slices.Contains([]string{"all", "go", "rules"}, *selectedCorpus) || !slices.Contains([]string{"all", "head", "historical"}, *selectedPath) {
		return errors.New("invalid corpus/path/transitions")
	}
	if *reuseGo != "" && (*selectedCorpus != "all" || *selectedPath != "all") {
		return errors.New("reuse-go requires the complete corpus and path selection")
	}
	manifest, err := prepareRun(ctx, *out, *oipa, *transitions)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, saveReport(manifest)) }()
	if *reuseGo != "" {
		if err := reuseGoReplay(&manifest, *reuseGo); err != nil {
			return err
		}
	}
	for _, corpus := range manifest.Corpora {
		if corpus.Name == "go" && manifest.ReusedGoRun != "" {
			continue
		}
		if *selectedCorpus != "all" && *selectedCorpus != corpus.Name {
			continue
		}
		for _, path := range []string{"head", "historical"} {
			if *selectedPath != "all" && *selectedPath != path {
				continue
			}
			if err := replay(ctx, &manifest, corpus, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func prepareRun(ctx context.Context, output, oipa string, transitions int) (runManifest, error) {
	working, err := os.Getwd()
	if err != nil {
		return runManifest{}, err
	}
	if output == "" {
		output = filepath.Join(working, ".tmp", "snapshot-benchmark", time.Now().Format("20060102-150405"))
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return runManifest{}, err
	}
	relative, err := filepath.Rel(filepath.Join(working, ".tmp", "snapshot-benchmark"), output)
	if err != nil || !filepath.IsLocal(relative) || relative == "." {
		return runManifest{}, errors.New("run output must be a new directory beneath .tmp/snapshot-benchmark")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return runManifest{}, err
	}
	if err := os.Mkdir(output, 0o755); err != nil {
		return runManifest{}, err
	}
	manifest := runManifest{Version: 1, StartedAt: time.Now().UTC(), Directory: output, WorkingDir: working, Transitions: transitions, Fingerprints: map[string]string{}}
	if err := prepareTools(ctx, &manifest, oipa); err != nil {
		return manifest, err
	}
	manifest.Corpora = []corpus{
		{Name: "go", Producer: "go", Source: "/Users/moshe/work/om/oma/oipa-cli", Tip: "2c34bc95bc95e4a75fc26cec4ea8b0e70aba6362", RootKey: "github.com/oipa/cli"},
		{Name: "rules", Producer: "oipa", Source: "/Users/moshe/work/om/oma/envs/rules-dev-baseline", Tip: "0d8fd63cf62dcc3b0bb9bb5e9ef065d74b783932"},
	}
	for i := range manifest.Corpora {
		corpus := &manifest.Corpora[i]
		commits, err := commandOutput(ctx, "git", "-C", corpus.Source, "log", "--first-parent", "--format=%H", fmt.Sprintf("-%d", transitions+1), corpus.Tip)
		if err != nil {
			return manifest, err
		}
		corpus.Commits = strings.Fields(commits)
		if len(corpus.Commits) != transitions+1 {
			return manifest, fmt.Errorf("%s: need %d commits, got %d", corpus.Name, transitions+1, len(corpus.Commits))
		}
		slices.Reverse(corpus.Commits)
		corpus.OriginalStatus, err = commandOutput(ctx, "git", "-C", corpus.Source, "status", "--porcelain=v1")
		if err != nil {
			return manifest, err
		}
	}
	return manifest, writeJSON(filepath.Join(output, "manifest.json"), manifest)
}

func prepareTools(ctx context.Context, manifest *runManifest, oipa string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	manifest.Runner = filepath.Join(manifest.Directory, "runner")
	if err := copyExecutable(executable, manifest.Runner); err != nil {
		return err
	}
	oipa, err = filepath.Abs(oipa)
	if err != nil {
		return err
	}
	gavel, err := exec.LookPath("gavel")
	if err != nil {
		return err
	}
	manifest.OIPA, manifest.Gavel = filepath.Join(manifest.Directory, "oipa-cli"), filepath.Join(manifest.Directory, "gavel")
	if err := copyExecutable(oipa, manifest.OIPA); err != nil {
		return err
	}
	if err := copyExecutable(gavel, manifest.Gavel); err != nil {
		return err
	}
	manifest.Fixture = filepath.Join(manifest.Directory, "observation.md")
	fixture, err := os.ReadFile(filepath.Join(manifest.WorkingDir, "fixtures", "bench", "snapshot-incremental.md"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(manifest.Fixture, fixture, 0o644); err != nil {
		return err
	}
	for name, path := range map[string]string{"runner": manifest.Runner, "oipa": manifest.OIPA, "gavel": manifest.Gavel} {
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%s binary: %w", name, err)
		}
		digest := sha256.Sum256(body)
		manifest.Fingerprints[name] = hex.EncodeToString(digest[:])
	}
	manifest.GoVersion, err = commandOutput(ctx, "go", "version")
	if err != nil {
		return err
	}
	manifest.Cache, err = commandOutput(ctx, "go", "env", "GOCACHE", "GOMODCACHE", "GOWORK")
	if err != nil {
		return err
	}
	return captureBuildInputs(ctx, manifest, oipa)
}

func commandOutput(ctx context.Context, executable string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, executable, args...)
	body, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %v: %w: %s", executable, args, err, body)
	}
	return strings.TrimSpace(string(body)), nil
}

func copyExecutable(source, destination string) (returnErr error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, input.Close()) }()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, output.Close()) }()
	_, err = io.Copy(output, input)
	return err
}
