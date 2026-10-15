package indexer

import (
	"github.com/flanksource/uir"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("empty external catalogue schema", func() {
	It("retains an empty schema document so a later capture can delete its last table", func() {
		publication, err := PublicationFromNodes(NodePublication{
			RootKey: "database/example", Name: "Example", Location: ExternalLocation{URI: "database://example"}, Revision: "capture-2",
			Documents: []NodeDocument{{PathKey: "public/catalog.sql", PackagePath: "public", Nodes: []uir.Node{}}},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(publication.Documents).To(HaveLen(1))
		Expect(publication.Documents[0].Content.Symbols).To(BeEmpty())
	})
})
