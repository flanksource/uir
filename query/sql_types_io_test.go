package query_test

import (
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var _ = Describe("Resolved variable type IO", func() {
	It("reads immutable facts without source blobs and reuses decoded documents", func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		root := metricCheckout()
		Expect(os.WriteFile(filepath.Join(root, "state.go"), []byte("package metrics\nvar State int\n"), 0o600)).To(Succeed())
		indexed := indexCheckout(ctx, database, root)
		Expect(os.RemoveAll(root)).To(Succeed())
		Expect(database.Exec("DELETE FROM source_blobs").Error).To(Succeed())
		observed := &metricIO{}
		pipeline, err := query.NewPipeline(database.Session(&gorm.Session{Logger: observed}))
		Expect(err).NotTo(HaveOccurred())
		for _, name := range []string{"cold", "warm"} {
			started, before := time.Now(), observed.contentReads
			result := runQuery(ctx, pipeline, "SELECT symbol.type FROM var:State", query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID})
			Expect(result.Rows).To(Equal([]map[string]any{{"symbol.type": "int"}}))
			AddReportEntry(name, map[string]any{"snapshot": indexed.SnapshotID, "duration": time.Since(started).String(), "document_reads": observed.contentReads - before, "source_reads": observed.sourceReads})
			if name == "warm" {
				Expect(observed.contentReads).To(Equal(before))
			}
		}
		Expect(observed.sourceReads).To(BeZero())
	})
})
