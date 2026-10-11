package main

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("snapshot replay evidence reuse", func() {
	var previous runManifest
	var directory string
	BeforeEach(func() {
		directory = GinkgoT().TempDir()
		fixture := corpus{Name: "go", Producer: "go", Source: "source", Tip: "second", RootKey: "root", Commits: []string{"first", "second"}}
		previous = runManifest{Directory: directory, Transitions: 1, Corpora: []corpus{fixture}, Fingerprints: map[string]string{"runner": "old-runner"}}
		for _, path := range []string{"head", "historical"} {
			previous.Cases = append(previous.Cases, replayCases(fixture, path)...)
		}
		Expect(writeJSON(filepath.Join(directory, "manifest.json"), previous)).To(Succeed())
	})

	It("retains original case descriptors and runner provenance for an identical pinned Go replay", func() {
		current := runManifest{Transitions: 1, Corpora: previous.Corpora, Fingerprints: map[string]string{}}
		Expect(reuseGoReplay(&current, directory)).To(Succeed())
		Expect(current.Cases).To(Equal(previous.Cases))
		Expect(current.ReusedGoRun).To(Equal(directory))
		Expect(current.Fingerprints["go-runner"]).To(Equal("old-runner"))
	})

	It("rejects a changed pinned commit sequence", func() {
		current := runManifest{Transitions: 1, Corpora: []corpus{previous.Corpora[0]}, Fingerprints: map[string]string{}}
		current.Corpora[0].Commits = []string{"other", "second"}
		Expect(reuseGoReplay(&current, directory)).To(MatchError(ContainSubstring("pinned Go corpus")))
	})

	It("rejects incomplete or reordered original Go observations", func() {
		previous.Cases[1] = previous.Cases[0]
		Expect(writeJSON(filepath.Join(directory, "manifest.json"), previous)).To(Succeed())
		current := runManifest{Transitions: 1, Corpora: previous.Corpora, Fingerprints: map[string]string{}}
		Expect(reuseGoReplay(&current, directory)).To(MatchError(ContainSubstring("Go observation 1")))
	})

	DescribeTable("rejects nonportable source identities", func(path string) {
		Expect(validateSourcePath(path)).To(HaveOccurred())
	}, Entry("archive path", ".tmp/uir-revisions/revision-123/rules/example.xml"), Entry("absolute", "/checkout/rules/example.xml"), Entry("outside root", "../rules/example.xml"))

	It("accepts repository-relative and generated source paths", func() {
		for _, path := range []string{"rules/pc/example/XMLData.xml", "pc/example/data.xml", "manifest.json", "generation"} {
			Expect(validateSourcePath(path)).To(Succeed())
		}
	})
})
