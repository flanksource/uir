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
	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	uiweb "github.com/flanksource/uir/web"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("serve", func() {
	It("lists files from every module checkout head without sending symbol payloads", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		workspace := GinkgoT().TempDir()
		for _, fixture := range []struct{ directory, module, file string }{
			{"service-a", "example.org/service", "first.go"},
			{"service-b", "example.org/service", "second.go"},
			{"worker", "example.org/worker", "worker.go"},
		} {
			path := filepath.Join(workspace, fixture.directory)
			Expect(os.Mkdir(path, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(path, "go.mod"), []byte("module "+fixture.module+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(path, fixture.file), []byte("package sample\n\nfunc Run() {}\n"), 0o644)).To(Succeed())
			_, err := addModules(ctx, database, path, false)
			Expect(err).To(Succeed())
		}
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).To(Succeed())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/modules/heads", nil))
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var heads []query.ModuleHeadFiles
		Expect(json.Unmarshal(response.Body.Bytes(), &heads)).To(Succeed())
		Expect(heads).To(HaveLen(3))
		filesByHead := make(map[string][]string, len(heads))
		for _, head := range heads {
			Expect(head.SnapshotID).ToNot(BeEmpty())
			for _, source := range head.Sources {
				Expect(source.RootKey).To(Equal(head.RootKey))
				Expect(source.Location).To(Equal(head.Location))
				Expect(source.SnapshotID).To(Equal(head.SnapshotID))
				filesByHead[head.RootKey+"@"+head.Location] = append(filesByHead[head.RootKey+"@"+head.Location], source.Path)
			}
		}
		canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
		Expect(err).To(Succeed())
		Expect(filesByHead).To(Equal(map[string][]string{
			"example.org/service@" + filepath.Join(canonicalWorkspace, "service-a"): {"first.go"},
			"example.org/service@" + filepath.Join(canonicalWorkspace, "service-b"): {"second.go"},
			"example.org/worker@" + filepath.Join(canonicalWorkspace, "worker"):     {"worker.go"},
		}))
		Expect(response.Body.String()).ToNot(ContainSubstring(`"nodes"`))
		spec := httptest.NewRecorder()
		handler.ServeHTTP(spec, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
		Expect(spec.Body.String()).To(ContainSubstring(`"/api/v1/modules/heads"`))
	})
	It("reports the running backend and active SQLite database without connection secrets", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		runtime := &commandRuntime{database: database, DSN: "sqlite://user:secret@unused/should-not-appear.db"}
		root := newRootCommand(runtime)
		root.Version = "v1.2.3"
		handler, err := newServeHandler(root, runtime, http.NotFoundHandler())
		Expect(err).To(Succeed())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil))
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var info struct {
			BackendVersion    string `json:"backend_version"`
			DatabaseType      string `json:"database_type"`
			DatabaseVersion   string `json:"database_version"`
			DatabaseLocation  string `json:"database_location"`
			DatabaseSizeBytes int64  `json:"database_size_bytes"`
		}
		Expect(json.Unmarshal(response.Body.Bytes(), &info)).To(Succeed())
		Expect(info.BackendVersion).To(Equal("v1.2.3"))
		Expect(info.DatabaseType).To(Equal("SQLite"))
		Expect(info.DatabaseVersion).To(MatchRegexp(`^\d+\.\d+\.\d+`))
		Expect(info.DatabaseLocation).To(HaveSuffix("command.db"))
		Expect(info.DatabaseSizeBytes).To(BeNumerically(">", 0))
		Expect(response.Body.String()).ToNot(ContainSubstring("secret"))
		spec := httptest.NewRecorder()
		handler.ServeHTTP(spec, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
		Expect(spec.Body.String()).To(ContainSubstring(`"/api/v1/system/info"`))
	})

	It("reports the active PostgreSQL server, database, schema, and size", func(ctx SpecContext) {
		database, err := storage.UirDB(ctx, storage.DBOptions{DSN: dbtest.ForGinkgo(dbtest.Options{Name: "uir_info"}).DSN(), Schema: "uir_info"})
		Expect(err).To(Succeed())
		DeferCleanup(func() {
			connection, err := database.DB()
			Expect(err).To(Succeed())
			Expect(connection.Close()).To(Succeed())
		})
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).To(Succeed())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil))
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var info systemInfo
		Expect(json.Unmarshal(response.Body.Bytes(), &info)).To(Succeed())
		Expect(info.DatabaseType).To(Equal("PostgreSQL"))
		Expect(info.DatabaseVersion).ToNot(BeEmpty())
		var databaseName string
		Expect(database.Raw("SELECT current_database()").Scan(&databaseName).Error).To(Succeed())
		Expect(info.DatabaseLocation).To(ContainSubstring(databaseName + " (schema uir_info)"))
		Expect(info.DatabaseLocation).ToNot(ContainSubstring("/128"))
		Expect(info.DatabaseSizeBytes).To(BeNumerically(">", 0))
	})
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
		var snapshotKeys struct {
			Data []map[string]any `json:"data"`
		}
		Expect(json.Unmarshal(snapshots.Body.Bytes(), &snapshotKeys)).To(Succeed())
		Expect(snapshotKeys.Data).To(HaveLen(1))
		Expect(snapshotKeys.Data[0]).To(And(HaveKeyWithValue("worktree_state", "unknown"), HaveKeyWithValue("coverage", "indexed"),
			HaveKey("completed_at"), Not(HaveKey("state"))), "a snapshot reports its worktree state and coverage, not a publication state")
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
		var queryResult moduleQueryResult
		Expect(json.Unmarshal(queried.Body.Bytes(), &queryResult)).To(Succeed())
		Expect(queryResult.Operation).To(Equal(query.OperationNodes))
		Expect(queryResult.Total).To(Equal(1))
		Expect(queryResult.Matches).To(HaveLen(1))
		Expect(queryResult.Matches[0].Symbol).To(ContainSubstring("Run"))
	})

	It("serves the query envelope with snake_case row fields for references and search", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		_, err := addModules(ctx, database, writeReferencesModule(), false)
		Expect(err).To(Succeed())
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).To(Succeed())
		post := func(expression string) map[string]any {
			GinkgoHelper()
			body, err := json.Marshal(map[string]any{"args": []string{expression}, "root": referencesRoot})
			Expect(err).To(Succeed())
			request := httptest.NewRequest(http.MethodPost, "/api/v1/modules/query", strings.NewReader(string(body)))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
			Expect(response.Header().Get("X-Total-Count")).To(Equal("1"))
			var envelope map[string]any
			Expect(json.Unmarshal(response.Body.Bytes(), &envelope)).To(Succeed())
			return envelope
		}

		references := post(`references of node where type = "Store" and method = "Save"`)
		Expect(references).To(HaveKeyWithValue("operation", "references"))
		Expect(references).To(HaveKeyWithValue("total", 1.0))
		Expect(references).To(HaveKey("stages"))
		reference := references["matches"].([]any)[0].(map[string]any)
		Expect(reference).To(HaveKeyWithValue("path", "app/app.go"))
		Expect(reference).To(HaveKeyWithValue("source", "app/app.go:5:28"))
		Expect([]any{reference["line"], reference["column"], reference["end_line"], reference["end_column"]}).To(Equal([]any{5.0, 28.0, 5.0, 32.0}))
		Expect(reference).To(HaveKeyWithValue("role", "call"))
		Expect(reference).To(HaveKeyWithValue("coverage", "indexed"))
		Expect(reference).To(HaveKey("symbol_id"))
		Expect(reference).To(HaveKey("enclosing_id"))
		Expect(reference).To(HaveKey("enclosing_key"))
		Expect(reference).ToNot(HaveKey("dispatch"), "dispatch is omitted unless the caller is reached through an interface")
		declaration := references["declarations"].([]any)[0].(map[string]any)
		Expect(declaration).To(HaveKeyWithValue("kind", "definition"))
		Expect(declaration).To(HaveKeyWithValue("path", "store/store.go"))
		symbol := references["symbols"].([]any)[0].(map[string]any)
		Expect(symbol).To(HaveKeyWithValue("id", reference["symbol_id"]))
		Expect(symbol).To(HaveKeyWithValue("owner", "Store"))
		Expect(symbol).To(HaveKeyWithValue("name", "Save"))
		Expect(symbol).To(HaveKeyWithValue("visibility", "exported"))
		Expect(references["coverage"]).To(ConsistOf(And(
			HaveKeyWithValue("package_path", referencesRoot+"/broken"), HaveKeyWithValue("coverage", "partial"),
		)))

		search := post(`search "Store.Sa"`)
		Expect(search).To(HaveKeyWithValue("operation", "search"))
		Expect(search["matches"]).To(ConsistOf(And(HaveKeyWithValue("kind", "symbol"), HaveKeyWithValue("path", "store/store.go"))))
		Expect(search).To(HaveKeyWithValue("declarations", BeEmpty()))
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
		tasks := httptest.NewRecorder()
		handler.ServeHTTP(tasks, httptest.NewRequest(http.MethodGet, "/api/v1/tasks?kind=module-index", nil))
		Expect(tasks.Code).To(Equal(http.StatusOK), tasks.Body.String())
		var runs []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Kind   string `json:"kind"`
			Status string `json:"status"`
		}
		Expect(json.Unmarshal(tasks.Body.Bytes(), &runs)).To(Succeed())
		Expect(runs).To(ContainElement(And(HaveField("Name", ContainSubstring("Reindex "+workspace)), HaveField("Kind", "module-index"), HaveField("Status", "success"))))
		detail := httptest.NewRecorder()
		handler.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+runs[0].ID, nil))
		Expect(detail.Code).To(Equal(http.StatusOK), detail.Body.String())
		Expect(detail.Body.String()).To(ContainSubstring("root=example.org/browser"))
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
