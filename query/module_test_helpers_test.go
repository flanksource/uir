package query_test

import (
	"context"
	"os"
	"path/filepath"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	entitiesModule = "example.org/entities"
	entitiesModel  = "package model\n" +
		"type Plan struct{ PlanField1 int; Status int }\n" +
		"type Policy struct{ PlanField1 int }\n" +
		"func Touch(p *Plan) { p.PlanField1 = 1 }\n" +
		"func Read(q *Policy) int { return q.PlanField1 }\n"
)

// entitiesCheckout writes a module whose Plan and Policy records both declare a PlanField1 field, so
// an Entity:Field reference has an owner to tell them apart by.
func entitiesCheckout() string {
	GinkgoHelper()
	checkout := GinkgoT().TempDir()
	Expect(os.MkdirAll(filepath.Join(checkout, "model"), 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+entitiesModule+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "model", "model.go"), []byte(entitiesModel), 0o644)).To(Succeed())
	return checkout
}

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
