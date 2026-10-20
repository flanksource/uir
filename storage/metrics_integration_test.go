package storage_test

import (
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Metric storage", func() {
	DescribeTable("enforces metric constraints and cascades source and document projections", func(ctx SpecContext, options func() storage.DBOptions) {
		database := openDB(ctx, options())
		root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/metrics", Name: "metrics", CreatedAt: time.Now(), Ordinal: 1}
		Expect(database.Create(&root).Error).To(Succeed())
		revision := sourceRevision(root, "metrics.go", "package metrics\n")
		Expect(database.Create(&revision).Error).To(Succeed())
		document := syntaxDocument(revision, digest("metric input"))
		Expect(database.Create(&document).Error).To(Succeed())
		source := storage.SourceMetrics{SourceRevisionID: revision.ID, Version: storage.MetricsVersion, Lines: 1}
		Expect(database.Create(&source).Error).To(Succeed())
		projection := storage.DeclarationMetrics{DocumentOrdinal: document.Ordinal, SymbolKey: "declaration", Version: storage.MetricsVersion,
			NameLine: 1, NameColumn: 1, NameEndLine: 1, NameEndColumn: 2}
		Expect(database.Create(&projection).Error).To(Succeed())
		invalid := projection
		invalid.SymbolKey = "invalid"
		negative := -1
		invalid.ParamCount = &negative
		Expect(database.Create(&invalid).Error).To(HaveOccurred())
		Expect(database.Delete(&document).Error).To(Succeed())
		var count int64
		Expect(database.Model(&storage.DeclarationMetrics{}).Count(&count).Error).To(Succeed())
		Expect(count).To(BeZero())
		Expect(database.Delete(&revision).Error).To(Succeed())
		Expect(database.Model(&storage.SourceMetrics{}).Count(&count).Error).To(Succeed())
		Expect(count).To(BeZero())
	}, Entry("SQLite", sqliteOptions("metrics.db")), Entry("PostgreSQL", postgresOptions("uir_metrics")))
})
