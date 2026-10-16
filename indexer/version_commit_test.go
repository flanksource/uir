package indexer

import (
	"context"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("versionCommit", func() {
	const tag = "v1.2.3"
	var (
		ctx      context.Context
		checkout string
		commit   string
		stamp    string
	)

	BeforeEach(func() {
		ctx = context.Background()
		checkout = canonicalTempDir()
		gitOutput(ctx, checkout, "init", "--quiet")
		writeFile(filepath.Join(checkout, "go.mod"), "module example.org/acme\n")
		commit = commitPaths(ctx, checkout, "initial", "go.mod")
		gitOutput(ctx, checkout, "tag", tag)
		committed, err := time.Parse(time.RFC3339, gitOutput(ctx, checkout, "show", "-s", "--format=%cI", commit))
		Expect(err).ToNot(HaveOccurred())
		stamp = committed.UTC().Format("20060102150405")
	})

	DescribeTable("resolves the commit a version names",
		func(version func() string) {
			Expect(versionCommit(ctx, checkout, version())).To(Equal(commit))
		},
		Entry("vX.0.0-timestamp-hash pseudo-version", func() string { return "v0.0.0-" + stamp + "-" + commit[:12] }),
		Entry("vX.Y.(Z+1)-0.timestamp-hash pseudo-version", func() string { return "v1.21.77-0." + stamp + "-" + commit[:12] }),
		Entry("vX.Y.Z-pre.0.timestamp-hash pseudo-version", func() string { return "v1.3.0-rc.1.0." + stamp + "-" + commit[:12] }),
		Entry("plain tag", func() string { return tag }),
	)

	It("fails for a pseudo-version whose hash no commit has", func() {
		_, err := versionCommit(ctx, checkout, "v1.21.77-0."+stamp+"-0123456789ab")
		Expect(err).To(HaveOccurred())
	})
})
