package schemagen

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSourceFacts(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Schema Source Facts Suite")
}

var _ = Describe("schema source facts", func() {
	otherPlatform := "windows"
	if runtime.GOOS == otherPlatform {
		otherPlatform = "linux"
	}

	DescribeTable("collects documentation and enums only from buildable non-test files", func(name, excluded string) {
		dir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "model.go"), []byte(`package uir

// Kind identifies the node category.
type Kind string

const Active Kind = "active"

// Node describes a declaration.
type Node struct {
	// Name is the declaration name.
	Name string
}
`), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, name), []byte(excluded), 0o600)).To(Succeed())

		facts, err := loadSource(dir, "example.org/model")
		Expect(err).NotTo(HaveOccurred())
		Expect(facts).To(Equal(&sourceFacts{
			typeDocs: map[string]string{
				"Kind": "Kind identifies the node category.",
				"Node": "Node describes a declaration.",
			},
			fieldDocs: map[string]map[string]string{"Node": {"Name": "Name is the declaration name."}},
			enums:     map[string][]string{"Kind": {"active"}},
		}))
	},
		Entry("build constraint", "ignored.go", `//go:build ignore

package uir

// Excluded must not be documented.
type Excluded struct{}

const Ignored Kind = "ignored"
`),
		Entry("platform suffix", "model_"+otherPlatform+".go", `package uir

// Excluded must not be documented.
type Excluded struct{}

const Platform Kind = "platform"
`),
		Entry("test file", "model_test.go", `package uir

// Excluded must not be documented.
type Excluded struct{}

const TestOnly Kind = "test"
`),
	)
})
