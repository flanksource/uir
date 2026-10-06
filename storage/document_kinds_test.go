package storage_test

import (
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DecodeDocument kinds", func() {
	var acmeKinds symbolhandle.Kinds
	BeforeEach(func() {
		var err error
		acmeKinds, err = symbolhandle.NewKinds([]symbolhandle.KindSpec{
			{Code: 63, Name: "acme.rule", Category: symbolhandle.CategoryCallable},
			{Code: 62, Name: "acme.catalog", Category: symbolhandle.CategoryContainer},
		})
		Expect(err).ToNot(HaveOccurred())
	})

	// declaring decodes the typed worker fixture with its method redeclared as kind.
	declaring := func(kind string, options ...storage.DecodeOption) error {
		document, source, content := typedWorkerDocument()
		content.Symbols[2].Kind = kind
		_, err := storage.DecodeDocument(encodeDocument(document, content), source, options...)
		return err
	}

	It("accepts a registered custom callable kind only with the database's registry", func() {
		Expect(declaring("acme.rule")).To(MatchError(ContainSubstring(`symbol kind "acme.rule" is not registered`)))
		Expect(declaring("acme.rule", storage.WithKinds(acmeKinds))).To(Succeed())
	})

	DescribeTable("accepts every declarable builtin kind outside the Go set without a type form",
		func(kind string) { Expect(declaring(kind)).To(Succeed()) },
		Entry(nil, "interface"), Entry(nil, "constructor"), Entry(nil, "record"), Entry(nil, "endpoint"),
		Entry(nil, "table"), Entry(nil, "column"), Entry(nil, "index"), Entry(nil, "foreign_key"), Entry(nil, "annotation"),
	)

	DescribeTable("refuses a declared container or builtin, whatever registry declares it",
		func(kind string) {
			Expect(declaring(kind, storage.WithKinds(acmeKinds))).To(MatchError(ContainSubstring(`a file cannot declare a "` + kind + `" symbol`)))
		},
		Entry(nil, "package"), Entry(nil, "module"), Entry(nil, "builtin"), Entry(nil, "acme.catalog"),
	)

	It("parses a Go shape only for the builtin type kind", func() {
		document, source, content := typedWorkerDocument()
		content.Symbols[0].Kind = "record"
		_, err := storage.DecodeDocument(encodeDocument(document, content), source)
		Expect(err).To(MatchError(ContainSubstring(`record symbol has type_form "struct"`)))
		content.Symbols[0].TypeForm, content.Symbols[0].Shape, content.Symbols[0].Implements = "", "record Service(Name)", nil
		_, err = storage.DecodeDocument(encodeDocument(document, content), source)
		Expect(err).ToNot(HaveOccurred(), "a record's shape is not Go syntax")
	})
})
