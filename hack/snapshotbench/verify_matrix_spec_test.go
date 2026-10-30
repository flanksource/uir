//go:build hack

package main

import (
	"fmt"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("snapshot replay evidence matrix", func() {
	newManifest := func() runManifest {
		manifest := runManifest{Transitions: 20}
		for _, name := range []string{"go", "rules"} {
			corpus := corpus{Name: name, Producer: name + "-producer", RootKey: name + "-root"}
			for i := range 21 {
				corpus.Commits = append(corpus.Commits, fmt.Sprintf("%040x", i+1))
			}
			corpus.Tip = corpus.Commits[20]
			manifest.Corpora = append(manifest.Corpora, corpus)
			for _, path := range []string{"head", "historical"} {
				manifest.Cases = append(manifest.Cases, replayCases(corpus, path)...)
			}
		}
		return manifest
	}

	It("accepts both complete chronological replays", func() {
		Expect(validateReplayMatrix(newManifest())).To(Succeed())
	})

	It("rejects a replay whose final analysis reports are missing", func() {
		directory := GinkgoT().TempDir()
		Expect(writeJSON(filepath.Join(directory, "manifest.json"), newManifest())).To(Succeed())
		Expect(verifyRun(directory)).To(MatchError(ContainSubstring("missing replay artifact")))
	})

	DescribeTable("rejects a complete-sized matrix with missing or mismatched evidence", func(mutate func(*runManifest), message string) {
		manifest := newManifest()
		mutate(&manifest)
		Expect(validateReplayMatrix(manifest)).To(MatchError(ContainSubstring(message)))
	},
		Entry("duplicate observation", func(m *runManifest) { m.Cases[1] = m.Cases[0] }, "observation 1"),
		Entry("wrong corpus", func(m *runManifest) { m.Corpora[1].Name = "go" }, "corpora"),
		Entry("wrong commit", func(m *runManifest) { m.Cases[1].Commit = m.Cases[0].Commit }, "observation 1"),
		Entry("wrong producer", func(m *runManifest) { m.Cases[1].Producer = "other" }, "observation 1"),
		Entry("missing commit", func(m *runManifest) { m.Corpora[0].Commits = m.Corpora[0].Commits[:20] }, "21 commits"),
		Entry("wrong tip", func(m *runManifest) { m.Corpora[0].Tip = m.Corpora[0].Commits[0] }, "pinned tip"),
	)
})
