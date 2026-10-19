package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/symboldiff"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	diffRoot     = "example.org/diffcli"
	diffBefore   = "package diffcli\n\nfunc Run(name string) string { return name }\n\nfunc helper() int { return 1 }\n"
	diffAfter    = "package diffcli\n\nfunc Run(name string, loud bool) string { return name }\n\nfunc helper() int { return 2 }\n"
	diffRunAfter = "func Run(name string, loud bool) string"
)

func gitIn(directory string, args ...string) string {
	GinkgoHelper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.org", "GIT_AUTHOR_DATE=2026-01-01T12:00:00Z",
		"GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.org", "GIT_COMMITTER_DATE=2026-01-01T12:00:00Z",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
	)
	output, err := command.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), "git %v: %s", args, output)
	return strings.TrimSpace(string(output))
}

// indexedCommits indexes a one-file module at two commits and returns them and its canonical checkout.
func indexedCommits(ctx context.Context, database *gorm.DB) (string, string, string) {
	GinkgoHelper()
	workspace, err := filepath.EvalSymlinks(GinkgoT().TempDir())
	Expect(err).To(Succeed())
	gitIn(workspace, "init", "--quiet", "--initial-branch=main")
	Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module "+diffRoot+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
	commits := make([]string, 0, 2)
	for _, source := range []string{diffBefore, diffAfter} {
		Expect(os.WriteFile(filepath.Join(workspace, "run.go"), []byte(source), 0o644)).To(Succeed())
		gitIn(workspace, "add", "--all")
		gitIn(workspace, "commit", "--quiet", "--message", "commit")
		commits = append(commits, gitIn(workspace, "rev-parse", "HEAD"))
		_, err := addModules(ctx, database, workspace, false)
		Expect(err).ToNot(HaveOccurred())
	}
	return commits[0], commits[1], workspace
}

// chdir makes directory the working directory for the rest of the spec.
func chdir(directory string) {
	GinkgoHelper()
	original, err := os.Getwd()
	Expect(err).To(Succeed())
	Expect(os.Chdir(directory)).To(Succeed())
	DeferCleanup(os.Chdir, original)
}

// runCLI runs the uir command line against runtime and returns what it printed.
func runCLI(ctx context.Context, runtime *commandRuntime, args ...string) (string, error) {
	GinkgoHelper()
	root := newRootCommand(runtime)
	root.SetErr(io.Discard)
	root.SetArgs(args)
	return captureStdout(func() error {
		return root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))
	})
}

// decodeDiff runs a diff on the command line and decodes its JSON result.
func decodeDiff(ctx context.Context, runtime *commandRuntime, args ...string) symboldiff.Result {
	GinkgoHelper()
	output, err := runCLI(ctx, runtime, append(append([]string{"diff"}, args...), "--format", "json")...)
	Expect(err).ToNot(HaveOccurred(), output)
	var result symboldiff.Result
	Expect(json.Unmarshal([]byte(output), &result)).To(Succeed(), output)
	return result
}

var _ = Describe("uir diff", func() {
	It("prints a commit diff as JSON with exported rows by default", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		from, to, _ := indexedCommits(ctx, database)
		runtime := &commandRuntime{database: database}
		root := newRootCommand(runtime)
		root.SetArgs([]string{"diff", from + ".." + to, "--root", diffRoot, "--format", "json"})
		output, err := captureStdout(func() error {
			return root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))
		})
		Expect(err).ToNot(HaveOccurred(), output)
		var result symboldiff.Result
		Expect(json.Unmarshal([]byte(output), &result)).To(Succeed(), output)
		Expect([]any{result.Visibility, result.Stat, len(result.Packages)}).To(Equal([]any{symboldiff.VisibilityExported, false, 1}))
		file := result.Packages[0].Files[0]
		Expect([]any{file.Path, file.HiddenRows, len(file.Rows)}).To(Equal([]any{"run.go", 1, 2}))
		Expect([]string{string(file.Rows[0].Class), file.Rows[1].Group, file.Rows[1].ShapeAfter}).To(Equal([]string{"removed", "Run", diffRunAfter}))
	})

	It("reads two revisions and A...B as git diff does", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		from, to, workspace := indexedCommits(ctx, database)
		runtime := &commandRuntime{database: database}
		chdir(workspace)
		ranged := decodeDiff(ctx, runtime, from+".."+to)

		Expect([]symboldiff.Result{decodeDiff(ctx, runtime, from, to), decodeDiff(ctx, runtime, from+"..."+to)}).To(Equal([]symboldiff.Result{ranged, ranged}))
	})

	It("compares a single revision against the worktree of the current checkout", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		from, _, workspace := indexedCommits(ctx, database)
		runtime := &commandRuntime{database: database}
		chdir(workspace)
		Expect(os.WriteFile(filepath.Join(workspace, "run.go"), []byte(diffAfter+"\nfunc Stop() {}\n"), 0o644)).To(Succeed())

		result := decodeDiff(ctx, runtime, "HEAD~1")
		_, err := runCLI(ctx, runtime, "diff", "HEAD~1", "--auto-index")

		file := result.Packages[0].Files[0]
		Expect([]any{result.RootKey, result.From.Commit, result.To.Commit, file.Path, len(file.Rows)}).To(Equal([]any{diffRoot, from, symboldiff.PatchWorktree, "run.go", 3}))
		Expect(err).To(MatchError(ContainSubstring("--auto-index applies to a range of two commits")))
	})

	It("defaults --root and --location to the registered checkout of the working directory", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		from, to, workspace := indexedCommits(ctx, database)
		runtime := &commandRuntime{database: database}
		explicit := decodeDiff(ctx, runtime, from+".."+to, "--root", diffRoot)
		chdir(workspace)

		implicit := decodeDiff(ctx, runtime, from+".."+to)
		head := decodeDiff(ctx, runtime, "HEAD~1..HEAD")

		Expect(implicit).To(Equal(explicit))
		Expect([]any{head.RootKey, head.From.Commit, head.To.Commit, head.Packages}).To(Equal([]any{diffRoot, from, to, explicit.Packages}))
	})

	It("requires --root with a commit range outside any registered checkout", func(ctx SpecContext) {
		runtime := &commandRuntime{database: openCommandDatabase(ctx)}
		chdir(GinkgoT().TempDir())
		_, err := runCLI(ctx, runtime, "diff", "HEAD~1..HEAD")
		Expect(err).To(MatchError(ContainSubstring("--root is required")))
	})

	It("diffs a revision against the worktree of an unregistered repository, untracked files included, and refuses a range there", func(ctx SpecContext) {
		workspace, err := filepath.EvalSymlinks(GinkgoT().TempDir())
		Expect(err).To(Succeed())
		gitIn(workspace, "init", "--quiet", "--initial-branch=main")
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module "+diffRoot+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, "run.go"), []byte(diffBefore), 0o644)).To(Succeed())
		gitIn(workspace, "add", "--all")
		gitIn(workspace, "commit", "--quiet", "--message", "commit")
		gitIn(workspace, "commit", "--quiet", "--allow-empty", "--message", "empty")
		Expect(os.WriteFile(filepath.Join(workspace, "run.go"), []byte("package diffcli\n\nfunc helper() int { return 1 }\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, "moved.go"), []byte("package diffcli\n\nfunc Run(name string) string { return name }\n"), 0o644)).To(Succeed())
		runtime := &commandRuntime{database: openCommandDatabase(ctx)}
		chdir(workspace)

		result := decodeDiff(ctx, runtime, "HEAD", "--untracked", "--visibility", "all", "--change-type", "moved")
		var rows []string
		for _, file := range result.Packages[0].Files {
			for _, row := range file.Rows {
				rows = append(rows, fmt.Sprintf("%s %s %s %s->%s away=%t", file.Path, row.Class, row.Name, row.PathBefore, row.PathAfter, row.MovedAway))
			}
		}
		_, rangeErr := runCLI(ctx, runtime, "diff", "HEAD~1..HEAD")

		Expect([]any{result.RootKey, result.To.Commit, result.ChangeTypes, rows}).To(Equal([]any{diffRoot, symboldiff.PatchWorktree, symboldiff.ChangeTypes{"moved"}, []string{
			"moved.go moved Run run.go->moved.go away=false",
			"run.go moved Run run.go->moved.go away=true",
		}}))
		Expect(rangeErr).To(MatchError(ContainSubstring(`--root is required for the range "HEAD~1..HEAD", which compares indexed snapshots`)))
	})

	It("prints the symbol diff of a worktree patch piped on stdin, without an index", func(ctx SpecContext) {
		workspace, err := filepath.EvalSymlinks(GinkgoT().TempDir())
		Expect(err).To(Succeed())
		gitIn(workspace, "init", "--quiet", "--initial-branch=main")
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module "+diffRoot+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, "run.go"), []byte(diffBefore), 0o644)).To(Succeed())
		gitIn(workspace, "add", "--all")
		gitIn(workspace, "commit", "--quiet", "--message", "commit")
		Expect(os.WriteFile(filepath.Join(workspace, "run.go"), []byte(diffAfter), 0o644)).To(Succeed())
		patch := gitIn(workspace, "diff") + "\n"
		original, err := os.Getwd()
		Expect(err).To(Succeed())
		Expect(os.Chdir(workspace)).To(Succeed())
		DeferCleanup(os.Chdir, original)
		runtime := &commandRuntime{}
		root := newRootCommand(runtime)
		root.SetIn(strings.NewReader(patch))
		root.SetArgs([]string{"diff", "--format", "json"})
		output, err := captureStdout(func() error {
			return root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))
		})
		Expect(err).ToNot(HaveOccurred(), output)
		var result symboldiff.Result
		Expect(json.Unmarshal([]byte(output), &result)).To(Succeed(), output)
		file := result.Packages[0].Files[0]
		Expect([]any{result.RootKey, result.To.Commit, file.Path, file.HiddenRows, len(file.Rows)}).To(Equal([]any{diffRoot, symboldiff.PatchWorktree, "run.go", 1, 2}))
		Expect([]string{string(file.Rows[0].Class), file.Rows[1].Group}).To(Equal([]string{"removed", "Run"}))
	})

	It("refuses commit range flags with a patch on stdin", func(ctx SpecContext) {
		runtime := &commandRuntime{}
		root := newRootCommand(runtime)
		root.SetIn(strings.NewReader(""))
		root.SetArgs([]string{"diff", "-", "--root", diffRoot})
		_, err := captureStdout(func() error {
			return root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))
		})
		Expect(err).To(MatchError(ContainSubstring("--root applies to a commit range, not a patch on stdin")))
	})

	It("serves the diff with line counts over the operations API", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		from, to, _ := indexedCommits(ctx, database)
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).To(Succeed())
		body, err := json.Marshal(map[string]any{"args": []string{from + ".." + to}, "root": diffRoot, "visibility": "all", "stat": true})
		Expect(err).To(Succeed())
		request := httptest.NewRequest(http.MethodPost, "/api/v1/modules/diff", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var result symboldiff.Result
		Expect(json.Unmarshal(response.Body.Bytes(), &result)).To(Succeed(), response.Body.String())
		file := result.Packages[0].Files[0]
		Expect([]any{file.HiddenRows, len(file.Rows), *file.Lines}).To(Equal([]any{0, 3, symboldiff.LineCount{Added: 2, Removed: 2}}))
		Expect(file.Rows[2].ShapeDiff).To(BeNil(), "helper is a body row")
		Expect(file.Rows[1].ShapeDiff[1].Tokens).To(ContainElement(symboldiff.Token{Op: symboldiff.OpInsert, Text: ", loud bool"}))
	})

	It("serves branch and commit choices for the comparison view", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		from, to, _ := indexedCommits(ctx, database)
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).To(Succeed())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/modules/history?root="+url.QueryEscape(diffRoot), nil))
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var history symboldiff.History
		Expect(json.Unmarshal(response.Body.Bytes(), &history)).To(Succeed(), response.Body.String())
		Expect(history.Branches).To(ContainElement(symboldiff.GitRef{Name: "main", Commit: to}))
		Expect([]string{history.Commits[0].Commit, history.Commits[0].Parents[0]}).To(Equal([]string{to, from}))
	})
})
