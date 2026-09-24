package indexer

import (
	"encoding/json"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Go AST extraction", func() {
	It("uses a lossless canonical identity key", func() {
		left := uir.Identifier{Package: "example.org/a.b", Type: "C", Method: "Run", NodeType: uir.NodeTypeMethod}
		right := uir.Identifier{Module: "example", Package: "org/a", Type: "b.C", Method: "Run", NodeType: uir.NodeTypeMethod}

		Expect(left.SymbolKey()).To(Equal(right.SymbolKey()))
		Expect(left.IdentityKey()).ToNot(Equal(right.IdentityKey()))
		Expect(left.IdentityKey()).To(Equal(`v1:["method","","example.org/a.b","C","Run","",""]`))
	})

	It("extracts declarations, struct fields, and call targets in source order", func() {
		content := []byte(`package worker

import "example.org/acme/helper"

type Service struct {
	Name string
}

func (s *Service) Run(value string) error {
	helper.Notify(value)
	s.Stop()
	return nil
}

func (s *Service) Stop() {}

func Build() *Service {
	return &Service{}
}
`)
		indexed, err := extractGoFile("worker/service.go", "example.org/acme/worker", content)
		Expect(err).ToNot(HaveOccurred())
		Expect(indexed.PackageName).To(Equal("worker"))
		Expect(indexed.PackagePath).To(Equal("example.org/acme/worker"))

		identities := make([]uir.Identifier, len(indexed.Nodes))
		for i := range indexed.Nodes {
			identities[i] = indexed.Nodes[i].Identifier
		}
		Expect(identities).To(Equal([]uir.Identifier{
			{Package: "example.org/acme/worker", Type: "Service", NodeType: uir.NodeTypeType},
			{Package: "example.org/acme/worker", Type: "Service", Field: "Name", NodeType: uir.NodeTypeField},
			{Package: "example.org/acme/worker", Type: "Service", Method: "Run", Signature: "(value string) error", NodeType: uir.NodeTypeMethod},
			{Package: "example.org/acme/worker", Type: "Service", Method: "Stop", Signature: "()", NodeType: uir.NodeTypeMethod},
			{Package: "example.org/acme/worker", Method: "Build", Signature: "() *Service", NodeType: uir.NodeTypeMethod},
		}))
		Expect(indexed.Nodes[1].ParentIdentity).To(Equal(indexed.Nodes[0].Identifier.IdentityKey()))
		Expect(indexed.Nodes[2].ParentIdentity).To(Equal(indexed.Nodes[0].Identifier.IdentityKey()))
		Expect(indexed.Calls).To(HaveLen(2))
		Expect(indexed.Calls[0].FromIdentity).To(Equal(indexed.Nodes[2].Identifier.IdentityKey()))
		Expect(indexed.Calls[0].ToIdentifier).To(Equal(uir.Identifier{
			Package: "example.org/acme/helper", Method: "Notify", NodeType: uir.NodeTypeMethod,
		}))
		Expect(indexed.Calls[0].Resolvable).To(BeTrue())
		Expect(indexed.Calls[1].ToIdentifier).To(Equal(uir.Identifier{
			Package: "example.org/acme/worker", Method: "Stop", NodeType: uir.NodeTypeMethod,
		}))
		Expect(indexed.Calls[1].Resolvable).To(BeFalse())
		Expect(indexed.Calls[0].Text).To(Equal("helper.Notify"))
		Expect(indexed.syntaxDocument().Occurrences[0].Range).To(Equal(storage.Range{10, 2, 10, 15}))
		Expect(json.Valid(indexed.Nodes[0].Payload)).To(BeTrue())
	})
})
