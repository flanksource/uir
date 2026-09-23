package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("snapshot browser entities", func() {
	It("scopes projects, roots, sources, and nodes to the saved snapshot", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		seedQueryProject(ctx, database)
		requestCtx := withDatabase(ctx, database)
		snapshots, err := listSnapshots(requestCtx, browseOptions{Project: "acme", Limit: 100})
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshots.Data).To(HaveLen(1))
		Expect(snapshots.Data[0].Head).To(BeTrue())
		selected := snapshots.Data[0].ID

		roots, err := listRoots(requestCtx, browseOptions{Snapshot: selected, Limit: 100})
		Expect(err).ToNot(HaveOccurred())
		Expect(roots.Data).To(HaveLen(1))
		Expect(roots.Data[0].Name).To(Equal("app"))

		sources, err := listSources(requestCtx, browseOptions{Snapshot: selected, Limit: 100})
		Expect(err).ToNot(HaveOccurred())
		Expect(sources.Data).To(HaveLen(1))
		Expect(sources.Data[0].Path).To(Equal("worker.go"))

		nodes, err := listNodes(requestCtx, browseOptions{Snapshot: selected, Limit: 100})
		Expect(err).ToNot(HaveOccurred())
		Expect(nodes.Data).To(HaveLen(1))
		Expect(nodes.Data[0].Symbol).To(Equal("method:acme.Worker:Run"))
		Expect(nodes.Data[0].SnapshotID).To(Equal(selected))
	})

	It("registers browse routes through Clicky", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		seedQueryProject(ctx, database)
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).ToNot(HaveOccurred())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/snapshot?project=acme&format=json", nil))
		Expect(response.Code).To(Equal(http.StatusOK))
		var payload struct {
			Data []browseRow `json:"data"`
		}
		Expect(json.Unmarshal(response.Body.Bytes(), &payload)).To(Succeed())
		Expect(payload.Data).To(HaveLen(1))
		Expect(payload.Data[0].ProjectKey).To(Equal("acme"))
	})
})
