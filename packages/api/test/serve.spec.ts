import { afterEach, expect, it, vi } from "vitest";
import { addModules, browseModule, getSystemInfo, listModuleHeads, listModuleLocations, listModuleSnapshots, readModuleSource, reindexModules, runModuleQuery } from "../../../web/src/api";

afterEach(() => vi.unstubAllGlobals());

it("loads every module checkout head for the explorer tree", async () => {
  const heads = [{ root_key: "example.org/service", name: "service", location: "/checkout/service", snapshot_id: "head-1", sources: [] }];
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(heads), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(listModuleHeads()).resolves.toEqual(heads);
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

it.each([
  {
    name: "references with declarations, symbols, and a partial package",
    expression: 'references of node where type = "Store" and method = "Save"',
    envelope: {
      operation: "references", total: 1,
      matches: [{
        kind: "reference", ...scope, symbol: "method:example.org/refs/app:Run#()", source: "app/app.go:5:28",
        path: "app/app.go", line: 5, column: 28, end_line: 5, end_column: 32, role: "call", symbol_id: saveID,
        enclosing_id: "d7e469f71d728df80c44c3f76b4408fbeeeebfc9753a23029f712fd8817ec05d",
        enclosing_key: 'v1:["method","","example.org/refs/app","","Run","","()"]', coverage: "indexed",
      }],
      declarations: [saveDeclaration],
      symbols: [{ id: saveID, module_key: scope.root, package_path: "example.org/refs/store", kind: "method", owner: "Store", name: "Save", visibility: "exported", parameter_types: [] }],
      coverage: [brokenPartial],
      stages: [{ name: "parse", value: "references" }, { name: "coverage", value: "incomplete: 1 package is not fully indexed (1 partial)" }],
    },
  },
  {
    name: "search rows in the same envelope",
    expression: 'search "Store.Sa"',
    envelope: {
      operation: "search", total: 1, matches: [{ ...saveDeclaration, kind: "symbol" }], declarations: [], symbols: [], coverage: [brokenPartial],
      stages: [{ name: "search", value: 'prefix "sa": 1 declared symbols' }],
    },
  },
])("posts the scoped PEG query and returns the $name", async ({ expression, envelope }) => {
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(envelope), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(runModuleQuery(expression, scope.root, snapshot)).resolves.toEqual(envelope);
  expect(fetcher).toHaveBeenCalledWith("/api/v1/modules/query", expect.objectContaining({
    method: "POST",
    body: JSON.stringify({ args: [expression], root: scope.root, snapshot }),
  }));
});

it("scopes checkout history, source projections, and content to a module snapshot", async () => {
  const fetcher = vi.fn().mockImplementation(async () => new Response("{}", { status: 200 }));
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

it("sends explicit add and reindex inputs and reports server failures", async () => {
  const fetcher = vi.fn().mockResolvedValueOnce(new Response("[]", { status: 200 }))
    .mockResolvedValueOnce(new Response("index failed", { status: 500, statusText: "Internal Server Error" }));
  vi.stubGlobal("fetch", fetcher);
  await addModules("/repo", true);
  expect(fetcher.mock.calls[0]).toEqual(["/api/v1/modules/add", expect.objectContaining({
    method: "POST", body: JSON.stringify({ args: ["/repo"], "include-tests": true, "no-workspace-uses": true }),
  })]);
  await expect(reindexModules("/repo", true, false)).rejects.toThrow("500 Internal Server Error: index failed");
  expect(fetcher.mock.calls[1]).toEqual(["/api/v1/modules/reindex", expect.objectContaining({
    method: "POST", body: JSON.stringify({ args: ["/repo"], "include-tests": true, force: false }),
  })]);
});
