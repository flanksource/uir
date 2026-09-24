package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

// indexedCommits indexes a one-file module at two commits and returns them.
func indexedCommits(ctx context.Context, database *gorm.DB) (string, string) {
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
	return commits[0], commits[1]
}

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(fn func() error) (string, error) {
	GinkgoHelper()
	reader, writer, err := os.Pipe()
	Expect(err).To(Succeed())
	original := os.Stdout
	os.Stdout = writer
	runErr := fn()
	os.Stdout = original
	Expect(writer.Close()).To(Succeed())
	output, err := io.ReadAll(reader)
	Expect(err).To(Succeed())
	return string(output), runErr
}

var _ = Describe("uir diff", func() {
	It("prints a commit diff as JSON with exported rows by default", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		from, to := indexedCommits(ctx, database)
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

	It("rejects a range without two revisions", func(ctx SpecContext) {
		runtime := &commandRuntime{database: openCommandDatabase(ctx)}
		root := newRootCommand(runtime)
		root.SetArgs([]string{"diff", "main", "--root", diffRoot})
		_, err := captureStdout(func() error {
			return root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))
		})
		Expect(err).To(MatchError(ContainSubstring(`commit range "main" is not <from>..<to>`)))
	})

	It("serves the diff with line counts over the operations API", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		from, to := indexedCommits(ctx, database)
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
})
