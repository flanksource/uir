//go:build hack

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func replay(ctx context.Context, manifest *runManifest, corpus corpus, path string) error {
	group := filepath.Join(manifest.Directory, corpus.Name+"-"+path)
	if err := os.Mkdir(group, 0o755); err != nil {
		return err
	}
	checkout := filepath.Join(group, "checkout")
	if _, err := commandOutput(ctx, "git", "clone", "--no-hardlinks", "--no-checkout", corpus.Source, checkout); err != nil {
		return err
	}
	checkout, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		return err
	}
	for _, entry := range replayCases(corpus, path) {
		entry.Checkout, entry.Database, entry.OIPA = checkout, filepath.Join(group, "uir.db"), manifest.OIPA
		entry.Directory = filepath.Join(group, entry.ID)
		if err := os.Mkdir(entry.Directory, 0o755); err != nil {
			return err
		}
		started := time.Now()
		checkoutCommit := entry.Commit
		if path == "historical" {
			checkoutCommit = corpus.Tip
		}
		if _, err := commandOutput(ctx, "git", "-C", checkout, "checkout", "--detach", checkoutCommit); err != nil {
			return err
		}
		entry.CheckoutMS = float64(time.Since(started).Microseconds()) / 1000
		manifest.Cases = append(manifest.Cases, entry)
		if err := writeJSON(filepath.Join(manifest.Directory, "manifest.json"), manifest); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s %s %s %.12s\n", corpus.Name, path, entry.Kind, entry.Commit)
		if err := runFixture(ctx, manifest, entry); err != nil {
			return err
		}
	}
	status, err := commandOutput(ctx, "git", "-C", corpus.Source, "status", "--porcelain=v1")
	if err != nil {
		return err
	}
	if status != corpus.OriginalStatus {
		fmt.Fprintf(os.Stderr, "Original %s working tree changed concurrently; the replay used only pinned committed sources.\n", corpus.Name)
	}
	for i := range manifest.Corpora {
		if manifest.Corpora[i].Name == corpus.Name {
			manifest.Corpora[i].OriginalStatusAfter = status
		}
	}
	return writeJSON(filepath.Join(manifest.Directory, "manifest.json"), manifest)
}

func replayCases(corpus corpus, path string) []measurement {
	base := measurement{Corpus: corpus.Name, Producer: corpus.Producer, Path: path, RootKey: corpus.RootKey}
	var entries []measurement
	add := func(kind, commit string) {
		entry := base
		entry.ID, entry.Kind, entry.Commit = fmt.Sprintf("%02d-%s", len(entries), kind), kind, commit
		entries = append(entries, entry)
	}
	if path == "historical" {
		add("initial", corpus.Tip)
		for _, commit := range corpus.Commits {
			add("revision", commit)
		}
		add("noop", corpus.Tip)
		add("repeat", corpus.Commits[len(corpus.Commits)-1])
	} else {
		for i, commit := range corpus.Commits {
			kind := "transition"
			if i == 0 {
				kind = "initial"
			}
			add(kind, commit)
		}
		add("noop", corpus.Tip)
	}
	return entries
}

func runFixture(ctx context.Context, manifest *runManifest, entry measurement) (returnErr error) {
	descriptor := filepath.Join(entry.Directory, "case.json")
	if err := writeJSON(descriptor, entry); err != nil {
		return err
	}
	fixture := filepath.Join(entry.Directory, "case.md")
	body, err := os.ReadFile(manifest.Fixture)
	if err != nil {
		return err
	}
	if err := os.WriteFile(fixture, body, 0o644); err != nil {
		return err
	}
	log, err := os.Create(filepath.Join(entry.Directory, "gavel.log"))
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, log.Close()) }()
	command := exec.CommandContext(ctx, manifest.Gavel, "fixtures", fixture, "--benchmark", "--profile", "--max-concurrent", "1", "--max-retries", "0", "--no-progress", "--no-color", "--format", "json="+filepath.Join(entry.Directory, "gavel.json"))
	command.Dir, command.Stdout, command.Stderr = manifest.Directory, log, log
	command.Env = append(os.Environ(), "GOWORK=off", "SNAPSHOT_RUNNER="+manifest.Runner, "SNAPSHOT_CASE="+descriptor)
	previous, err := benchmarkFiles(manifest.Directory)
	if err != nil {
		return err
	}
	runErr := command.Run()
	artifactErr := captureBenchmark(manifest.Directory, entry.Directory, previous)
	if runErr != nil {
		runErr = fmt.Errorf("%s failed: %w; inspect %s", entry.ID, runErr, filepath.Join(entry.Directory, "gavel.log"))
	}
	return errors.Join(runErr, artifactErr)
}
