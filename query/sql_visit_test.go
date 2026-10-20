package query_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL row visitor", func() {
	It("rejects pagination and terminal sources before opening a database", func() {
		for _, input := range []string{"SELECT path FROM files() LIMIT 1", "SELECT * FROM graph(func:*)", "SELECT * FROM path(func:A, func:B)", "invalid"} {
			Expect(query.ValidateSQLRows(input)).To(HaveOccurred(), input)
		}
		Expect(query.ValidateSQLRows("SELECT symbol.name AS name FROM callees(func:*)")).To(Succeed())
	})
	DescribeTable("visits more than 1000 ordered rows with independent witnesses and stops on callback errors", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		checkout := metricCheckout()
		var source strings.Builder
		source.WriteString("package metrics\n")
		const count = 1003
		for i := range count {
			fmt.Fprintf(&source, "func Many%04d() {}\n", i)
		}
		Expect(os.WriteFile(filepath.Join(checkout, "visitor.go"), []byte(source.String()), 0o644)).To(Succeed())
		indexed := indexCheckout(ctx, database, checkout)
		pipeline, err := query.NewPipeline(database)
		Expect(err).NotTo(HaveOccurred())
		var rows []query.SQLRow
		scope := query.ModuleScopeOptions{SnapshotID: indexed.SnapshotID}
		result, err := pipeline.VisitSQL(ctx, "SELECT symbol.name AS name FROM func:Many* ORDER BY name DESC", scope, func(row query.SQLRow) error { rows = append(rows, row); return nil })
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Total).To(Equal(count))
		Expect(result.Rows).To(BeEmpty())
		Expect(rows).To(HaveLen(count))
		Expect(rows[0].Values).To(Equal(map[string]any{"name": "Many1002"}))
		Expect(rows[0].Match.Path).To(Equal("visitor.go"))
		Expect(rows[0].Match.Line).To(HaveValue(Equal(1004)))
		stopped := errors.New("stop visiting")
		calls := 0
		_, err = pipeline.VisitSQL(ctx, "SELECT path FROM files()", scope, func(query.SQLRow) error { calls++; return stopped })
		Expect(err).To(MatchError(stopped))
		Expect(calls).To(Equal(1))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
})
