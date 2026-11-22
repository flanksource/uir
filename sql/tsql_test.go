package sql_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	sqlsource "github.com/flanksource/uir/sql"
)

var _ = Describe("LowerFile T-SQL", func() {
	const (
		customerExists = "EXISTS (SELECT 1 FROM dbo.Customers WHERE CustomerId = @CustomerId)"
		linesToReserve = "EXISTS (SELECT 1 FROM dbo.OrderLines WHERE OrderId = @OrderId AND Reserved = 0)"
	)
	call, read, write := uir.RelationshipTypeCall, uir.RelationshipTypeRead, uir.RelationshipTypeWrite

	It("lowers every routine of the fixture with its calls, reads and writes, guarded by IF and WHILE", func() {
		file := lowerFixture("tsql_orders.sql", "tsql")

		Expect(file.Diagnostics).To(BeEmpty())
		submit, validate, reject, total, ping := "dbo:SubmitOrder", "dbo:ValidateOrder", "dbo:RejectOrder", "dbo:OrderTotal", "dbo:Ping"
		Expect(routineViews(file)).To(Equal([]routineView{
			{ID: submit, Line: 2, Params: []string{"@OrderId INTEGER", "@CustomerId INTEGER"}, Edges: []edge{
				{Type: read, From: submit, To: "dbo.Customers", Line: 8, Guards: []string{}},
				{Type: call, From: submit, To: validate, Line: 10, Guards: []string{customerExists}},
				{Type: write, From: submit, To: "dbo.Orders", Line: 11, Guards: []string{customerExists}},
				{Type: call, From: submit, To: reject, Line: 15, Guards: []string{"!" + customerExists}},
				{Type: read, From: submit, To: "dbo.OrderLines", Line: 16, Guards: []string{}},
				{Type: write, From: submit, To: "dbo.OrderLines", Line: 17, Guards: []string{linesToReserve}},
			}},
			{ID: validate, Line: 21, Params: []string{"@OrderId INTEGER"}, Edges: []edge{
				{Type: read, From: validate, To: "dbo.Orders", Line: 23, Guards: []string{}},
				{Type: read, From: validate, To: "dbo.Customers", Line: 23, Guards: []string{}},
				{Type: call, From: validate, To: total, Line: 23, Guards: []string{}},
			}},
			{ID: reject, Line: 29, Params: []string{"@OrderId INTEGER"}, Edges: []edge{
				{Type: write, From: reject, To: "dbo.RejectedOrders", Line: 31, Guards: []string{}},
				{Type: read, From: reject, To: "dbo.Orders", Line: 31, Guards: []string{}},
			}},
			{ID: total, Line: 35, Params: []string{"@OrderId INTEGER"}, Edges: []edge{
				{Type: read, From: total, To: "dbo.OrderLines", Line: 38, Guards: []string{}},
			}},
			{ID: ping, Line: 42, Params: []string{}, Edges: []edge{}},
		}))
	})

	It("gives each statement the file, its start line and its text", func() {
		file := lowerFixture("tsql_orders.sql", "tsql")

		rels := uir.CollectRelationships(file.Routines[0])
		Expect(rels[2].Location.Path).To(Equal("../fixtures/sql/tsql_orders.sql"))
		Expect(*rels[2].Content).To(Equal("INSERT INTO dbo.Orders (OrderId, CustomerId, Status)\n        VALUES (@OrderId, @CustomerId, 'submitted')"))
	})

	It("lowers an anonymous T-SQL block as one file script with guarded table access", func() {
		source := `BEGIN
DECLARE @RecordCount INT
SELECT @RecordCount = COUNT(*) FROM dbo.Code
IF @RecordCount = 0
BEGIN
INSERT INTO dbo.Code (Value) VALUES ('new')
END
ELSE
BEGIN
PRINT 'already exists'
END
END`
		file, err := sqlsource.LowerFile("test.sql", []byte(source), "tsql")

		Expect(err).NotTo(HaveOccurred())
		Expect(file.Diagnostics).To(BeEmpty())
		Expect(file.Routines).To(HaveLen(1))
		Expect([]any{file.Routines[0].Method, file.Routines[0].Properties["routine_type"], *file.Routines[0].StartLine}).
			To(Equal([]any{"test.sql", "script", 1}))
		Expect(targets(file.Routines[0])).To(Equal([]string{"read dbo.Code", "write dbo.Code"}))
		Expect(guardText(uir.CollectRelationships(file.Routines[0])[1].Guards)).To(Equal([]string{"@RecordCount = 0"}))
	})

	It("reports an unsupported anonymous statement and continues with the next one", func() {
		file, err := sqlsource.LowerFile("test.sql", []byte("BULK INSERT dbo.Code FROM 'code.csv';\nSELECT * FROM dbo.Code"), "tsql")

		Expect(err).NotTo(HaveOccurred())
		Expect(file.Routines).To(HaveLen(1))
		Expect(targets(file.Routines[0])).To(Equal([]string{"read dbo.Code"}))
		Expect(file.Diagnostics).To(HaveLen(1))
		Expect([]any{file.Diagnostics[0].Line, file.Diagnostics[0].Statement}).To(Equal([]any{
			1, "BULK INSERT dbo.Code FROM 'code.csv'",
		}))
		Expect(file.Diagnostics[0].Message).To(ContainSubstring("unsupported"))
	})

	It("keeps setup batches outside a named procedure out of the graph", func() {
		file, err := sqlsource.LowerFile("test.sql", []byte("SET ANSI_NULLS ON\nGO\nCREATE PROCEDURE dbo.Load AS SELECT * FROM dbo.Code"), "tsql")

		Expect(err).NotTo(HaveOccurred())
		Expect(file.Diagnostics).To(BeEmpty())
		Expect(file.Routines).To(HaveLen(1))
		Expect(file.Routines[0].Method).To(Equal("Load"))
		Expect(targets(file.Routines[0])).To(Equal([]string{"read dbo.Code"}))
	})

	It("keeps data access outside a named procedure as a separate script root", func() {
		file, err := sqlsource.LowerFile("test.sql", []byte("INSERT INTO dbo.Code (Value) VALUES ('new')\nGO\nCREATE PROCEDURE dbo.Load AS SELECT * FROM dbo.Code"), "tsql")

		Expect(err).NotTo(HaveOccurred())
		Expect(file.Diagnostics).To(BeEmpty())
		Expect(file.Routines).To(HaveLen(2))
		Expect([]string{file.Routines[0].Method, file.Routines[1].Method}).To(Equal([]string{"Load", "test.sql"}))
		Expect(targets(file.Routines[0])).To(Equal([]string{"read dbo.Code"}))
		Expect(targets(file.Routines[1])).To(Equal([]string{"write dbo.Code"}))
	})

	DescribeTable("reads only real tables, as their schema-qualified names",
		func(body string, expected []string) {
			routine, diagnostics := lowerOne("CREATE PROCEDURE dbo.P @Id int AS\n"+body, "tsql")
			Expect(diagnostics).To(BeEmpty())
			Expect(targets(routine)).To(Equal(expected))
		},
		Entry("an unqualified table is in dbo", "SELECT * FROM Orders", []string{"read dbo.Orders"}),
		Entry("a CTE is no table", "WITH o AS (SELECT * FROM sales.Orders) SELECT * FROM o", []string{"read sales.Orders"}),
		Entry("temp tables and table variables are local",
			"DECLARE @t TABLE (id int)\nINSERT INTO #tmp SELECT id FROM @t\nSELECT * FROM #tmp", []string{}),
		Entry("UPDATE by alias writes the FROM table it names",
			"UPDATE o SET Status = 1 FROM dbo.Orders o JOIN dbo.Customers c ON c.Id = o.CustomerId",
			[]string{"write dbo.Orders", "read dbo.Customers"}),
		Entry("MERGE writes its target and reads its source",
			"MERGE INTO dbo.Orders AS t USING staging.Orders AS s ON t.Id = s.Id WHEN MATCHED THEN UPDATE SET t.Status = s.Status;",
			[]string{"write dbo.Orders", "read staging.Orders"}),
		Entry("DELETE reads the tables its subquery names",
			"DELETE FROM dbo.Orders WHERE CustomerId IN (SELECT Id FROM dbo.Customers)",
			[]string{"write dbo.Orders", "read dbo.Customers"}),
		Entry("a table-valued function in FROM is a call", "SELECT * FROM dbo.OpenOrders(@Id)", []string{"call dbo:OpenOrders"}),
		Entry("an xml method is no call", "SELECT Data.value('(/a)[1]', 'int') FROM dbo.Docs", []string{"read dbo.Docs"}),
		Entry("a bare builtin is no call", "SELECT ISNUMERIC(Code) FROM dbo.Codes", []string{"read dbo.Codes"}),
		Entry("EXEC of a three-part name keeps its database",
			"EXEC Archive.dbo.Purge @Id", []string{"call Archive.dbo:Purge"}),
		Entry("CREATE TABLE writes the table, a temp table nothing",
			"CREATE TABLE dbo.Codes (Id int REFERENCES dbo.Kinds (Id))\nCREATE TABLE #t (Id int)", []string{"write dbo.Codes", "read dbo.Kinds"}),
		Entry("ALTER, TRUNCATE and DROP write the table they name",
			"ALTER TABLE Codes ADD Kind int\nTRUNCATE TABLE dbo.Stage\nDROP TABLE IF EXISTS dbo.Old",
			[]string{"write dbo.Codes", "write dbo.Stage", "write dbo.Old"}),
		Entry("EXEC of a string literal runs the statements it holds",
			"EXEC('UPDATE dbo.Codes SET Kind = ''a'' WHERE Id IN (SELECT Id FROM dbo.Kinds)')",
			[]string{"write dbo.Codes", "read dbo.Kinds"}),
		Entry("sp_executesql of a string literal runs the statements it holds",
			"EXEC sp_executesql N'DELETE FROM dbo.Stage'", []string{"write dbo.Stage"}),
	)

	DescribeTable("reports dynamic SQL it cannot read",
		func(body string) {
			routine, diagnostics := lowerOne("CREATE PROCEDURE dbo.P @sql nvarchar(max) AS\n"+body, "tsql")
			Expect(targets(routine)).To(BeEmpty())
			Expect(diagnostics).To(HaveLen(1))
			Expect([]any{diagnostics[0].Line, diagnostics[0].Message}).To(Equal([]any{2, "unsupported SQL statement: dynamic SQL"}))
		},
		Entry("EXEC of a variable", "EXEC (@sql)"),
		Entry("sp_executesql of a variable", "EXEC sp_executesql @sql"),
	)

	It("lowers a routine an EXEC literal defines as a routine of the file", func() {
		file, err := sqlsource.LowerFile("test.sql", []byte("EXEC('CREATE OR ALTER FUNCTION dbo.Country() RETURNS varchar(2) AS\nBEGIN\n  RETURN (SELECT TOP 1 Code FROM dbo.AsEnv WHERE EnvKey = ''current'')\nEND')"), "tsql")

		Expect(err).NotTo(HaveOccurred())
		Expect(file.Diagnostics).To(BeEmpty())
		Expect(routineViews(file)).To(Equal([]routineView{
			{ID: "dbo:Country", Line: 1, Params: []string{}, Edges: []edge{
				{Type: read, From: "dbo:Country", To: "dbo.AsEnv", Line: 3, Guards: []string{}},
			}},
			{ID: ":test.sql#test.sql", Line: 1, Params: []string{}, Edges: []edge{}},
		}))
	})

	It("writes a view it creates and does not read what the view's query reads", func() {
		file, err := sqlsource.LowerFile("test.sql", []byte("CREATE VIEW dbo.OpenCodes AS SELECT * FROM dbo.Codes"), "tsql")

		Expect(err).NotTo(HaveOccurred())
		Expect(file.Diagnostics).To(BeEmpty())
		Expect(targets(file.Routines[0])).To(Equal([]string{"write dbo.OpenCodes"}))
	})

	It("reads non-breaking spaces as the whitespace SQL Server takes them for", func() {
		nbsp := "  "
		file, err := sqlsource.LowerFile("test.sql", []byte("IF @n = 0\n"+nbsp+"BEGIN\n"+nbsp+"INSERT INTO dbo.Code VALUES ('a b')\n"+nbsp+"END\nELSE\n"+nbsp+"PRINT 'present'"), "tsql")

		Expect(err).NotTo(HaveOccurred())
		Expect(file.Diagnostics).To(BeEmpty())
		Expect(targets(file.Routines[0])).To(Equal([]string{"write dbo.Code"}))
	})

	It("records a statement that does not parse as a diagnostic and lowers the others", func() {
		routine, diagnostics := lowerOne(`CREATE PROCEDURE dbo.Load AS
BEGIN
    BULK INSERT dbo.Orders FROM 'orders.csv';
    EXEC dbo.Notify;
END`, "tsql")

		Expect(targets(routine)).To(Equal([]string{"call dbo:Notify"}))
		Expect(diagnostics).To(HaveLen(1))
		Expect(diagnostics[0].Line).To(Equal(3))
		Expect(diagnostics[0].Statement).To(Equal("BULK INSERT dbo.Orders FROM 'orders.csv'"))
		Expect(diagnostics[0].Message).NotTo(BeEmpty())
	})

	It("guards an ELSE IF chain and a TRY block like the statements they hold", func() {
		routine, diagnostics := lowerOne(`CREATE PROCEDURE dbo.Route @Kind int AS
IF @Kind = 1
    EXEC dbo.One;
ELSE IF @Kind = 2
BEGIN TRY
    EXEC dbo.Two;
END TRY
BEGIN CATCH
    EXEC dbo.Failed;
END CATCH
EXEC dbo.Always;`, "tsql")

		Expect(diagnostics).To(BeEmpty())
		route := "dbo:Route"
		Expect(edges(routine)).To(Equal([]edge{
			{Type: call, From: route, To: "dbo:One", Line: 3, Guards: []string{"@Kind = 1"}},
			{Type: call, From: route, To: "dbo:Two", Line: 6, Guards: []string{"!@Kind = 1", "@Kind = 2"}},
			{Type: call, From: route, To: "dbo:Failed", Line: 9, Guards: []string{"!@Kind = 1", "@Kind = 2"}},
			{Type: call, From: route, To: "dbo:Always", Line: 11, Guards: []string{}},
		}))
	})

	It("refuses a dialect it does not lower and a file with no statements", func() {
		_, err := sqlsource.LowerFile("x.sql", []byte("CREATE PROCEDURE p AS SELECT 1"), "mysql")
		Expect(err).To(MatchError(ContainSubstring(`dialect "mysql"`)))

		_, err = sqlsource.LowerFile("x.sql", []byte("-- no SQL statements"), "tsql")
		Expect(err).To(MatchError(ContainSubstring("no routines")))
	})
})
