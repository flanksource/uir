package symboldiff

import (
	"bytes"
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var _ = Describe("filtered patch output", func() {
	It("emits a verified snapshot rename patch", func(ctx SpecContext) {
		database := openDatabase(ctx, sqliteOptions())
		repo := newRepository()
		repo.write(map[string]*string{"go.mod": text("module " + patchModule + "\n\ngo 1.26\n"), "run.go": text("package p\nfunc Before() int { return 1 }\n")})
		before := repo.commit("before")
		repo.index(ctx, database)
		repo.write(map[string]*string{"run.go": text("package p\nfunc After() int { return 1 }\n")})
		after := repo.commit("after")
		repo.index(ctx, database)
		result, err := Diff(ctx, database, Options{RootKey: patchModule, Revisions: []string{before, after}, Visibility: VisibilityAll, ChangeTypes: ChangeTypes{"renamed"}, EmitPatch: true})
		Expect(err).To(Succeed())
		Expect(string(result.Patch)).To(Equal(patchOf(repo, "--full-index", "--no-renames", before, after)))
	})
	DescribeTable("preserves exact hunks and handles conservative rename selection", func(ctx SpecContext, before, after, kind string, wantError, wantEmpty bool) {
		repo := newRepository()
		repo.write(map[string]*string{"go.mod": text("module " + patchModule + "\n\ngo 1.26\n"), "run.go": text(before)})
		repo.commit("before")
		repo.write(map[string]*string{"run.go": text(after)})
		raw := patchOf(repo, "--full-index", "--no-renames")
		result, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(raw), Locator: gitLocator(ctx, repo), Visibility: VisibilityAll, ChangeTypes: ChangeTypes{kind}, EmitPatch: true})
		if wantError {
			Expect(err).To(HaveOccurred())
			Expect(result.Patch).To(BeEmpty())
			return
		}
		Expect(err).To(Succeed())
		if wantEmpty {
			Expect(result.Patch).To(BeEmpty())
		} else {
			Expect(string(result.Patch)).To(Equal(raw))
		}
	},
		Entry("recursive function", "package p\nfunc Before(n int) int { if n == 0 { return n }; return Before(n-1) }\n", "package p\nfunc After(n int) int { if n == 0 { return n }; return After(n-1) }\n", "renamed", false, false),
		Entry("recursive type", "package p\ntype Before struct { Next *Before }\n", "package p\ntype After struct { Next *After }\n", "renamed", false, false),
		Entry("type with methods", "package p\ntype Before struct { Value int }\nfunc (b Before) Read() int { return b.Value }\n", "package p\ntype After struct { Value int }\nfunc (b After) Read() int { return b.Value }\n", "renamed", false, false),
		Entry("missing terminal newline", "package p\nfunc Before() {}", "package p\nfunc After() {}", "renamed", false, false),
		Entry("CRLF", "package p\r\nfunc Before() {}\r\n", "package p\r\nfunc After() {}\r\n", "renamed", false, false),
		Entry("changed implementation is not a pure rename", "package p\nfunc Before() int { return 1 }\n", "package p\nfunc After() int { return 2 }\n", "renamed", false, true),
		Entry("mixed caller line", "package p\nfunc Before() int { return 1 }\nfunc Caller() int { return Before() }\n", "package p\nfunc After() int { return 1 }\nfunc Caller() int { return After()+1 }\n", "renamed", true, false),
		Entry("two declarations on one line", "package p\nfunc Before() {}; func Other() int { return 1 }\n", "package p\nfunc After() {}; func Other() int { return 2 }\n", "renamed", true, false),
		Entry("ambiguous identical functions", "package p\nfunc Before() {}\nfunc Other() {}\n", "package p\nfunc After() {}\nfunc Another() {}\n", "renamed", false, true),
	)
	It("stages both halves of a move into a new file without changing the worktree", func(ctx SpecContext) {
		repo := newRepository()
		before := "package p\n\nfunc Move() int { return 1 }\n"
		after := "package p\n"
		repo.write(map[string]*string{"go.mod": text("module " + patchModule + "\n\ngo 1.26\n"), "old.go": text(before)})
		repo.commit("before")
		repo.write(map[string]*string{"old.go": text(after), "new.go": text(before)})
		patch, err := WorktreePatch(ctx, WorktreePatchOptions{Checkout: repo.path, Commit: "HEAD", Untracked: true})
		Expect(err).To(Succeed())
		result, err := DiffPatch(ctx, PatchOptions{Patch: patch, Locator: gitLocator(ctx, repo), Visibility: VisibilityAll, ChangeTypes: ChangeTypes{"moved"}, EmitPatch: true})
		Expect(err).To(Succeed())
		index := filepath.Join(repo.path, "staging-index")
		git := func(args ...string) string {
			cmd := exec.Command("git", append([]string{"-C", repo.path}, args...)...)
			cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+index)
			if len(args) > 0 && args[0] == "apply" {
				cmd.Stdin = bytes.NewReader(result.Patch)
			}
			output, err := cmd.CombinedOutput()
			Expect(err).To(Succeed(), string(output))
			return string(output)
		}
		git("read-tree", "HEAD")
		git("apply", "--cached", "--check")
		git("apply", "--cached")
		Expect(git("show", ":new.go")).To(Equal(before))
		Expect(git("show", ":old.go")).To(Equal(after))
		content, err := os.ReadFile(filepath.Join(repo.path, "new.go"))
		Expect(err).To(Succeed())
		Expect(string(content)).To(Equal(before))
	})
	It("preserves a rename and its callers as original hunks while omitting another file", func(ctx SpecContext) {
		repo := newRepository()
		repo.write(map[string]*string{"go.mod": text("module " + patchModule + "\n\ngo 1.26\n"), "run.go": text("package p\nfunc Before() int { return 1 }\nfunc Caller() int { return Before() }\n"), "other.go": text("package p\nfunc Other() int { return 1 }\n")})
		repo.commit("before")
		repo.write(map[string]*string{"run.go": text("package p\nfunc After() int { return 1 }\nfunc Caller() int { return After() }\n"), "other.go": text("package p\nfunc Other() int { return 2 }\n")})
		patch := patchOf(repo, "--full-index", "--no-renames")
		result, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(patch), Locator: gitLocator(ctx, repo), Visibility: VisibilityAll, ChangeTypes: ChangeTypes{"renamed"}, EmitPatch: true})
		Expect(err).To(Succeed())
		Expect(string(result.Patch)).To(Equal(patchOf(repo, "--full-index", "--no-renames", "--", "run.go")))
		Expect(result.Packages[0].Files[0].Rows[0].NameBefore).To(Equal("Before"))
	})
	It("rejects a hunk mixing a rename with an implementation edit", func(ctx SpecContext) {
		repo := newRepository()
		repo.write(map[string]*string{"go.mod": text("module " + patchModule + "\n\ngo 1.26\n"), "run.go": text("package p\nfunc Before() int { return 1 }\nfunc Other() int { return 1 }\n")})
		repo.commit("before")
		repo.write(map[string]*string{"run.go": text("package p\nfunc After() int { return 1 }\nfunc Other() int { return 2 }\n")})
		result, err := DiffPatch(context.Background(), PatchOptions{Patch: strings.NewReader(patchOf(repo, "--no-renames")), Locator: gitLocator(ctx, repo), Visibility: VisibilityAll, ChangeTypes: ChangeTypes{"renamed"}, EmitPatch: true})
		Expect(err).To(MatchError(ContainSubstring("mixed hunk")))
		Expect(result.Patch).To(BeEmpty())
	})
})
