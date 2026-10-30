-- Order functions of the demo shop, as PostgreSQL stores them.
CREATE OR REPLACE FUNCTION public.submit_order(p_order_id integer, p_customer_id integer)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
  v_status text := 'submitted';
BEGIN
  IF EXISTS (SELECT 1 FROM public.customers WHERE customer_id = p_customer_id) THEN
    PERFORM public.validate_order(p_order_id);
    INSERT INTO public.orders (order_id, customer_id, status)
    VALUES (p_order_id, p_customer_id, v_status);
  ELSE
    RAISE NOTICE 'unknown customer %', p_customer_id;
  END IF;
  WHILE EXISTS (SELECT 1 FROM order_lines WHERE order_id = p_order_id AND NOT reserved) LOOP
    UPDATE order_lines SET reserved = true WHERE order_id = p_order_id;
  END LOOP;
END;
$$;

CREATE FUNCTION validate_order(p_order_id integer) RETURNS boolean AS $body$
  SELECT order_total(o.order_id) > 0
  FROM orders o
  JOIN customers c ON c.customer_id = o.customer_id
  WHERE o.order_id = p_order_id;
$body$ LANGUAGE sql STABLE;

CREATE FUNCTION order_total(p_order_id integer) RETURNS numeric
LANGUAGE sql STABLE
AS $$ SELECT sum(amount) FROM order_lines WHERE order_id = p_order_id $$;

CREATE FUNCTION ping() RETURNS integer AS $$ SELECT 1 $$ LANGUAGE sql;
