package sql_test

import (
	"github.com/flanksource/sqlglot-go/sqlglot/access"
	"github.com/flanksource/uir/sql"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("migration access analysis", func() {
	DescribeTable("finds table writes and function calls at their migration sites", func(dialect, source, table, function string) {
		analysis, err := sql.AnalyzeMigration(source, dialect, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(analysis.Issues).To(BeEmpty())
		var operations []string
		for _, operation := range analysis.Accesses {
			if operation.Kind == access.Write || operation.Kind == access.Call {
				operations = append(operations, string(operation.Kind)+" "+operation.Object.String())
			}
		}
		Expect(operations).To(ConsistOf("write "+table, "call "+function))
	},
		Entry("T-SQL literal dynamic SQL", "tsql", "EXEC(N'INSERT INTO finance.ledger VALUES (dbo.mask_pan(1));');", "finance.ledger", "dbo.mask_pan"),
		Entry("PostgreSQL DO block", "postgres", "DO $$ DECLARE x integer; BEGIN x := public.mask_pan(1); INSERT INTO finance.ledger VALUES (x); END $$;", "finance.ledger", "public.mask_pan"),
		Entry("SQLite trigger body", "sqlite", "CREATE TRIGGER audit AFTER INSERT ON input BEGIN INSERT INTO ledger VALUES (mask_pan(NEW.id)); END;", "main.ledger", "main.mask_pan"))

	DescribeTable("records function definition changes as writes", func(dialect, source, qualified string) {
		analysis, err := sql.AnalyzeMigration(source, dialect, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(analysis.Issues).To(BeEmpty())
		Expect(analysis.Accesses).To(ContainElement(And(
			HaveField("Kind", access.Write), HaveField("ObjectKind", "function"),
			WithTransform(func(operation access.Access) string { return operation.Object.String() }, Equal(qualified)),
		)))
	},
		Entry("SQL Server create or alter", "tsql", "CREATE OR ALTER FUNCTION dbo.mask_pan() RETURNS int AS BEGIN RETURN 0; END;", "dbo.mask_pan"),
		Entry("SQL Server drop", "tsql", "DROP FUNCTION IF EXISTS dbo.mask_pan;", "dbo.mask_pan"),
		Entry("PostgreSQL create", "postgres", "CREATE FUNCTION public.mask_pan() RETURNS integer AS $$SELECT 0;$$ LANGUAGE SQL;", "public.mask_pan"),
		Entry("PostgreSQL drop", "postgres", "DROP FUNCTION IF EXISTS public.mask_pan(integer);", "public.mask_pan"))

	It("reports opaque dynamic SQL while retaining accesses from later statements", func() {
		analysis, err := sql.AnalyzeMigration("EXEC(@sql);\nGO\nDELETE FROM dbo.ledger;", "tsql", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(analysis.Issues).To(HaveLen(1))
		Expect(analysis.Issues[0].Rule).To(Equal("sql-analysis-incomplete"))
		Expect(analysis.Accesses).To(HaveLen(1))
		Expect(analysis.Accesses[0].Object.String()).To(Equal("dbo.ledger"))
	})
})
