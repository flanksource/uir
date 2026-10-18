package indexer

import (
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/tools/go/packages"
)

// The deterministic messages are the ones a historical load of github.com/flanksource/gavel at
// 6b1a6093bec8 reports (GOWORK=off, -mod=readonly): its go.mod pins commons-db and captain versions
// that lack packages the commit imports.
var _ = Describe("coverage cause classification", func() {
	DescribeTable("a load error is deterministic only when it is a fact of the commit and its pinned modules",
		func(kind packages.ErrorKind, message string, deterministic bool) {
			Expect(deterministicLoadError(packages.Error{Kind: kind, Msg: message})).To(Equal(deterministic))
		},
		Entry("a package absent from the pinned module version", packages.ListError,
			"cannot find module providing package github.com/flanksource/commons-db/tracing/terminal: import lookup disabled by -mod=readonly", true),
		Entry("a package no required module provides", packages.ListError,
			"no required module provides package example.org/missing; to add it:\n\tgo get example.org/missing", true),
		Entry("an import the toolchain's standard library lacks", packages.ListError,
			"package example/missing is not in std (/usr/local/go/src/example/missing)", true),
		Entry("an import of a type the pinned version lacks", packages.TypeError, "undefined: captaincli.PromptRunHandler", true),
		Entry("an import of an absent package", packages.TypeError,
			`could not import github.com/flanksource/commons-db/tracing/terminal (invalid package name: "")`, true),
		Entry("a redeclaration", packages.TypeError, "traces redeclared in this block", true),
		Entry("a parse error", packages.ParseError, "expected ';', found 'EOF'", true),
		Entry("an unreachable proxy", packages.ListError,
			`example.org/missing@v1.0.0: Get "https://proxy.golang.org/example.org/missing/@v/v1.0.0.mod": dial tcp: lookup proxy.golang.org: no such host`, false),
		Entry("a proxy server error", packages.ListError,
			"example.org/missing@v1.0.0: reading https://proxy.golang.org/example.org/missing/@v/v1.0.0.zip: 502 Bad Gateway", false),
		Entry("a checksum database failure", packages.ListError,
			"example.org/missing@v1.0.0: verifying module: example.org/missing@v1.0.0: Get \"https://sum.golang.org/lookup/example.org/missing@v1.0.0\": net/http: TLS handshake timeout", false),
		Entry("a checksum mismatch", packages.ListError,
			"verifying example.org/missing@v1.0.0: checksum mismatch\n\tdownloaded: h1:a=\n\tgo.sum:     h1:b=\n\nSECURITY ERROR", false),
		Entry("a module cache permission failure", packages.ListError,
			"open /Users/example/go/pkg/mod/cache/download/example.org/missing/@v/v1.0.0.lock: permission denied", false),
		Entry("a type error carrying an I/O failure", packages.TypeError,
			"could not import example.org/missing (read /Users/example/go/pkg/mod/example.org/missing@v1.0.0/a.go: input/output error)", false),
		Entry("an unrecognised list error", packages.ListError, "go: something new went wrong", false),
		Entry("an unknown error", packages.UnknownError, "undefined: captaincli.PromptRunHandler", false),
	)

	DescribeTable("a dependency edge is deterministic only when its unresolved reason is a fact of selection or registration",
		func(reason string, deterministic bool) {
			Expect(deterministicDependency(storage.SnapshotDependency{UnresolvedReason: reason})).To(Equal(deterministic))
		},
		Entry("a resolved edge", "", true),
		Entry("a version no registered checkout contains", versionNotRegistered("example.org/pricing@v1.0.0").Error(), true),
		Entry("a module absent from the build list", reasonAbsentFromBuildList, true),
		Entry("a selected module not indexed", reasonSelectedNotIndexed, true),
		Entry("a failed module selection", "Go module selection failed: go: example.org/pricing@v1.0.0: Get \"https://proxy.golang.org\": dial tcp: i/o timeout", false),
		Entry("an unresolvable local dependency", `resolve local dependency "../pricing": lstat ../pricing: no such file or directory`, false),
		Entry("an unrecognised reason", "something new", false),
	)
})
