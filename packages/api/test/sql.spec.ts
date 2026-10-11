import { afterEach, expect, it, vi } from "vitest";
import { getQueryCatalog, runModuleQuery } from "../../../web/src/api";

afterEach(() => vi.unstubAllGlobals());

it("posts SQL with its selected language and preserves projected rows and witnesses", async () => {
  const result = { operation: "set", total: 3, matches: [{ symbol: "Orders", relationship: { kind: "http.handled_by", resolution: "static", targets: ["handler"] } }],
    declarations: [], symbols: [], coverage: [], warnings: [], stages: [], columns: [{ name: "method", field: "symbol.props.method", type: "string" }], rows: [{ method: "POST" }] };
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(result), { headers: { "Server-Timing": "total;dur=1" } }));
  vi.stubGlobal("fetch", fetcher);
  const sql = "SELECT r.method FROM http.route:* AS r LIMIT 1";
  await expect(runModuleQuery(sql, "example.org/routes", "snapshot-1", { language: "sql" })).resolves.toMatchObject({ data: result });
  expect(fetcher).toHaveBeenCalledWith("/api/v1/modules/query", expect.objectContaining({ body: JSON.stringify({ args: [sql], root: "example.org/routes", snapshot: "snapshot-1", language: "sql" }) }));
});

it("loads the function, kind and property catalog for SQL completion", async () => {
  const catalog = { functions: [], kinds: [], fields: {}, domains: { symbols: {}, relationships: {} } };
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(catalog)));
  vi.stubGlobal("fetch", fetcher);
  await expect(getQueryCatalog()).resolves.toEqual(catalog);
  expect(fetcher).toHaveBeenCalledWith("/api/v1/modules/query-catalog", expect.objectContaining({ headers: { Accept: "application/json" } }));
});
