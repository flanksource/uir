package indexer

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Git-backed external publication validation", func() {
	DescribeTable("rejects incomplete bindings and paths outside the repository", func(repository, commit, path, message string) {
		publication := prodPublication("input", "100")
		publication.Location.RepositoryURI = repository
		publication.Options.GitCommit = commit
		publication.Documents[0].GitPath = path
		Expect(publication.validate()).To(MatchError(ContainSubstring(message)))
	},
		Entry("no repository", "", strings.Repeat("a", 40), "rules/a.xml", "requires location.repository_uri"),
		Entry("no commit", "file:///source.git", "", "rules/a.xml", "requires location.repository_uri"),
		Entry("parent traversal", "file:///source.git", strings.Repeat("a", 40), "../outside.xml", "repository-relative"),
		Entry("absolute path", "file:///source.git", strings.Repeat("a", 40), "/outside.xml", "repository-relative"),
		Entry("directory root", "file:///source.git", strings.Repeat("a", 40), ".", "repository-relative"),
		Entry("NUL", "file:///source.git", strings.Repeat("a", 40), "rules/a\x00.xml", "repository-relative"),
	)
})
