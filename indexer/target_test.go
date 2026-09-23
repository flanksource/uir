package indexer

import (
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Call target lookup", func() {
	It("preserves exact, root-scoped, and ambiguous resolution", func() {
		identifier := uir.Identifier{Package: "example.org/work", Method: "Run", Signature: "()", NodeType: uir.NodeTypeMethod}
		app := persistedTarget{RootKey: "app", Node: storage.Node{ID: uuid.New()}, ID: identifier}
		plugin := persistedTarget{RootKey: "app/plugin", Node: storage.Node{ID: uuid.New()}, ID: identifier}
		index := targetIndex{}
		index.add(app)
		index.add(plugin)

		resolved, ok := index.resolve(callSpec{ToIdentifier: identifier})
		Expect(ok).To(BeFalse())
		Expect(resolved).To(BeZero())

		resolved, ok = index.resolve(callSpec{ToIdentifier: identifier, ToRootKey: stringPointer("app")})
		Expect(ok).To(BeTrue())
		Expect(resolved.Node.ID).To(Equal(app.Node.ID))

		withoutSignature := identifier
		withoutSignature.Signature = ""
		resolved, ok = index.resolve(callSpec{ToIdentifier: withoutSignature, ToRootKey: stringPointer("app")})
		Expect(ok).To(BeTrue())
		Expect(resolved.Node.ID).To(Equal(app.Node.ID))

		overload := identifier
		overload.Signature = "(string)"
		index.add(persistedTarget{RootKey: "app", Node: storage.Node{ID: uuid.New()}, ID: overload})
		_, ok = index.resolve(callSpec{ToIdentifier: withoutSignature, ToRootKey: stringPointer("app")})
		Expect(ok).To(BeFalse())

		resolved, ok = index.resolve(callSpec{ToIdentifier: identifier, ToRootKey: stringPointer("app")})
		Expect(ok).To(BeTrue())
		Expect(resolved.Node.ID).To(Equal(app.Node.ID))
	})
})
