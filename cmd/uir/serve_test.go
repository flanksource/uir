package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	uiweb "github.com/flanksource/uir/web"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("serve", func() {
	It("exposes module-root browsing and query operations", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		workspace := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.org/browser\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package browser\n\nfunc Run() {}\n"), 0o644)).To(Succeed())
		_, err := addModules(ctx, database, workspace, false)
		Expect(err).To(Succeed())
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).To(Succeed())

		listed := httptest.NewRecorder()
		handler.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/api/v1/modules", nil))
		Expect(listed.Code).To(Equal(http.StatusOK), listed.Body.String())
		var roots []moduleRootRow
		Expect(json.Unmarshal(listed.Body.Bytes(), &roots)).To(Succeed())
		Expect(roots).To(HaveLen(1))
		Expect(roots[0].RootKey).To(Equal("example.org/browser"))

		locations := httptest.NewRecorder()
		handler.ServeHTTP(locations, httptest.NewRequest(http.MethodGet, "/api/v1/modules/locations?root=example.org%2Fbrowser", nil))
		Expect(locations.Code).To(Equal(http.StatusOK), locations.Body.String())
		var locationRows []storage.ModuleLocationView
		Expect(json.Unmarshal(locations.Body.Bytes(), &locationRows)).To(Succeed())
		Expect(locationRows).To(HaveLen(1))
		Expect(locationRows[0].Primary).To(BeTrue())

		snapshots := httptest.NewRecorder()
		handler.ServeHTTP(snapshots, httptest.NewRequest(http.MethodGet,
			"/api/v1/modules/snapshots?root=example.org%2Fbrowser&location="+url.QueryEscape(locationRows[0].CanonicalPath), nil))
		Expect(snapshots.Code).To(Equal(http.StatusOK), snapshots.Body.String())
		var snapshotPage struct {
			Data []storage.ModuleSnapshotView `json:"data"`
		}
		Expect(json.Unmarshal(snapshots.Body.Bytes(), &snapshotPage)).To(Succeed())
		Expect(snapshotPage.Data).To(HaveLen(1))
		Expect(snapshotPage.Data[0].ID).To(Equal(locationRows[0].HeadSnapshotID))

		browsed := httptest.NewRecorder()
		handler.ServeHTTP(browsed, httptest.NewRequest(http.MethodGet,
			"/api/v1/modules/browse?snapshot="+snapshotPage.Data[0].ID.String(), nil))
		Expect(browsed.Code).To(Equal(http.StatusOK), browsed.Body.String())
		Expect(browsed.Body.String()).To(ContainSubstring("main.go"))
		Expect(browsed.Body.String()).To(ContainSubstring("Run"))
		content := httptest.NewRecorder()
		handler.ServeHTTP(content, httptest.NewRequest(http.MethodGet,
			"/api/v1/modules/content?snapshot="+snapshotPage.Data[0].ID.String()+"&path=main.go", nil))
		Expect(content.Code).To(Equal(http.StatusOK), content.Body.String())
		Expect(content.Body.String()).To(ContainSubstring("func Run()"))

		queried := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/modules/query", strings.NewReader(`{"args":["nodes where method = \"Run\""],"root":"example.org/browser"}`))
		request.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(queried, request)
		Expect(queried.Code).To(Equal(http.StatusOK), queried.Body.String())
		var queryRows []moduleQueryRow
		Expect(json.Unmarshal(queried.Body.Bytes(), &queryRows)).To(Succeed())
		Expect(queryRows).To(HaveLen(1))
		Expect(queryRows[0].Symbol).To(ContainSubstring("Run"))
	})

	It("adds and reindexes module roots through structured API operations", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		workspace := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.org/browser\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package browser\n\nfunc Run() {}\n"), 0o644)).To(Succeed())
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).To(Succeed())
		for _, operation := range []string{"add", "reindex"} {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/modules/"+operation,
				strings.NewReader(`{"args":["`+workspace+`"],"no-workspace-uses":true}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			Expect(response.Code).To(Equal(http.StatusOK), operation+": "+response.Body.String())
			var results []indexer.ModuleResult
			Expect(json.Unmarshal(response.Body.Bytes(), &results)).To(Succeed())
			Expect(results).To(HaveLen(1))
			Expect(results[0].RootKey).To(Equal("example.org/browser"))
			Expect(results[0].Unchanged).To(Equal(operation == "reindex"))
		}
	})

	It("serves the browser and omits removed project routes", func(ctx SpecContext) {
		ui, err := uiweb.Handler()
		Expect(err).To(Succeed())
		for _, path := range []string{"/", "/explorer"} {
			response := httptest.NewRecorder()
			ui.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			Expect(response.Code).To(Equal(http.StatusOK), path)
			Expect(response.Body.String()).To(ContainSubstring("UIR Snapshot Browser"))
		}
		runtime := &commandRuntime{database: openCommandDatabase(ctx)}
		root := newRootCommand(runtime)
		Expect(clicky.IsLocalOnly(findCommand(root, "serve"))).To(BeTrue())
		handler, err := newServeHandler(root, runtime, http.NotFoundHandler())
		Expect(err).To(Succeed())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/project", nil))
		Expect(response.Code).To(Equal(http.StatusNotFound))
		spec := httptest.NewRecorder()
		handler.ServeHTTP(spec, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
		Expect(spec.Code).To(Equal(http.StatusOK))
		Expect(spec.Body.String()).ToNot(ContainSubstring(`"/api/v1/project`))
		Expect(spec.Body.String()).ToNot(ContainSubstring(`"/api/v1/serve`))
	})
})
