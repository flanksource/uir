import { afterEach, expect, it, vi } from "vitest";
import { getQueryCatalog, runModuleQuery } from "../../../web/src/api";

afterEach(() => vi.unstubAllGlobals());

it("preserves intentional syntax coverage and declaration metrics from analysis snapshots", async () => {
  const result = { operation: "set", total: 1, declarations: [], symbols: [], stages: [],
    matches: [{ path: "work.go", line: 2, snapshot_id: "syntax-snapshot", coverage: "syntax" }],
    coverage: [{ root_key: "example.org/app", location: "/workspace/app", snapshot_id: "syntax-snapshot", package_path: "example.org/app", coverage: "syntax", analysis_mode: "syntax", diagnostics: 0 }],
    columns: [{ name: "params", field: "symbol.metrics.param_count", type: "integer" }], rows: [{ params: 4 }] };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(result), { headers: { "Server-Timing": "total;dur=1" } })));
  await expect(runModuleQuery("SELECT symbol.metrics.param_count AS params FROM func:*", "example.org/app", "syntax-snapshot", { language: "sql" })).resolves.toMatchObject({ data: result });
});

it("preserves file function and struct counts for consolidation queries", async () => {
  const sql = "SELECT path, file.functions, file.structs FROM files() WHERE file.lines < 150";
  const result = { operation: "set", total: 2, declarations: [], symbols: [], coverage: [], stages: [],
    matches: [{ path: "record.go", source_id: "source-record", snapshot_id: "snapshot-small" }],
    columns: [{ name: "file.functions", field: "file.functions", type: "integer" }, { name: "file.structs", field: "file.structs", type: "integer" }],
    rows: [{ path: "record.go", "file.functions": 0, "file.structs": 1 }, { path: "run.go", "file.functions": 1, "file.structs": 0 }] };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(result), { headers: { "Server-Timing": "total;dur=1" } })));
  await expect(runModuleQuery(sql, "example.org/small", "snapshot-small", { language: "sql" })).resolves.toMatchObject({ data: result });
});

it("preserves build constraints and non-Go NULLs in SQL responses", async () => {
  const sql = "SELECT file.build_constraint FROM files(include_excluded => TRUE)";
  const result = { operation: "set", total: 3, declarations: [], symbols: [], matches: [{ path: "hack/tool.go", coverage: "excluded", source_id: "source-build", snapshot_id: "snapshot-build" }], coverage: [], stages: [],
    columns: [{ name: "file.build_constraint", field: "file.build_constraint", type: "string" }],
    rows: [{ "file.build_constraint": "hack && linux" }, { "file.build_constraint": "" }, { "file.build_constraint": null }] };
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(result), { headers: { "Server-Timing": "total;dur=1" } }));
  vi.stubGlobal("fetch", fetcher);
  await expect(runModuleQuery(sql, "example.org/build-constraints", "snapshot-build", { language: "sql" })).resolves.toMatchObject({ data: result });
  expect(fetcher).toHaveBeenCalledWith("/api/v1/modules/query", expect.objectContaining({ body: JSON.stringify({ args: [sql], root: "example.org/build-constraints", snapshot: "snapshot-build", language: "sql" }) }));
});

it("preserves canonical variable types and NULLs in SQL responses", async () => {
  const sql = "SELECT symbol.type FROM var:*";
  const result = { operation: "set", total: 2, declarations: [], symbols: [], matches: [{ path: "state.go", line: 11 }], coverage: [], stages: [],
    columns: [{ name: "symbol.type", field: "symbol.type", type: "string" }], rows: [{ "symbol.type": "*gorm.io/gorm.DB" }, { "symbol.type": null }] };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(result), { headers: { "Server-Timing": "total;dur=1" } })));
  await expect(runModuleQuery(sql, "example.org/state", "snapshot-types", { language: "sql" })).resolves.toMatchObject({ data: result });
});

it("preserves integer metrics, bodyless NULLs, totals and file witnesses", async () => {
  const sql = "SELECT symbol.metrics.param_count, symbol.metrics.body_lines FROM func:*";
  const result = { operation: "set", total: 2, declarations: [], symbols: [], coverage: [], stages: [], matches: [{ kind: "symbol", path: "reader.go", source_id: "source-1", snapshot_id: "snapshot-1", line: 4 }],
    columns: [{ name: "symbol.metrics.param_count", field: "symbol.metrics.param_count", type: "integer" }, { name: "symbol.metrics.body_lines", field: "symbol.metrics.body_lines", type: "integer" }],
    rows: [{ "symbol.metrics.param_count": 2, "symbol.metrics.body_lines": null }] };
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(result), { headers: { "Server-Timing": "total;dur=1" } }));
  vi.stubGlobal("fetch", fetcher);
  await expect(runModuleQuery(sql, "example.org/metrics", "snapshot-1", { language: "sql" })).resolves.toMatchObject({ data: result });
  expect(fetcher).toHaveBeenCalledWith("/api/v1/modules/query", expect.objectContaining({ body: JSON.stringify({ args: [sql], root: "example.org/metrics", snapshot: "snapshot-1", language: "sql" }) }));
});

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
