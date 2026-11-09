package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/testruns"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("uir tests plan identity", func() {
	It("compares GOEXPERIMENT as an unordered set", func() {
		Expect(canonicalGoExperiments("newinliner,arenas newinliner")).To(Equal("arenas,newinliner"))
	})

	It("excludes an ignored generated results file from worktree identity", func() {
		workspace := GinkgoT().TempDir()
		gitIn(workspace, "init", "--quiet", "--initial-branch=main")
		Expect(os.WriteFile(filepath.Join(workspace, ".gitignore"), []byte("run.json\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/results\n\ngo 1.25\n"), 0o644)).To(Succeed())
		gitIn(workspace, "add", "--all")
		gitIn(workspace, "commit", "--quiet", "--message", "baseline")
		results := filepath.Join(workspace, "run.json")
		Expect(os.WriteFile(results, []byte("generated"), 0o644)).To(Succeed())
		mismatch, err := ranOn(context.Background(), workspace, results, testruns.Run{GitCommit: gitIn(workspace, "rev-parse", "HEAD")})
		Expect(err).ToNot(HaveOccurred())
		Expect(mismatch).To(BeEmpty())
	})

	It("detects indexed input mutations around planning", func() {
		workspace := GinkgoT().TempDir()
		gitIn(workspace, "init", "--quiet", "--initial-branch=main")
		source := filepath.Join(workspace, "deep.go")
		requestFile := filepath.Join(GinkgoT().TempDir(), "plan.json")
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/deep\n\ngo 1.25\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(source, []byte(plannerIdentitySource("before")), 0o644)).To(Succeed())
		gitIn(workspace, "add", "--all")
		gitIn(workspace, "commit", "--quiet", "--message", "baseline")
		commit := gitIn(workspace, "rev-parse", "HEAD")
		Expect(os.WriteFile(requestFile, []byte("request"), 0o644)).To(Succeed())

		clean := testruns.Run{GitCommit: commit}
		mismatch, err := ranOn(context.Background(), workspace, requestFile, clean)
		Expect(err).ToNot(HaveOccurred())
		Expect(mismatch).To(BeEmpty())

		Expect(os.WriteFile(source, []byte(plannerIdentitySource("planned")), 0o644)).To(Succeed())
		gitIn(workspace, "add", "--all")
		tree := gitIn(workspace, "write-tree")
		gitIn(workspace, "reset", "--quiet")
		dirty := testruns.Run{GitCommit: commit, Dirty: true, GitTree: tree}
		mismatch, err = ranOn(context.Background(), workspace, requestFile, dirty)
		Expect(err).ToNot(HaveOccurred())
		Expect(mismatch).To(BeEmpty())

		Expect(os.WriteFile(source, []byte(plannerIdentitySource("mutated")), 0o644)).To(Succeed())
		mismatch, err = ranOn(context.Background(), workspace, requestFile, dirty)
		Expect(err).ToNot(HaveOccurred())
		Expect(mismatch).To(ContainSubstring("not git_tree"))
	})
})

func plannerIdentitySource(value string) string {
	return "package deep\n\nvar value = \"" + value + "\"\n"
}
