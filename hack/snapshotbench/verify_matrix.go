//go:build hack

package main

import "fmt"

func validateReplayMatrix(manifest runManifest) error {
	if manifest.Transitions != 20 || len(manifest.Corpora) != 2 || len(manifest.Cases) != 92 {
		return fmt.Errorf("full replay requires two corpora, 20 transitions and 92 observations; got %d/%d/%d", len(manifest.Corpora), manifest.Transitions, len(manifest.Cases))
	}
	if manifest.Corpora[0].Name != "go" || manifest.Corpora[1].Name != "rules" {
		return fmt.Errorf("replay corpora must be go and rules, got %q and %q", manifest.Corpora[0].Name, manifest.Corpora[1].Name)
	}
	var expected []measurement
	for _, corpus := range manifest.Corpora {
		if len(corpus.Commits) != 21 {
			return fmt.Errorf("%s replay requires 21 commits, got %d", corpus.Name, len(corpus.Commits))
		}
		if corpus.Commits[20] != corpus.Tip {
			return fmt.Errorf("%s final commit differs from pinned tip", corpus.Name)
		}
		seen := map[string]bool{}
		for _, commit := range corpus.Commits {
			if commit == "" || seen[commit] {
				return fmt.Errorf("%s replay has empty or duplicate commit %q", corpus.Name, commit)
			}
			seen[commit] = true
		}
		for _, path := range []string{"head", "historical"} {
			expected = append(expected, replayCases(corpus, path)...)
		}
	}
	for i, want := range expected {
		got := manifest.Cases[i]
		if got.ID != want.ID || got.Corpus != want.Corpus || got.Producer != want.Producer || got.Path != want.Path || got.Kind != want.Kind || got.Commit != want.Commit || got.RootKey != want.RootKey {
			return fmt.Errorf("observation %d: expected %s/%s/%s at %s, got %s/%s/%s at %s", i, want.Corpus, want.Path, want.ID, want.Commit, got.Corpus, got.Path, got.ID, got.Commit)
		}
	}
	return nil
}
