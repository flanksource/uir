import { afterEach, expect, it, vi } from "vitest";
import { ApiError, addModules, browseModule, getSystemInfo, listModuleHeads, listModuleLocations, listModuleSnapshots, readModuleSource, reindexModules, runModuleQuery, suggestModuleSymbols, suggestTypedSelectors } from "../../../web/src/api";

afterEach(() => vi.unstubAllGlobals());

it("loads every module checkout head for the explorer tree", async () => {
  const heads = [{ root_key: "example.org/service", name: "service", location: "/checkout/service", snapshot_id: "head-1", sources: [] }];
  const warnings = [{ root_key: "example.org/missing", location: "/checkout/missing", message: "registered checkout has no indexed head; run `uir reindex --all`" }];
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: heads, warnings }), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(listModuleHeads()).resolves.toEqual({ items: heads, warnings });
  expect(fetcher).toHaveBeenCalledWith("/api/v1/modules/heads", expect.objectContaining({ headers: { Accept: "application/json" } }));
});

it("loads version and database details from the running backend", async () => {
  const info = { backend_version: "v1.2.3", database_type: "SQLite", database_version: "3.50.0", database_location: "/data/uir.db", database_size_bytes: 4096 };
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(info), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(getSystemInfo()).resolves.toEqual(info);
  expect(fetcher).toHaveBeenCalledWith("/api/v1/system/info", expect.objectContaining({ headers: { Accept: "application/json" } }));
});

const snapshot = "11111111-1111-1111-1111-111111111111";
const scope = { root: "example.org/refs", location: "/checkout/refs", snapshot_id: snapshot };
const saveID = "f22a813c22bc61f6b25e5838892b700742c88875bc2ef83f0c61f9543d877ee9";
const brokenPartial = { root_key: scope.root, location: scope.location, snapshot_id: snapshot, package_path: "example.org/refs/broken", coverage: "partial" };
const saveDeclaration = {
  kind: "definition", ...scope, symbol: "method:example.org/refs/store.Store:Save#()", source: "store/store.go:5:14",
  path: "store/store.go", line: 5, column: 14, end_line: 5, end_column: 18, role: "definition", symbol_id: saveID, coverage: "indexed",
};

it("unwraps structured query errors and keeps their hint and position", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
    code: "invalid_query", message: "Invalid query syntax", hint: "Enter a symbol after &",
    context: { line: 1, column: 12 }, trace: "trace-1",
  }), { status: 400, headers: { "Content-Type": "application/json" } })));

  await expect(runModuleQuery("func:Save &", scope.root, snapshot)).rejects.toMatchObject({
    name: "ApiError", status: 400, code: "invalid_query", message: "Invalid query syntax",
    hint: "Enter a symbol after &", context: { line: 1, column: 12 }, trace: "trace-1",
  } satisfies Partial<ApiError>);
});

it("shows a server error message and hint without the HTTP 500 wrapper", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
    code: "internal_error", message: "symbol lookup failed", hint: "Check the selected snapshot", trace: "trace-2",
  }), { status: 500, headers: { "Content-Type": "application/json" } })));

  await expect(runModuleQuery("func:Save <", scope.root, snapshot)).rejects.toMatchObject({
    name: "ApiError", status: 500, code: "internal_error", message: "symbol lookup failed",
    hint: "Check the selected snapshot", trace: "trace-2",
  } satisfies Partial<ApiError>);
});

it.each([
  {
    name: "references with declarations, symbols, and a partial package",
    expression: 'store.Store.Save <',
    envelope: {
      operation: "incoming", total: 1,
      matches: [{
        kind: "reference", ...scope, symbol: "method:example.org/refs/app:Run#()", source: "app/app.go:5:28",
        path: "app/app.go", line: 5, column: 28, end_line: 5, end_column: 32, role: "call", symbol_id: saveID,
        enclosing_id: "d7e469f71d728df80c44c3f76b4408fbeeeebfc9753a23029f712fd8817ec05d",
        enclosing_key: 'v1:["method","","example.org/refs/app","","Run","","()"]', coverage: "indexed",
      }],
      declarations: [saveDeclaration],
      symbols: [{ id: saveID, module_key: scope.root, package_path: "example.org/refs/store", kind: "method", owner: "Store", name: "Save", visibility: "exported", parameter_types: [] }],
      coverage: [brokenPartial],
      warnings: [],
      stages: [{ name: "parse", value: "incoming" }, { name: "coverage", value: "incomplete: 1 package is not fully indexed (1 partial)" }],
    },
  },
  {
    name: "search rows in the same envelope",
    expression: 'store.Store.Save',
    envelope: {
      operation: "resolve", total: 1, matches: [{ ...saveDeclaration, kind: "symbol" }], declarations: [], symbols: [], coverage: [brokenPartial], warnings: [],
      stages: [{ name: "parse", value: "resolve" }],
    },
  },
  {
    name: "module scoped package selector on a binary relation",
    expression: "func:Save < pkg:example.org/refs:app/**",
    envelope: {
      operation: "incoming", total: 0, matches: [], declarations: [], symbols: [], coverage: [brokenPartial], warnings: [],
      stages: [{ name: "parse", value: "incoming" }],
    },
  },
])("posts the scoped PEG query and returns the $name", async ({ expression, envelope }) => {
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(envelope), { status: 200, headers: { "Server-Timing": "total;dur=12.5, command;dur=9.2" } }));
  vi.stubGlobal("fetch", fetcher);
  await expect(runModuleQuery(expression, scope.root, snapshot)).resolves.toEqual({ data: envelope, timing: [
    { name: "total", duration: 12.5, counters: {} }, { name: "command", duration: 9.2, counters: {} },
  ] });
  expect(fetcher).toHaveBeenCalledWith("/api/v1/modules/query", expect.objectContaining({
    method: "POST",
    body: JSON.stringify({ args: [expression], root: scope.root, snapshot }),
  }));
});

it("gets scoped canonical symbol suggestions for query completion", async () => {
  const symbols = [{ id: saveID, query_name: "example.org/refs/store.Store.Save", package_path: "example.org/refs/store", kind: "method", name: "Save" }];
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: symbols, warnings: [] }), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(suggestModuleSymbols("store.Store.S", scope.root, snapshot)).resolves.toEqual(symbols);
  expect(fetcher).toHaveBeenCalledWith(`/api/v1/modules/suggest?prefix=store.Store.S&root=example.org%2Frefs&snapshot=${snapshot}`, expect.objectContaining({ headers: { Accept: "application/json" } }));
});

it("gets module scoped relative package completions", async () => {
  const choices = ["pkg:example.org/refs:app", "pkg:example.org/refs:app/sub"];
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: choices, warnings: [] }), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(suggestTypedSelectors("pkg:example.org/refs:app", scope.root, snapshot)).resolves.toEqual(choices);
  expect(fetcher).toHaveBeenCalledWith(`/api/v1/modules/suggest-selectors?prefix=pkg%3Aexample.org%2Frefs%3Aapp&root=example.org%2Frefs&snapshot=${snapshot}`, expect.objectContaining({ headers: { Accept: "application/json" } }));
});

it("posts an unscoped PEG query across every module root", async () => {
  const expression = 'example.org/refs/store.Store.Save <';
  const warnings = [{ root_key: "example.org/missing", location: "/checkout/missing", message: "registered checkout has no indexed head; run `uir reindex --all`" }];
  const envelope = { operation: "incoming", total: 0, matches: [], declarations: [], symbols: [], coverage: [], warnings, stages: [] };
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(envelope), { status: 200, headers: { "Server-Timing": "total;dur=7" } }));
  vi.stubGlobal("fetch", fetcher);
  await expect(runModuleQuery(expression, "", "")).resolves.toEqual({ data: envelope, timing: [{ name: "total", duration: 7, counters: {} }] });
  expect(fetcher).toHaveBeenCalledWith("/api/v1/modules/query", expect.objectContaining({
    method: "POST",
    body: JSON.stringify({ args: [expression], root: "", snapshot: "" }),
  }));
});

it("posts structured flags without a positional expression", async () => {
  const envelope = { operation: "set", total: 1, matches: [{ kind: "module", source: scope.root, symbol: `module:${scope.root}` }], declarations: [], symbols: [], coverage: [], warnings: [], stages: [] };
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(envelope), { status: 200, headers: { "Server-Timing": "total;dur=1" } }));
  vi.stubGlobal("fetch", fetcher);
  await expect(runModuleQuery("", scope.root, snapshot, { modules: true, packages: true, include: ["example.org/refs/**"] })).resolves.toMatchObject({ data: envelope });
  expect(fetcher).toHaveBeenCalledWith("/api/v1/modules/query", expect.objectContaining({
    method: "POST", body: JSON.stringify({ root: scope.root, snapshot, modules: true, packages: true, include: ["example.org/refs/**"] }),
  }));
});

it("posts a bare symbol glob unchanged and returns its expanded symbols", async () => {
  const expression = "github.com/flanksource/clicky.Exec*";
  const symbols = ["Exec", "Execf"].map((name) => ({ query_name: `github.com/flanksource/clicky.${name}`, kind: "var" }));
  const envelope = {
    operation: "resolve", total: 2, symbols,
    matches: symbols.map((symbol) => ({ kind: "symbol", query_name: symbol.query_name })),
    declarations: [], coverage: [], warnings: [], stages: [],
  };
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(envelope), { status: 200, headers: { "Server-Timing": "total;dur=1" } }));
  vi.stubGlobal("fetch", fetcher);
  await expect(runModuleQuery(expression, "github.com/flanksource/clicky", "")).resolves.toMatchObject({ data: envelope });
  expect(fetcher).toHaveBeenCalledWith("/api/v1/modules/query", expect.objectContaining({
    method: "POST", body: JSON.stringify({ args: [expression], root: "github.com/flanksource/clicky", snapshot: "" }),
  }));
});

it("scopes checkout history, source projections, and content to a module snapshot", async () => {
  const fetcher = vi.fn().mockImplementation(async () => new Response("{}", { status: 200, headers: { "Server-Timing": "total;dur=1" } }));
  vi.stubGlobal("fetch", fetcher);
  await listModuleLocations("example.org/service");
  await listModuleSnapshots("example.org/service", "/checkout/service", 100);
  await browseModule("snapshot-id");
  await readModuleSource("snapshot-id", "pkg/main.go");
  expect(fetcher.mock.calls.map(([path]) => {
    const url = new URL(path, "http://localhost");
    return [url.pathname, Object.fromEntries(url.searchParams)];
  })).toEqual([
    ["/api/v1/modules/locations", { root: "example.org/service" }],
    ["/api/v1/modules/snapshots", { root: "example.org/service", location: "/checkout/service", offset: "100", limit: "100" }],
    ["/api/v1/modules/browse", { snapshot: "snapshot-id" }],
    ["/api/v1/modules/content", { snapshot: "snapshot-id", path: "pkg/main.go" }],
  ]);
});

it("reads server timing from the explorer browse response", async () => {
  const browse = { sources: [], nodes: [] };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(browse), {
    status: 200, headers: { "Server-Timing": "total;dur=4.1, command;dur=3.5" },
  })));
  await expect(browseModule(snapshot)).resolves.toEqual({ data: browse, timing: [
    { name: "total", duration: 4.1, counters: {} }, { name: "command", duration: 3.5, counters: {} },
  ] });
});

it("sends explicit add and reindex inputs and reports server failures", async () => {
  const fetcher = vi.fn().mockResolvedValueOnce(new Response("[]", { status: 200 }))
    .mockResolvedValueOnce(new Response("index failed", { status: 500, statusText: "Internal Server Error" }));
  vi.stubGlobal("fetch", fetcher);
  await addModules("/repo", true);
  expect(fetcher.mock.calls[0]).toEqual(["/api/v1/modules/add", expect.objectContaining({
    method: "POST", body: JSON.stringify({ args: ["/repo"], "include-tests": true, "no-workspace-uses": true }),
  })]);
  await expect(reindexModules({ path: "/repo", includeTests: true, force: false })).rejects.toThrow("index failed");
  expect(fetcher.mock.calls[1]).toEqual(["/api/v1/modules/reindex", expect.objectContaining({
    method: "POST", body: JSON.stringify({ args: ["/repo"], "include-tests": true, force: false, all: false }),
  })]);
});

it("requests reindex of registered checkouts with missing heads", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response("[]", { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(reindexModules({ all: true })).resolves.toEqual([]);
  expect(fetcher).toHaveBeenCalledWith("/api/v1/modules/reindex", expect.objectContaining({
    method: "POST", body: JSON.stringify({ "include-tests": false, force: false, all: true }),
  }));
});
