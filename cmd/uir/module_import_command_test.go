package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/indexer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// importExamples is the docs example: the publications of roots Co/Prod and Co/Prod/PlanA.
var importExamples = filepath.Join("..", "..", "docs", "examples", "import")

var _ = Describe("module import", func() {
	It("imports publications from a file and from stdin, and queries read their stored sources", func(ctx context.Context) {
		database := openCommandDatabase(ctx)
		runtime := &commandRuntime{database: database}
		execute := func(stdin io.Reader, args ...string) error {
			root := newRootCommand(runtime)
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			root.SetIn(stdin)
			root.SetArgs(args)
			return root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))
		}
		Expect(execute(strings.NewReader(""), "import", filepath.Join(importExamples, "co-prod.json"))).To(Succeed())
		planA, err := os.Open(filepath.Join(importExamples, "co-prod-plana.json"))
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(planA.Close)
		Expect(execute(planA, "import", "-")).To(Succeed())

		rows, err := listModuleRoots(ctx, database)
		Expect(err).ToNot(HaveOccurred())
		Expect(rows).To(HaveLen(2))
		Expect([]string{rows[0].RootKey, rows[0].Name, rows[0].Location, rows[1].RootKey, rows[1].Name, rows[1].Location}).To(Equal([]string{
			"Co/Prod", "Prod", "test://lab/Co/Prod", "Co/Prod/PlanA", "PlanA", "test://lab/Co/Prod/PlanA",
		}))
		callers, err := queryModules(ctx, database, moduleQueryOptions{Expression: "func:Calc <", GroupBy: "module,package,file", Limit: 100})
		Expect(err).ToNot(HaveOccurred())
		Expect(callers.Matches).To(HaveLen(1))
		Expect([]string{callers.Matches[0].Path, callers.Matches[0].usage}).To(Equal([]string{"rules/Apply.xml", `<Call rule="Calc"/>`}),
			"the CLI query reads the usage line from the source the publication stored")

		Expect(execute(strings.NewReader(""), "import")).To(MatchError(ContainSubstring("import requires a publication JSON file, or - for stdin")))
		Expect(execute(strings.NewReader("{"), "import", "-")).To(MatchError(ContainSubstring("decode the publication from stdin")))
		Expect(execute(strings.NewReader(""), "reindex", "test://lab/Co/Prod")).To(MatchError(ContainSubstring(
			`root "Co/Prod" at "test://lab/Co/Prod" is published by an external producer; republish it with uir import`)))
		results, err := reindexAllModules(ctx, database, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(BeEmpty(), "reindex --all leaves the published roots to their producer")
	})

	It("serves POST /api/v1/modules/import with the publication in the body", func(ctx context.Context) {
		_, handler := servedRuntime(ctx)
		publication, err := os.ReadFile(filepath.Join(importExamples, "co-prod.json"))
		Expect(err).ToNot(HaveOccurred())
		response := postJSON(ctx, handler, "/api/v1/modules/import", `{"publication":`+string(publication)+`}`)
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var result indexer.ModuleResult
		Expect(json.Unmarshal(response.Body.Bytes(), &result)).To(Succeed())
		Expect(result).To(Equal(indexer.ModuleResult{RootKey: "Co/Prod", Location: "test://lab/Co/Prod", SnapshotID: result.SnapshotID, HeadVersion: 1, Files: 2, ParsedFiles: 2}))

		for _, test := range []struct{ body, message string }{
			{`{"args":["/etc/hosts"]}`, "an HTTP import takes the publication in the body as publication, not a file"},
			{`{"publication":{"root_key":"Co/Prod"}}`, "invalid publication: name is required"},
		} {
			refused := postJSON(ctx, handler, "/api/v1/modules/import", test.body)
			Expect(refused.Code).To(Equal(http.StatusBadRequest), refused.Body.String())
			Expect(refused.Body.String()).To(ContainSubstring(test.message))
		}
		spec := httptest.NewRecorder()
		handler.ServeHTTP(spec, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
		Expect(spec.Body.String()).To(ContainSubstring(`"/api/v1/modules/import"`))
	})
})
