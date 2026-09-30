import { afterEach, expect, it, vi } from "vitest";
import { ApiError, addModules, applyModuleRefactor, browseModule, getModuleGraph, getSystemInfo, listModuleDependencies, listModuleHeads, listModuleLocations, listModuleSnapshots, previewModuleRefactor, readModuleSource, reindexModules, runModuleQuery, suggestModuleSymbols, suggestTypedSelectors, type ModuleGraphResult } from "../../../web/src/api";

afterEach(() => vi.unstubAllGlobals());

it("loads dependency edges for the selected immutable snapshot", async () => {
  const dependencies = { captured: true, items: [{ module_path: "example.org/pricing", declared_version: "v1.0.0", target_snapshot_id: "target-1" }] };
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(dependencies), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(listModuleDependencies("snapshot-1")).resolves.toEqual(dependencies);
  expect(fetcher).toHaveBeenCalledWith("/api/v1/modules/dependencies?snapshot=snapshot-1", expect.objectContaining({ headers: { Accept: "application/json" } }));
});

it("previews then applies the exact selected explorer refactor", async () => {
  const request = { snapshot: "head-1", source: "source-1", node: "node-1", action: "rename" as const, newName: "Persist" };
  const preview = { diff: "--- /checkout/store.go\n+++ /checkout/store.go\n@@ -1 +1 @@\n-Save\n+Persist\n", preview_hash: "sha256", files: ["/checkout/store.go"] };
  const applied = { applied: true, files: preview.files, snapshots: [{ snapshot_id: "head-2", location: "/checkout" }] };
  const fetcher = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(preview), { status: 200 }))
    .mockResolvedValueOnce(new Response(JSON.stringify(applied), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(previewModuleRefactor(request)).resolves.toEqual(preview);
  await expect(applyModuleRefactor(request, preview.preview_hash)).resolves.toEqual(applied);
  expect(fetcher.mock.calls).toEqual([
    ["/api/v1/modules/refactor/preview", expect.objectContaining({ method: "POST", body: JSON.stringify({
      snapshot: "head-1", source: "source-1", node: "node-1", action: "rename", "new-name": "Persist",
    }) })],
    ["/api/v1/modules/refactor/apply", expect.objectContaining({ method: "POST", body: JSON.stringify({
      snapshot: "head-1", source: "source-1", node: "node-1", action: "rename", "new-name": "Persist", "preview-hash": "sha256",
    }) })],
  ]);
});

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

const submitID = "33ae5c20b31b1eac8eaa49ac87520ce6a61b9f8c308a413c4e0bba473f7df224";
const chargeID = "0c2f9d0a6d7b1c1e6a7a2f0f6f2b9d3c51c3b8a4f6f1e0d9c8b7a6f5e4d3c2b1";
const hookID = `unresolved:${submitID}:v1:["variable","","example.org/orders","","","hook",""]`;
const ordersLocation = { root_key: "example.org/orders", checkout_path: "/checkout/orders", snapshot_id: snapshot, source_id: "bb0bc7de-b868-48a7-982c-57dc38c82c99", path: "orders.go" };
const submitGraph: ModuleGraphResult = {
  roots: [submitID],
  nodes: [
    { id: submitID, identifier: { package: "example.org/orders", method: "Submit", signature: "(order Order)", node_type: "method" }, kind: "func", label: "Submit", group: "example.org/orders", depth: 0, in: 1, out: 2,
      location: { ...ordersLocation, identity_key: 'v1:["method","","example.org/orders","","Submit","","(order Order)"]', line: 16, column: 6 } },
    { id: chargeID, identifier: { package: "example.org/orders", method: "charge", signature: "(order Order)", node_type: "method" }, kind: "func", label: "charge", group: "example.org/orders", depth: 1, in: 1, out: 1,
      location: { ...ordersLocation, line: 29, column: 6 }, properties: { declarations: "2" } },
    { id: hookID, identifier: { package: "example.org/orders", field: "hook", node_type: "variable" }, kind: "unresolved", label: "hook", group: "example.org/orders", depth: 1, in: 1, out: 0, unresolved: true },
  ],
  edges: [
    { id: `${submitID}|${chargeID}|call`, from: submitID, to: chargeID, type: "call", sites: [
      { path: "orders.go", line: 18, column: 3, text: "charge", guards: ["order.Total > 0"] },
      { path: "orders.go", line: 21, column: 3, text: "charge", guards: ["order.Rush"] },
    ] },
    { id: `${submitID}|${hookID}|call`, from: submitID, to: hookID, type: "call", sites: [{ path: "orders.go", line: 24, column: 3, text: "hook", guards: ["hook != nil"] }] },
    { id: `${submitID}|${chargeID}|dispatch`, from: submitID, to: chargeID, type: "dispatch", sites: [{ path: "orders.go", line: 26, column: 11, text: "notifier.Notify" }] },
  ],
  omitted: { node_limit: true, beyond_depth: 1, unresolved: 1, unreadable_source: ["orders.go"] },
  stages: [{ name: "scope", value: "1 snapshots" }, { name: "resolve", value: "example.org/orders.Submit" }],
  warnings: [],
  candidates: [],
};

it("gets the call graph of a selector with only the options that were set", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(submitGraph), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(getModuleGraph({ selector: "orders.Submit", direction: "both", depth: 2, limit: 150, root: "example.org/orders", snapshot })).resolves.toEqual(submitGraph);
  expect(fetcher).toHaveBeenCalledWith(
    `/api/v1/modules/graph?selector=orders.Submit&direction=both&depth=2&limit=150&root=example.org%2Forders&snapshot=${snapshot}`,
    expect.objectContaining({ headers: { Accept: "application/json" } }),
  );
});

it("gets the call graph of a node by its symbol id and returns the candidates of an ambiguous selector", async () => {
  const candidates: ModuleGraphResult = { roots: [], nodes: [], edges: [], omitted: {}, warnings: [], stages: [{ name: "resolve", value: "2 candidates" }], candidates: [
    { id: saveID, module_key: scope.root, package_path: "example.org/refs/store", kind: "method", owner: "Store", name: "Save", query_name: "example.org/refs/store.Store.Save", visibility: "exported", parameter_types: [] },
    { id: chargeID, module_key: scope.root, package_path: "example.org/refs/cache", kind: "method", owner: "Cache", name: "Save", query_name: "example.org/refs/cache.Cache.Save", visibility: "exported", parameter_types: [] },
  ] };
  const fetcher = vi.fn().mockImplementation(async () => new Response(JSON.stringify(candidates), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(getModuleGraph({ selector: "Save" })).resolves.toEqual(candidates);
  await getModuleGraph({ symbol: saveID, direction: "callers", location: scope.location });
  expect(fetcher.mock.calls.map(([path]) => path)).toEqual([
    "/api/v1/modules/graph?selector=Save",
    `/api/v1/modules/graph?symbol=${saveID}&direction=callers&location=%2Fcheckout%2Frefs`,
  ]);
});

it("rejects a graph response that is not the graph envelope and surfaces a refused request", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ roots: [submitID], nodes: [], edges: [] }), { status: 200 })));
  await expect(getModuleGraph({ selector: "orders.Submit" })).rejects.toThrow("Graph response is not a graph envelope");
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
    code: "invalid_query", message: "depth 9 is outside 1..8", hint: "Give depth 1 through 8", trace: "trace-3",
  }), { status: 400, headers: { "Content-Type": "application/json" } })));
  await expect(getModuleGraph({ selector: "orders.Submit", depth: 9 })).rejects.toMatchObject({
    name: "ApiError", status: 400, code: "invalid_query", message: "depth 9 is outside 1..8", hint: "Give depth 1 through 8",
  } satisfies Partial<ApiError>);
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

it("reads symbol kind, visibility, and server timing from the explorer browse response", async () => {
  const browse = { sources: [], nodes: [{ id: "symbol-1", kind: "func", visibility: "exported" }] };
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
