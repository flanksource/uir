package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/storage"
	uiweb "github.com/flanksource/uir/web"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("serve", func() {
	It("serves the embedded entry point at root and deep links", func() {
		ui, err := uiweb.Handler()
		Expect(err).ToNot(HaveOccurred())
		for _, path := range []string{"/", "/explorer"} {
			response := httptest.NewRecorder()
			ui.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			Expect(response.Code).To(Equal(http.StatusOK), path)
			Expect(response.Body.String()).To(ContainSubstring("UIR Snapshot Browser"))
		}
	})
	It("publishes project operations and the UI without exposing the process command", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		seedQueryProject(ctx, database)
		runtime := &commandRuntime{database: database}
		root := newRootCommand(runtime)
		Expect(clicky.IsLocalOnly(findCommand(root, "serve"))).To(BeTrue())

		ui := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("UIR browser"))
		})
		handler, err := newServeHandler(root, runtime, ui)
		Expect(err).ToNot(HaveOccurred())

		project := httptest.NewRecorder()
		handler.ServeHTTP(project, httptest.NewRequest(http.MethodGet, "/api/v1/project?format=json", nil))
		Expect(project.Code).To(Equal(http.StatusOK))
		Expect(project.Body.String()).To(ContainSubstring("acme"))

		page := httptest.NewRecorder()
		handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
		Expect(page.Code).To(Equal(http.StatusOK))
		Expect(page.Body.String()).To(Equal("UIR browser"))

		spec := httptest.NewRecorder()
		handler.ServeHTTP(spec, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
		Expect(spec.Code).To(Equal(http.StatusOK))
		Expect(strings.Contains(spec.Body.String(), `"/api/v1/serve"`)).To(BeFalse())
	})

	It("serves snapshot, source, node, and query data from Clicky routes", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		seedQueryProject(ctx, database)
		var snapshot storage.Snapshot
		Expect(database.First(&snapshot).Error).To(Succeed())
		var root storage.Root
		Expect(database.First(&root).Error).To(Succeed())
		workspace := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(workspace, "worker.go"), []byte("package acme\n"), 0o644)).To(Succeed())
		Expect(database.Model(&root).Update("local_path", workspace).Error).To(Succeed())
		var source storage.Source
		Expect(database.First(&source).Error).To(Succeed())
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).ToNot(HaveOccurred())

		for _, path := range []string{
			"/api/v1/snapshot?project=acme",
			"/api/v1/root?snapshot=" + snapshot.ID.String(),
			"/api/v1/source?snapshot=" + snapshot.ID.String(),
			"/api/v1/node?snapshot=" + snapshot.ID.String(),
		} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			Expect(response.Code).To(Equal(http.StatusOK), path+": "+response.Body.String())
			Expect(response.Body.String()).To(ContainSubstring(`"data"`))
		}
		content := httptest.NewRecorder()
		handler.ServeHTTP(content, httptest.NewRequest(http.MethodGet, "/api/v1/source/"+source.ID.String()+"/content", nil))
		Expect(content.Code).To(Equal(http.StatusOK), content.Body.String())
		Expect(content.Body.String()).To(ContainSubstring("package acme"))

		query := httptest.NewRecorder()
		queryRequest := httptest.NewRequest(http.MethodPost, "/api/v1/project/acme/query", strings.NewReader(`{"expression":"nodes where method = \"Run\"","snapshot":"`+snapshot.ID.String()+`"}`))
		queryRequest.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(query, queryRequest)
		Expect(query.Code).To(Equal(http.StatusOK), query.Body.String())
		Expect(query.Body.String()).To(ContainSubstring("method:acme.Worker:Run"))
	})

	It("reindexes a local checkout and publishes the resulting snapshot", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		workspace := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.org/cli\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package cli\n\nfunc Run() {}\n"), 0o644)).To(Succeed())
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).ToNot(HaveOccurred())
		body, err := json.Marshal(map[string]any{"path": workspace, "root": "workspace"})
		Expect(err).ToNot(HaveOccurred())
		request := httptest.NewRequest(http.MethodPost, "/api/v1/project/example/reindex", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var result struct {
			SnapshotID string `json:"snapshot_id"`
		}
		Expect(json.Unmarshal(response.Body.Bytes(), &result)).To(Succeed())
		Expect(result.SnapshotID).ToNot(BeEmpty())
		published := httptest.NewRecorder()
		handler.ServeHTTP(published, httptest.NewRequest(http.MethodGet, "/api/v1/snapshot?project=example", nil))
		Expect(published.Code).To(Equal(http.StatusOK), published.Body.String())
		Expect(published.Body.String()).To(ContainSubstring(result.SnapshotID))
	})
})
