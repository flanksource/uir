package sql_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
)

var _ = Describe("LowerFile PostgreSQL", func() {
	call, read, write := uir.RelationshipTypeCall, uir.RelationshipTypeRead, uir.RelationshipTypeWrite

	It("lowers every function of the fixture, plpgsql and sql bodies alike", func() {
		file := lowerFixture("postgres_orders.sql", "postgres")

		Expect(file.Diagnostics).To(BeEmpty())
		const (
			customerExists = "EXISTS (SELECT 1 FROM public.customers WHERE customer_id = p_customer_id)"
			linesToReserve = "EXISTS (SELECT 1 FROM order_lines WHERE order_id = p_order_id AND NOT reserved)"
		)
		submit, validate, total, ping := "public:submit_order", "public:validate_order", "public:order_total", "public:ping"
		Expect(routineViews(file)).To(Equal([]routineView{
			{ID: submit, Line: 2, Params: []string{"p_order_id INT", "p_customer_id INT"}, Edges: []edge{
				{Type: read, From: submit, To: "public.customers", Line: 9, Guards: []string{}},
				{Type: call, From: submit, To: validate, Line: 10, Guards: []string{customerExists}},
				{Type: write, From: submit, To: "public.orders", Line: 11, Guards: []string{customerExists}},
				{Type: read, From: submit, To: "public.order_lines", Line: 16, Guards: []string{}},
				{Type: write, From: submit, To: "public.order_lines", Line: 17, Guards: []string{linesToReserve}},
			}},
			{ID: validate, Line: 22, Params: []string{"p_order_id INT"}, Edges: []edge{
				{Type: read, From: validate, To: "public.orders", Line: 23, Guards: []string{}},
				{Type: read, From: validate, To: "public.customers", Line: 23, Guards: []string{}},
				{Type: call, From: validate, To: total, Line: 23, Guards: []string{}},
			}},
			{ID: total, Line: 29, Params: []string{"p_order_id INT"}, Edges: []edge{
				{Type: read, From: total, To: "public.order_lines", Line: 31, Guards: []string{}},
			}},
			{ID: ping, Line: 33, Params: []string{}, Edges: []edge{}},
		}))
	})

	It("lowers plpgsql control flow, RETURN QUERY, CALL and SELECT INTO a variable", func() {
		routine, diagnostics := lowerOne(`CREATE FUNCTION billing.close_month(p_month date) RETURNS TABLE (id integer)
LANGUAGE plpgsql AS $fn$
DECLARE
  v_total numeric;
BEGIN
  SELECT sum(amount) INTO v_total FROM invoices WHERE month = p_month;
  IF v_total IS NULL THEN
    RETURN;
  ELSIF v_total > 1000 THEN
    CALL billing.flag_large(p_month);
  ELSE
    v_total := 0;
  END IF;
  <<months>>
  FOR r IN SELECT id FROM archive.months LOOP
    PERFORM billing.touch(r.id);
    EXIT months WHEN r.id > 10;
  END LOOP;
  RETURN QUERY SELECT * FROM invoices WHERE month = p_month;
EXCEPTION
  WHEN others THEN
    INSERT INTO billing.errors (month) VALUES (p_month);
END
$fn$`, "postgres")

		Expect(diagnostics).To(BeEmpty())
		closeMonth := "billing:close_month"
		Expect(edges(routine)).To(Equal([]edge{
			{Type: read, From: closeMonth, To: "public.invoices", Line: 6, Guards: []string{}},
			{Type: call, From: closeMonth, To: "billing:flag_large", Line: 10, Guards: []string{"!v_total IS NULL", "v_total > 1000"}},
			{Type: read, From: closeMonth, To: "archive.months", Line: 15, Guards: []string{}},
			{Type: call, From: closeMonth, To: "billing:touch", Line: 16, Guards: []string{}},
			{Type: read, From: closeMonth, To: "public.invoices", Line: 19, Guards: []string{}},
			{Type: write, From: closeMonth, To: "billing.errors", Line: 22, Guards: []string{}},
		}))
	})

	It("records a statement it cannot lower as a diagnostic and lowers the others", func() {
		routine, diagnostics := lowerOne(`CREATE FUNCTION refresh() RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  EXECUTE format('TRUNCATE %I', 'cache');
  DELETE FROM cache_log;
END $$`, "postgres")

		Expect(targets(routine)).To(Equal([]string{"write public.cache_log"}))
		Expect(diagnostics).To(HaveLen(1))
		Expect(diagnostics[0].Line).To(Equal(3))
		Expect(diagnostics[0].Statement).To(Equal("EXECUTE format('TRUNCATE %I', 'cache')"))
	})

	It("records a routine whose definition does not parse as a diagnostic", func() {
		file, err := lowerPostgres(`CREATE FUNCTION broken( RETURNS void AS $$ SELECT 1 $$ LANGUAGE sql;
CREATE FUNCTION fine() RETURNS void AS $$ DELETE FROM jobs $$ LANGUAGE sql;`)

		Expect(err).NotTo(HaveOccurred())
		Expect(routineViews(file)).To(HaveLen(1))
		Expect(targets(file.Routines[0])).To(Equal([]string{"write public.jobs"}))
		Expect(file.Diagnostics).To(HaveLen(1))
		Expect(file.Diagnostics[0].Line).To(Equal(1))
	})
})
