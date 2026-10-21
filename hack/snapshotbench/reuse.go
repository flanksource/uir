//go:build hack

package main

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

func reuseGoReplay(manifest *runManifest, directory string) error {
	var previous runManifest
	if err := readJSON(filepath.Join(directory, "manifest.json"), &previous); err != nil {
		return err
	}
	if len(previous.Corpora) == 0 || len(manifest.Corpora) == 0 {
		return fmt.Errorf("reused run is missing its pinned Go corpus")
	}
	want, got := manifest.Corpora[0], previous.Corpora[0]
	if want.Name != "go" || got.Name != "go" || want.Producer != got.Producer || want.Source != got.Source || want.Tip != got.Tip || want.RootKey != got.RootKey || !reflect.DeepEqual(want.Commits, got.Commits) || manifest.Transitions != previous.Transitions || manifest.GoVersion != previous.GoVersion || manifest.Cache != previous.Cache {
		return fmt.Errorf("reused run differs from the pinned Go corpus or toolchain/cache settings")
	}
	var expected, entries []measurement
	for _, path := range []string{"head", "historical"} {
		expected = append(expected, replayCases(want, path)...)
	}
	for _, entry := range previous.Cases {
		if entry.Corpus == "go" {
			entries = append(entries, entry)
		}
	}
	if len(entries) != len(expected) {
		return fmt.Errorf("reused Go replay requires %d observations, got %d", len(expected), len(entries))
	}
	for i, want := range expected {
		got := entries[i]
		want.Checkout, want.Database, want.Directory, want.OIPA, want.CheckoutMS = got.Checkout, got.Database, got.Directory, got.OIPA, got.CheckoutMS
		if want != got {
			return fmt.Errorf("reused Go observation %d differs from the pinned sequence", i)
		}
	}
	if previous.Fingerprints["runner"] == "" {
		return fmt.Errorf("reused Go replay has no runner fingerprint")
	}
	manifest.ReusedGoRun = previous.Directory
	manifest.Fingerprints["go-runner"] = previous.Fingerprints["runner"]
	manifest.Fingerprints["go-gavel"] = previous.Fingerprints["gavel"]
	manifest.Cases = append(manifest.Cases, entries...)
	return nil
}

func validateSourcePath(path string) error {
	if !filepath.IsLocal(path) || strings.HasPrefix(filepath.ToSlash(path), ".tmp/") {
		return fmt.Errorf("nonportable source identity %q", path)
	}
	return nil
}
