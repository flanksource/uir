package query_test

import (
	"context"
	"path/filepath"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func openQueryDatabase(ctx context.Context, backend string) *gorm.DB {
	GinkgoHelper()
	options := storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "query.db")}
	if backend == "postgres" {
		options.DSN = dbtest.ForGinkgo(dbtest.Options{Name: "uir_query"}).DSN()
	}
	database, err := storage.UirDB(ctx, options)
	Expect(err).To(Succeed())
	DeferCleanup(func() {
		sqlDB, dbErr := database.DB()
		Expect(dbErr).To(Succeed())
		Expect(sqlDB.Close()).To(Succeed())
	})
	return database
}
