package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	clickytask "github.com/flanksource/clicky/task"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/taskruns"
	"github.com/flanksource/uir/symboldiff"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const browserSource = "package browser\n\nfunc Run() {}\n"

// servedRuntime is a runtime serving under ctx, as `uir serve` does, and its handler.
func servedRuntime(ctx context.Context) (*commandRuntime, http.Handler) {
	GinkgoHelper()
	runtime := &commandRuntime{database: openCommandDatabase(ctx), serveContext: ctx}
	DeferCleanup(func() { Expect(runtime.Close()).To(Succeed()) })
	handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
	Expect(err).To(Succeed())
	return runtime, handler
}

func browserModule() string {
	GinkgoHelper()
	workspace, err := filepath.EvalSymlinks(GinkgoT().TempDir())
	Expect(err).To(Succeed())
	Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.org/browser\n\ngo 1.26\n"), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(workspace, "main.go"), []byte(browserSource), 0o644)).To(Succeed())
	return workspace
}

// postJSON posts body to path under ctx and returns the response.
func postJSON(ctx context.Context, handler http.Handler, path, body string) *httptest.ResponseRecorder {
	GinkgoHelper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// startedRun decodes a 202 Accepted index answer into its run id.
func startedRun(response *httptest.ResponseRecorder) string {
	GinkgoHelper()
	Expect(response.Code).To(Equal(http.StatusAccepted), response.Body.String())
	var started map[string]any
	Expect(json.Unmarshal(response.Body.Bytes(), &started)).To(Succeed())
	Expect(started).To(HaveLen(1), "a started run answers with its run id alone")
	Expect(started["run_id"]).To(BeAssignableToTypeOf(""))
	return started["run_id"].(string)
}

// finishedRun polls the task API until the run is terminal and returns its snapshots, group first.
func finishedRun(handler http.Handler, id string) []clickytask.TaskSnapshot {
	GinkgoHelper()
	var snapshots []clickytask.TaskSnapshot
	Eventually(func() string {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+id, nil))
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		Expect(json.Unmarshal(response.Body.Bytes(), &snapshots)).To(Succeed())
		return snapshots[0].Status
	}, 60*time.Second, 20*time.Millisecond).ShouldNot(BeElementOf(string(clickytask.StatusPending), string(clickytask.StatusRunning)))
	return snapshots
}

func listedRuns(handler http.Handler) []clickytask.RunMeta {
	GinkgoHelper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tasks?kind="+indexer.ModuleIndexKind, nil))
	Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
	var runs []clickytask.RunMeta
	Expect(json.Unmarshal(response.Body.Bytes(), &runs)).To(Succeed())
	return runs
}

var _ = Describe("serve task runs", func() {
	It("starts add and reindex as background runs and answers with their run ids", func(ctx SpecContext) {
		runtime, handler := servedRuntime(ctx)
		workspace := browserModule()

		added := startedRun(postJSON(ctx, handler, "/api/v1/modules/add", `{"args":["`+workspace+`"],"no-workspace-uses":true}`))
		addRun := finishedRun(handler, added)
		reindexed := startedRun(postJSON(ctx, handler, "/api/v1/modules/reindex", `{"args":["`+workspace+`"]}`))
		reindexRun := finishedRun(handler, reindexed)

		Expect(addRun[0]).To(And(HaveField("Status", string(clickytask.StatusSuccess)), HaveField("Kind", indexer.ModuleIndexKind),
			HaveField("Labels", HaveKeyWithValue("reason", "add"))))
		Expect(addRun[1]).To(And(HaveField("Name", "example.org/browser"), HaveField("Description", workspace)))
		Expect(reindexRun[0]).To(And(HaveField("Name", "Reindex "+workspace), HaveField("Status", string(clickytask.StatusSuccess))))
		Expect(reindexRun[1].Logs).To(ContainElement(HaveField("Message", ContainSubstring("root=example.org/browser"))))
		Expect(runtime.waitForDetachedRuns(time.Second)).To(Succeed())
		var head storage.ModuleSnapshot
		Expect(runtime.database.Where("task_run_id = ?", added).Take(&head).Error).To(Succeed(), "the published snapshot records the run that published it")
	})

	It("reindexes every checkout without a head as one background run", func(ctx SpecContext) {
		runtime, handler := servedRuntime(ctx)
		missing := browserModule()
		root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/browser", Name: "browser", CreatedAt: time.Now().UTC()}
		Expect(storage.CreateModuleRoot(ctx, runtime.database, &root)).To(Succeed())
		location := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: missing, Kind: "module", CreatedAt: time.Now().UTC()}
		Expect(runtime.database.Create(&location).Error).To(Succeed())
		Expect(runtime.database.Create(&storage.ModulePrimary{RootID: root.ID, LocationID: location.ID}).Error).To(Succeed())

		run := finishedRun(handler, startedRun(postJSON(ctx, handler, "/api/v1/modules/reindex", `{"all":true}`)))

		Expect(run[0].Status).To(Equal(string(clickytask.StatusSuccess)))
		Expect(run[1].Name).To(Equal(root.RootKey))
	})

	It("keeps a run going when the request that started it goes away", func(ctx SpecContext) {
		runtime, handler := servedRuntime(ctx)
		request, cancel := context.WithCancel(ctx)

		id := startedRun(postJSON(request, handler, "/api/v1/modules/add", `{"args":["`+browserModule()+`"],"no-workspace-uses":true}`))
		cancel()

		Expect(finishedRun(handler, id)[0].Status).To(Equal(string(clickytask.StatusSuccess)))
		Expect(runtime.waitForDetachedRuns(time.Second)).To(Succeed())
	})

	It("lists and opens runs another process finished, from the task run store", func(ctx SpecContext) {
		runtime, handler := servedRuntime(ctx)
		stored := "cli-" + uuid.NewString()
		snapshotID := uuid.NewString()
		Expect(taskruns.New(runtime.database).SaveRun(ctx, stored, []clickytask.TaskSnapshot{
			{ID: "Reindex /repo", Name: "Reindex /repo", Type: "group", GroupID: stored, Kind: indexer.ModuleIndexKind, Status: "success",
				StartedAt: "2026-10-01T09:00:00Z", FinishedAt: "2026-10-01T09:00:02Z", Total: 1, Completed: 1,
				Details: taskruns.RunDetails{SnapshotIDs: []string{snapshotID}}},
			{ID: "task", Name: "example.org/repo", Type: "task", GroupID: stored, Status: "success"},
		})).To(Succeed())

		Expect(listedRuns(handler)).To(ContainElement(And(HaveField("ID", stored), HaveField("Status", "success"))))
		detail := finishedRun(handler, stored)
		Expect(detail).To(HaveLen(2))
		Expect(detail[0].Details).To(Equal(map[string]any{"snapshot_ids": []any{snapshotID}}))
		control := postJSON(ctx, handler, "/api/v1/tasks/"+stored+"/control", `{"action":"stop"}`)
		Expect(control.Code).To(Equal(http.StatusConflict), control.Body.String())
	})

	It("rejects an HTTP index request outside uir serve instead of tying the run to the request", func(ctx SpecContext) {
		runtime := &commandRuntime{database: openCommandDatabase(ctx)}
		DeferCleanup(func() { Expect(runtime.Close()).To(Succeed()) })
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).To(Succeed())

		response := postJSON(ctx, handler, "/api/v1/modules/add", `{"args":["`+browserModule()+`"],"no-workspace-uses":true}`)

		Expect(response.Code).To(Equal(http.StatusInternalServerError), response.Body.String())
		Expect(listedRuns(handler)).ToNot(ContainElement(HaveField("Labels", HaveKeyWithValue("path", ContainSubstring("browser")))))
	})

	It("marks indexed commits in history and diffs from the checkout a request names", func(ctx SpecContext) {
		runtime, handler := servedRuntime(ctx)
		from, to := indexedCommits(ctx, runtime.database)
		locations, err := storage.ModuleLocations(ctx, runtime.database, diffRoot)
		Expect(err).To(Succeed())
		checkout := locations[0].CanonicalPath

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/modules/history?root="+url.QueryEscape(diffRoot)+"&location="+url.QueryEscape(checkout), nil))
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var history symboldiff.History
		Expect(json.Unmarshal(response.Body.Bytes(), &history)).To(Succeed())
		diffed := postJSON(ctx, handler, "/api/v1/modules/diff",
			`{"args":["`+from+`..`+to+`"],"root":"`+diffRoot+`","visibility":"all","auto-index":true,"location":"`+checkout+`"}`)
		elsewhere := postJSON(ctx, handler, "/api/v1/modules/diff",
			`{"args":["`+from+`..`+to+`"],"root":"`+diffRoot+`","auto-index":true,"location":"/not/registered"}`)

		Expect(history.Commits[0]).To(And(HaveField("Commit", to), HaveField("SnapshotID", locations[0].HeadSnapshotID.String()), HaveField("SnapshotReason", "add")))
		Expect(history.Commits[1]).To(And(HaveField("Commit", from), HaveField("SnapshotID", Not(BeEmpty()))))
		Expect(diffed.Code).To(Equal(http.StatusOK), diffed.Body.String())
		Expect(elsewhere.Code).ToNot(Equal(http.StatusOK))
	})
})
