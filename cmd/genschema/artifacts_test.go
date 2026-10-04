package main

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestArtifacts(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Schema artifacts")
}

var _ = Describe("Schema artifact updates", func() {
	var output artifact
	BeforeEach(func() {
		output = artifact{Path: filepath.Join(GinkgoT().TempDir(), "schema.json"), Data: []byte("{\"type\":\"object\"}\n")}
	})

	It("writes the generated artifact", func() {
		Expect(updateArtifacts([]artifact{output}, artifactOptions{})).To(Succeed())
		actual, err := os.ReadFile(output.Path)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(output.Data))
	})

	It("accepts an unchanged artifact in check mode", func() {
		Expect(os.WriteFile(output.Path, output.Data, 0o600)).To(Succeed())
		Expect(updateArtifacts([]artifact{output}, artifactOptions{Check: true})).To(Succeed())
		info, err := os.Stat(output.Path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
	})

	It("reports drift without overwriting stale bytes", func() {
		stale := []byte("{\"type\":\"array\"}\n")
		Expect(os.WriteFile(output.Path, stale, 0o600)).To(Succeed())
		Expect(updateArtifacts([]artifact{output}, artifactOptions{Check: true})).To(MatchError(ContainSubstring(output.Path + " is stale")))
		actual, err := os.ReadFile(output.Path)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(stale))
	})

	It("reports a missing artifact without creating it", func() {
		Expect(updateArtifacts([]artifact{output}, artifactOptions{Check: true})).To(MatchError(ContainSubstring(output.Path)))
		_, err := os.Stat(output.Path)
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	It("reports an unwritable destination", func() {
		output.Path = filepath.Join(output.Path, "missing", "schema.json")
		Expect(updateArtifacts([]artifact{output}, artifactOptions{})).To(MatchError(ContainSubstring(output.Path)))
	})
})
