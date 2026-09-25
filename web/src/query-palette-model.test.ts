import { describe, expect, it } from "vitest";
import type { ModuleQueryResult, ModuleQueryRow, ModuleSource } from "./api";
import { queryMatchLabel, queryMatchRoute, queryPaletteRows, searchInputMode } from "./query-palette-model";

const row: ModuleQueryRow = {
  kind: "caller", symbol: "store.Store.Save", root: "example.org/shop", location: "/work/shop",
  snapshot_id: "snapshot-1", source: "store/store.go:12:4", path: "store/store.go", line: 12, column: 4,
};
const sources: ModuleSource[] = [
  { id: "other", root_key: "example.org/other", location: "/work/other", snapshot_id: "snapshot-1", path: "store/store.go", package_path: "example.org/other/store", content_hash: "a", size_bytes: 1 },
  { id: "source-1", root_key: "example.org/shop", location: "/work/shop", snapshot_id: "snapshot-1", path: "store/store.go", package_path: "example.org/shop/store", content_hash: "b", size_bytes: 1 },
];

describe("query palette result navigation", () => {
  it("switches only a leading > search into an expression", () => {
    expect(searchInputMode("> store.Store.Save <")).toEqual({ mode: "expression", value: "store.Store.Save <" });
    expect(searchInputMode("store.Store.Save")).toEqual({ mode: "search", value: "store.Store.Save" });
    expect(searchInputMode("find > symbol")).toEqual({ mode: "search", value: "find > symbol" });
  });

  it("uses located call sites for a path and separates unlocated candidates otherwise", () => {
    const candidate = { ...row, kind: "candidate", path: undefined, symbol_id: "symbol-1" };
    const result: ModuleQueryResult = { operation: "callers", total: 2, matches: [row, candidate], declarations: [],
      symbols: [{ id: "symbol-1", module_key: "example.org/shop", package_path: "example.org/shop/store", kind: "method", name: "Save",
        query_name: "example.org/shop/store.Store.Save", visibility: "exported", parameter_types: [] }], coverage: [], stages: [] };
    expect(queryPaletteRows(result)).toEqual({ matches: [row], candidates: [candidate] });
    expect(queryPaletteRows({ ...result, path: { calls: [{ ...row, kind: "path" }], symbols: [] } }))
      .toEqual({ matches: [{ ...row, kind: "path" }], candidates: [] });
    expect(() => queryPaletteRows({ ...result, symbols: [] })).toThrow(/Candidate symbol-1 has no resolved symbol/);
  });

  it("opens the exact indexed source and position across module roots", () => {
    expect(queryMatchRoute(row, sources)).toEqual({
      view: "explorer", module: "example.org/shop", location: "/work/shop", snapshot: "snapshot-1",
      source: "source-1", node: "", line: 12, column: 4, fileSearch: "", symbolSearch: "",
    });
  });

  it("shows the Go owner and member instead of truncating the end of a symbol key", () => {
    expect(queryMatchLabel({ ...row, symbol: "method:example.org/shop/store.Store:Save#(ctx context.Context) error" }))
      .toBe("store.Store.Save");
  });

  it("fails when a result has no source in its snapshot", () => {
    expect(() => queryMatchRoute(row, sources.slice(0, 1))).toThrow(/store\/store.go.*snapshot-1/);
  });

  it("fails on an unlocated candidate", () => {
    expect(() => queryMatchRoute({ ...row, path: undefined }, sources)).toThrow(/has no indexed file/);
  });
});
