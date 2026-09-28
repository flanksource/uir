package main

import (
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("explorer refactor commands", func() {
	It("uses the typed UIR-backed rename for a selected method", func() {
		source := query.ModuleSourceView{Path: "store/store.go", PackagePath: "example.org/service/store"}
		node := &query.ModuleNodeView{Identifier: uir.Identifier{Type: "Store", Method: "Save"}}
		args, err := refactorArgs(refactorOptions{Action: "rename", NewName: "Persist"}, source, node, "state.db", "")
		Expect(err).To(Succeed())
		Expect(args).To(Equal([]string{"rename", "method", "Store.Save", "Persist", "--package", source.PackagePath, "--uir-dsn", "state.db", "./..."}))
	})

	It("moves a file using its module-relative path", func() {
		source := query.ModuleSourceView{Path: "store/store.go"}
		args, err := refactorArgs(refactorOptions{Action: "move", Destination: "model/store.go"}, source, nil, "state.db", "")
		Expect(err).To(Succeed())
		Expect(args).To(Equal([]string{"move", "file", "store/store.go", "model/store.go"}))
	})

	It("maps a type move and rejects a field move", func() {
		source := query.ModuleSourceView{Path: "store/store.go", PackagePath: "example.org/service/store"}
		typeNode := &query.ModuleNodeView{Identifier: uir.Identifier{Type: "Store"}}
		args, err := refactorArgs(refactorOptions{Action: "move", Destination: "model/store.go"}, source, typeNode, "state.db", "")
		Expect(err).To(Succeed())
		Expect(args).To(Equal([]string{"move", "type", "Store", "model/store.go", "--package", source.PackagePath}))
		field := &query.ModuleNodeView{Identifier: uir.Identifier{Type: "Store", Field: "Name"}}
		_, err = refactorArgs(refactorOptions{Action: "move", Destination: "model/store.go"}, source, field, "state.db", "")
		Expect(err).To(MatchError(ContainSubstring("field move")))
	})

	It("rejects a destination that escapes the module", func() {
		_, err := refactorArgs(refactorOptions{Action: "move", Destination: "../outside.go"}, query.ModuleSourceView{Path: "store/store.go"}, nil, "state.db", "")
		Expect(err).To(MatchError(ContainSubstring("module-relative")))
	})
})
