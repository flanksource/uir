package storage_test

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Source revision Git path migration", func() {
	It("upgrades a populated four-column key without losing revisions or dependent metrics", func(ctx SpecContext) {
		options := storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "upgrade.db")}
		database, err := storage.UirDB(ctx, options)
		Expect(err).NotTo(HaveOccurred())
		connection, err := database.DB()
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(connection.Close)
		root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/upgrade", Name: "upgrade", Ordinal: 1, CreatedAt: time.Now().UTC()}
		Expect(database.Create(&root).Error).To(Succeed())
		revision := storage.SourceRevision{ID: uuid.New(), RootID: root.ID, PathKey: "main.go", ContentHash: strings.Repeat("a", 64), PackagePath: root.RootKey, SizeBytes: 12}
		Expect(database.Create(&revision).Error).To(Succeed())
		metrics := storage.SourceMetrics{SourceRevisionID: revision.ID, Version: storage.MetricsVersion, Lines: 2}
		Expect(database.Create(&metrics).Error).To(Succeed())
		const key = "source_revisions_root_id_path_key_content_hash_package_path_key"
		Expect(database.Exec(`DROP INDEX "` + key + `"`).Error).To(Succeed())
		Expect(database.Exec(`ALTER TABLE source_revisions DROP COLUMN git_path`).Error).To(Succeed())
		Expect(database.Exec(`CREATE UNIQUE INDEX "` + key + `" ON source_revisions(root_id, path_key, content_hash, package_path)`).Error).To(Succeed())
		Expect(connection.Close()).To(Succeed())
		upgraded := openDB(ctx, options)
		var stored storage.SourceRevision
		Expect(upgraded.Where("id = ?", revision.ID).Take(&stored).Error).To(Succeed())
		Expect(stored).To(Equal(revision))
		var dependent storage.SourceMetrics
		Expect(upgraded.Where("source_revision_id = ?", revision.ID).Take(&dependent).Error).To(Succeed())
		Expect(dependent).To(Equal(metrics))
		mapped := revision
		mapped.ID, mapped.GitPath = uuid.New(), "mapped/main.go"
		Expect(upgraded.Create(&mapped).Error).To(Succeed())
		mapped.ID = uuid.New()
		Expect(upgraded.Create(&mapped).Error).To(MatchError(ContainSubstring("UNIQUE constraint failed")))
	})
})
