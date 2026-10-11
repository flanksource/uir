package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("historical module import API", func() {
	It("imports a historical-only root, reuses it and exposes its snapshot without a head", func(ctx context.Context) {
		runtime, handler := servedRuntime(ctx)
		body, err := os.ReadFile(filepath.Join(importExamples, "co-prod.json"))
		Expect(err).ToNot(HaveOccurred())
		var publication indexer.Publication
		Expect(json.Unmarshal(body, &publication)).To(Succeed())
		publication.Options = indexer.PublishOptions{Historical: true, GitCommit: strings.Repeat("a", 40), Producer: "api-test-v1"}
		encoded, err := json.Marshal(map[string]any{"publication": publication})
		Expect(err).ToNot(HaveOccurred())
		first := postJSON(ctx, handler, "/api/v1/modules/import", string(encoded))
		Expect(first.Code).To(Equal(http.StatusOK), first.Body.String())
		var result indexer.ModuleResult
		Expect(json.Unmarshal(first.Body.Bytes(), &result)).To(Succeed())
		Expect(result.HeadVersion).To(BeZero())
		again := postJSON(ctx, handler, "/api/v1/modules/import", string(encoded))
		Expect(again.Code).To(Equal(http.StatusOK), again.Body.String())
		var reused indexer.ModuleResult
		Expect(json.Unmarshal(again.Body.Bytes(), &reused)).To(Succeed())
		Expect(reused.SnapshotID).To(Equal(result.SnapshotID))
		Expect(reused.Unchanged).To(BeTrue())
		rows, total, err := storage.ModuleSnapshots(ctx, runtime.database, storage.ModuleSnapshotListOptions{RootKey: publication.RootKey, Location: publication.Location.URI})
		Expect(err).ToNot(HaveOccurred())
		Expect(total).To(Equal(int64(1)))
		Expect(rows[0].Kind).To(Equal(storage.SnapshotKindHistorical))
	})
})
