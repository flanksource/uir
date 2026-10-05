package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

// publishExample publishes the docs example publication file at revision.
func publishExample(ctx context.Context, database *gorm.DB, file, revision string) {
	GinkgoHelper()
	encoded, err := os.ReadFile(filepath.Join(importExamples, file))
	Expect(err).ToNot(HaveOccurred())
	var publication indexer.Publication
	Expect(json.Unmarshal(encoded, &publication)).To(Succeed())
	publication.Revision = revision
	_, err = indexer.Publish(ctx, database, publication)
	Expect(err).ToNot(HaveOccurred())
}

func snapshotCount(database *gorm.DB, rootKey string) int64 {
	GinkgoHelper()
	var count int64
	Expect(database.Table("snapshots").Joins("JOIN modules ON modules.id = snapshots.root_id").Where("modules.root_key = ?", rootKey).Count(&count).Error).To(Succeed())
	return count
}

var _ = Describe("module prune", func() {
	It("prunes the named root, or every root, to its newest snapshots and refuses a keep below one", func(ctx context.Context) {
		database := openCommandDatabase(ctx)
		runtime := &commandRuntime{database: database}
		execute := func(args ...string) (string, error) {
			root := newRootCommand(runtime)
			root.SetErr(io.Discard)
			root.SetArgs(args)
			return captureStdout(func() error {
				return root.ExecuteContext(context.WithValue(ctx, runtimeContextKey{}, runtime))
			})
		}
		for _, revision := range []string{"r1", "r2", "r3"} {
			publishExample(ctx, database, "co-prod.json", revision)
			publishExample(ctx, database, "co-prod-plana.json", revision)
		}

		_, err := execute("prune", "Co/Prod", "--keep", "2", "--format", "json")
		Expect(err).ToNot(HaveOccurred())
		Expect([]int64{snapshotCount(database, "Co/Prod"), snapshotCount(database, "Co/Prod/PlanA")}).To(Equal([]int64{2, 3}), "only the named root is pruned")

		out, err := execute("prune", "--keep", "1", "--format", "json")
		Expect(err).ToNot(HaveOccurred())
		var results []storage.PruneResult
		Expect(json.Unmarshal([]byte(out), &results)).To(Succeed(), out)
		Expect(results).To(HaveLen(2))
		Expect([]any{results[0].RootKey, results[0].Snapshots, results[1].RootKey, results[1].Snapshots}).To(Equal([]any{"Co/Prod", int64(1), "Co/Prod/PlanA", int64(2)}))
		Expect([]int64{snapshotCount(database, "Co/Prod"), snapshotCount(database, "Co/Prod/PlanA")}).To(Equal([]int64{1, 1}))

		_, err = execute("prune", "--keep", "0")
		Expect(err).To(MatchError(ContainSubstring("at least the newest snapshot must be kept")))
		_, err = execute("prune", "Co/Missing", "--keep", "1")
		Expect(err).To(MatchError(ContainSubstring(`module root "Co/Missing" is not registered`)))
	})
})
