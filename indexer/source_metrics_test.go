package indexer

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Source metrics", func() {
	DescribeTable("counts physical lines without a phantom final line", func(content string, expected int) {
		Expect(physicalLineCount([]byte(content))).To(Equal(expected))
	},
		Entry("empty source", "", 0),
		Entry("unterminated line", "package sample", 1),
		Entry("terminated line", "package sample\n", 1),
		Entry("blank and comment lines", "package sample\n\n// comment\n", 3),
		Entry("CRLF", "package sample\r\n\r\n// comment\r\n", 3),
		Entry("unterminated last line", "package sample\n// comment", 2),
	)
})
