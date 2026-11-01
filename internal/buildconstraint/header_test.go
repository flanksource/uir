package buildconstraint

import (
	"context"
	"fmt"
	"go/build/constraint"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBuildConstraints(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Go build constraints")
}

var _ = Describe("Go build constraints", func() {
	It("agrees with exhaustive Boolean evaluation for composed expressions", func() {
		atoms := []string{"hack", "linux", "darwin", "!hack", "!linux", "!darwin"}
		tags := []string{"hack", "linux", "darwin"}
		for _, left := range atoms {
			for _, right := range atoms {
				for _, operator := range []string{"&&", "||"} {
					expression := "!(" + left + " " + operator + " " + right + ") || (hack && !linux)"
					header, err := Parse([]byte("//go:build " + expression + "\n\npackage sample\n"))
					Expect(err).NotTo(HaveOccurred())
					parsed, err := constraint.Parse("//go:build " + expression)
					Expect(err).NotTo(HaveOccurred())
					expected := map[string]bool{"hack": true, "linux": true, "darwin": true}
					satisfiable := false
					for mask := range 1 << len(tags) {
						assignment := map[string]bool{}
						for bit, tag := range tags {
							assignment[tag] = mask&(1<<bit) != 0
						}
						if parsed.Eval(func(tag string) bool { return assignment[tag] }) {
							satisfiable = true
							for _, tag := range tags {
								expected[tag] = expected[tag] && assignment[tag]
							}
						}
					}
					if !satisfiable {
						for _, tag := range tags {
							expected[tag] = false
						}
					}
					actual, err := header.Requires(context.Background(), tags)
					Expect(err).NotTo(HaveOccurred())
					Expect(actual).To(Equal(expected), expression)
				}
			}
		}
	})
	DescribeTable("determines semantic necessity rather than tag occurrence", func(expression string, expected bool) {
		header, err := Parse([]byte("//go:build " + expression + "\n\npackage sample\n"))
		Expect(err).NotTo(HaveOccurred())
		actual, err := header.Requires(context.Background(), []string{"hack", "absent"})
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(map[string]bool{"hack": expected, "absent": false}))
	},
		Entry("single tag", "hack", true),
		Entry("conjunction", "hack && linux", true),
		Entry("required on both branches", "(hack && linux) || (hack && darwin)", true),
		Entry("correlated positive and negative tags", "(hack || linux) && !linux", true),
		Entry("double negation", "!(!hack)", true),
		Entry("De Morgan", "!(!hack || linux)", true),
		Entry("alternative", "hack || linux", false),
		Entry("negated", "!hack", false),
		Entry("ignore", "ignore", false),
		Entry("contradiction", "hack && !hack", false),
		Entry("tautology", "hack || !hack", false),
		Entry("case sensitive", "HACK", false),
	)

	DescribeTable("recognizes only modern directives in the leading header", func(source, expected string) {
		header, err := Parse([]byte(source))
		Expect(err).NotTo(HaveOccurred())
		Expect(header.String()).To(Equal(expected))
	},
		Entry("none", "package sample\n", ""),
		Entry("legacy only", "// +build hack\n\npackage sample\n", ""),
		Entry("license and normalization", "// Copyright example\n\n//go:build (hack&&linux)\n\npackage sample\n", "hack && linux"),
		Entry("BOM and CRLF", "\ufeff//go:build hack\r\n\r\npackage sample\r\n", "hack"),
		Entry("block comment then directive", "/* license */\n//go:build hack\n\npackage sample\n", "hack"),
		Entry("inside block comment", "/*\n//go:build hack\n*/\npackage sample\n", ""),
		Entry("after package", "package sample\n//go:build hack\n", ""),
		Entry("inside string", "package sample\nvar s = `//go:build hack`\n", ""),
		Entry("not a standalone line", "/* license */ //go:build hack\npackage sample\n", ""),
		Entry("modern controls", "//go:build hack\n// +build ignore\n\npackage sample\n", "hack"),
	)

	DescribeTable("rejects malformed or duplicate recognized directives", func(source string) {
		_, err := Parse([]byte(source))
		Expect(err).To(HaveOccurred())
	},
		Entry("empty", "//go:build\n\npackage sample\n"),
		Entry("malformed", "//go:build hack &&\n\npackage sample\n"),
		Entry("duplicate", "//go:build hack\n//go:build linux\n\npackage sample\n"),
	)

	It("does not require a tag when no modern directive exists", func() {
		header, err := Parse([]byte("package sample\n"))
		Expect(err).NotTo(HaveOccurred())
		actual, err := header.Requires(context.Background(), []string{"hack"})
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(map[string]bool{"hack": false}))
	})

	DescribeTable("validates a single positive tag", func(tag string, valid bool) {
		err := ValidateTag(tag)
		Expect(err == nil).To(Equal(valid))
	}, Entry("hack", "hack", true), Entry("version", "go1.27", true), Entry("empty", "", false), Entry("negation", "!hack", false), Entry("expression", "hack || linux", false), Entry("whitespace", " hack", false))

	It("observes cancellation", func() {
		header, err := Parse([]byte("//go:build hack\n\npackage sample\n"))
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = header.Requires(ctx, []string{"hack"})
		Expect(err).To(MatchError(context.Canceled))
	})

	It("fails explicitly when exact analysis exceeds its node budget", func() {
		var terms []string
		for i := range 20 {
			terms = append(terms, fmt.Sprintf("(a%02d && z%02d)", i, i))
		}
		header, err := Parse([]byte("//go:build " + strings.Join(terms, " || ") + "\n\npackage sample\n"))
		Expect(err).NotTo(HaveOccurred())
		_, err = header.Requires(context.Background(), []string{"hack"})
		Expect(err).To(MatchError(ContainSubstring("65536")))
	})
})
