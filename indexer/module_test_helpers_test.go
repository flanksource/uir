package indexer

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func openIndexerSQLite(ctx context.Context) *gorm.DB {
	GinkgoHelper()
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "index.db")})
	Expect(err).To(Succeed())
	DeferCleanup(closeIndexerDB, database)
	return database
}

func openIndexerPostgres(ctx context.Context) *gorm.DB {
	GinkgoHelper()
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: dbtest.ForGinkgo(dbtest.Options{Name: "uir_indexer"}).DSN(), Schema: "uir_indexer"})
	Expect(err).To(Succeed())
	DeferCleanup(closeIndexerDB, database)
	return database
}

func closeIndexerDB(database *gorm.DB) {
	GinkgoHelper()
	sqlDB, err := database.DB()
	Expect(err).To(Succeed())
	Expect(sqlDB.Close()).To(Succeed())
}

func writeFile(path, content string) {
	GinkgoHelper()
	Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
	modified := time.Now().Add(time.Second)
	Expect(os.Chtimes(path, modified, modified)).To(Succeed())
}
