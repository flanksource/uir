package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("revision worktree cleanup", func() {
	var checkout, scratch, head string

	BeforeEach(func(ctx SpecContext) {
		checkout = canonicalTempDir()
		scratch = canonicalTempDir()
		gitOutput(ctx, checkout, "init")
		writeFile(filepath.Join(checkout, "snapshot.txt"), "historical snapshot\n")
		head = commitPaths(ctx, checkout, "test: snapshot", "snapshot.txt")
	})

	It("removes the registration when a post-checkout hook makes worktree creation fail", func(ctx SpecContext) {
		hooks := canonicalTempDir()
		Expect(os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte("#!/bin/sh\nexit 1\n"), 0o755)).To(Succeed())
		gitOutput(ctx, checkout, "config", "core.hooksPath", hooks)

		_, err := revisionWorktree(ctx, revisionWorktreeOptions{Repository: checkout, Commit: head, Directory: scratch})

		Expect(err).To(HaveOccurred())
		Expect(strings.Count(gitOutput(ctx, checkout, "worktree", "list", "--porcelain"), "worktree ")).To(Equal(1))
		entries, err := os.ReadDir(scratch)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	It("removes its missing locked registration after cancellation without pruning other worktrees", func(ctx SpecContext) {
		other := filepath.Join(canonicalTempDir(), "other")
		gitOutput(ctx, checkout, "worktree", "add", "--detach", other, head)
		Expect(os.RemoveAll(other)).To(Succeed())
		workCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		prepared, err := revisionWorktree(workCtx, revisionWorktreeOptions{Repository: checkout, Commit: head, Directory: scratch})
		Expect(err).NotTo(HaveOccurred())
		gitOutput(ctx, checkout, "worktree", "lock", "--reason", "initializing", prepared.worktree)
		Expect(os.RemoveAll(prepared.worktree)).To(Succeed())
		cancel()

		Expect(prepared.cleanup()).To(Succeed())

		listed := gitOutput(ctx, checkout, "worktree", "list", "--porcelain")
		Expect(listed).NotTo(ContainSubstring(prepared.worktree))
		Expect(listed).To(ContainSubstring(other))
		Expect(prepared.cleanup()).To(Succeed(), "cleanup is idempotent")
	})

	It("cleans scratch when creation fails before a worktree is registered", func(ctx SpecContext) {
		_, err := revisionWorktree(ctx, revisionWorktreeOptions{Repository: checkout, Commit: "missing-revision", Directory: scratch})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).NotTo(ContainSubstring("is not a working tree"))
		entries, err := os.ReadDir(scratch)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	It("cleans a locked registration when creation is canceled", func(ctx SpecContext) {
		hooks := canonicalTempDir()
		hook := "#!/bin/sh\ngit worktree lock --reason initializing \"$PWD\"\nkill -STOP \"$PPID\"\ntouch ready\n"
		Expect(os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte(hook), 0o755)).To(Succeed())
		gitOutput(ctx, checkout, "config", "core.hooksPath", hooks)
		workCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		finished := make(chan error, 1)
		go func() {
			_, err := revisionWorktree(workCtx, revisionWorktreeOptions{Repository: checkout, Commit: head, Directory: scratch})
			finished <- err
		}()
		Eventually(func() ([]string, error) {
			return filepath.Glob(filepath.Join(scratch, "uir-revision-*", "worktree", "ready"))
		}).WithTimeout(10 * time.Second).Should(HaveLen(1))
		cancel()

		Eventually(finished).WithTimeout(10 * time.Second).Should(Receive(HaveOccurred()))
		Expect(strings.Count(gitOutput(ctx, checkout, "worktree", "list", "--porcelain"), "worktree ")).To(Equal(1))
		entries, err := os.ReadDir(scratch)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})
})
