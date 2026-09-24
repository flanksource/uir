package storage_test

import (
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("search names", func() {
	DescribeTable("restricts a name to lowercase [a-z0-9]",
		func(name, expected string) { Expect(storage.SearchName(name)).To(Equal(expected)) },
		Entry("mixed case", "SaveInvoice", "saveinvoice"),
		Entry("underscores are removed", "Save_V2", "savev2"),
		Entry("non-ASCII letters are removed", "Straßeé", "strae"),
		Entry("a blank identifier has no searchable character", "_", ""),
		Entry("a non-ASCII name has no searchable character", "π", ""),
	)

	type bound struct {
		Upper   string
		Bounded bool
	}
	DescribeTable("bounds a prefix range by incrementing within 0-9 then a-z, carrying past z",
		func(prefix string, expected bound) {
			upper, bounded, err := storage.SearchPrefixUpperBound(prefix)
			Expect(err).ToNot(HaveOccurred())
			Expect(bound{Upper: upper, Bounded: bounded}).To(Equal(expected))
		},
		Entry("a letter increments", "item", bound{Upper: "iten", Bounded: true}),
		Entry("a digit increments", "fiz8", bound{Upper: "fiz9", Bounded: true}),
		Entry("9 increments to a", "fiz9", bound{Upper: "fiza", Bounded: true}),
		Entry("a trailing z carries into the previous character", "fizz", bound{Upper: "fj", Bounded: true}),
		Entry("a carry reaches a digit", "a9zz", bound{Upper: "aa", Bounded: true}),
		Entry("every character z has no upper bound", "zz", bound{}),
	)

	DescribeTable("rejects a prefix outside the search alphabet",
		func(prefix, message string) {
			_, _, err := storage.SearchPrefixUpperBound(prefix)
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("an empty prefix", "", "empty"),
		Entry("an underscore", "save_", `'_'`),
		Entry("an uppercase letter", "Save", `'S'`),
	)
})
