package symboldiff

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("history terminal layout", func() {
	It("shows the checkout, refs, and indexed commits in a coloured layout", func() {
		history := History{
			RootKey: "example.org/ledger", Location: "/work/ledger",
			Branches:     []GitRef{{Name: "main", Commit: strings.Repeat("a", 40)}},
			PullRequests: []PullRequestRef{{Number: 23, Commit: strings.Repeat("b", 40)}},
			Commits: []GitCommit{{
				Commit: strings.Repeat("b", 40), Subject: "feat: add invoice total", AuthoredAt: "2026-09-25T06:34:35+03:00",
				SnapshotID: "5f9c8efe-d077-4d9d-9a87-61a97e816db1", SnapshotReason: "add",
			}},
		}

		plain := history.Pretty().String()
		Expect(plain).To(Equal("example.org/ledger · history\nCheckout  /work/ledger\nBranches\n  main  aaaaaaaaaaaa\nPull requests\n  #23  bbbbbbbbbbbb\nCommits\n  bbbbbbbbbbbb  feat: add invoice total\n    2026-09-25T06:34:35+03:00  indexed (add)  5f9c8efe-d077\n"))
		Expect(history.Pretty().ANSI()).To(And(ContainSubstring("\x1b["), ContainSubstring("feat: add invoice total"), MatchRegexp(`indexed\x1b\[[0-9;]*m\x1b\[[0-9;]*m \(add\)`)))
	})

	It("makes missing pull request refs and an empty log explicit", func() {
		history := History{RootKey: "example.org/ledger", Location: "/work/ledger", PullRequestError: "origin remote is not configured"}
		Expect(history.Pretty().String()).To(ContainSubstring("Pull requests\n  unavailable: origin remote is not configured\nCommits\n  No commits\n"))
	})
})
