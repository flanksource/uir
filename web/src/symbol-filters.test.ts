import { expect, it } from "vitest";
import type { ModuleNode } from "./api";
import type { PackageSymbolItem } from "./explorer-model";
import { cycleSymbolFilter, filterPackageSymbols, symbolFilterState } from "./symbol-filters";

function symbol(name: string, kind: string, visibility: string, children: PackageSymbolItem[] = []): PackageSymbolItem {
  const node = { id: name, source_id: "source-1", path: "store.go", symbol: name, node_type: kind,
    kind, visibility, identifier: { method: name }, child_slot: "methods", ordinal: 0,
    payload: {}, semantic_hash: "hash", line: 2, calls: [] } as ModuleNode;
  return { id: name, label: name, kind: "symbol", node, children };
}

const packageItem = (children: PackageSymbolItem[]): PackageSymbolItem => ({
  id: "package:example.org/store", label: "store", kind: "package", packagePath: "example.org/store", children,
});

it("cycles each symbol facet through neutral, include, and exclude with stable URL values", () => {
  const included = cycleSymbolFilter("", "type");
  const excluded = cycleSymbolFilter(included, "type");
  expect([included, excluded, cycleSymbolFilter(excluded, "type")]).toEqual(["+type", "-type", ""]);
  expect(cycleSymbolFilter("+type", "field")).toBe("+field,+type");
  expect([symbolFilterState("+type,-field", "type"), symbolFilterState("+type,-field", "field"),
    symbolFilterState("+type,-field", "method")]).toEqual(["include", "exclude", "neutral"]);
});

it("combines visibility and kind choices and promotes matching children past excluded parents", () => {
  const rows = [packageItem([
    symbol("Store", "type", "exported", [symbol("Save", "method", "exported"), symbol("secret", "field", "internal")]),
    symbol("Build", "func", "exported"), symbol("hidden", "func", "internal"),
  ])];
  expect(filterPackageSymbols(rows, "+exported,-internal", "+func,+method,-type")).toEqual([
    packageItem([symbol("Save", "method", "exported"), symbol("Build", "func", "exported")]),
  ]);
  expect(filterPackageSymbols(rows, "", "-type,-func,-method,-field")).toEqual([]);
  expect(filterPackageSymbols(rows, "", "")).toBe(rows);
});
