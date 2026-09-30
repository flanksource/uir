import { expect, it } from "vitest";
import type { ModuleHead, ModuleNode, ModuleSource } from "./api";
import { moduleHeadTree } from "./explorer-model";
import { fileSelectionPatch, symbolSelectionPatch } from "./explorer-navigation";

it("only opens a file when an Explorer tree row is selected", () => {
  const source: ModuleSource = { id: "source-1", root_key: "example.org/service", location: "/work/service", snapshot_id: "head-1",
    path: "internal/main.go", package_path: "example.org/service/internal", content_hash: "hash", size_bytes: 12 };
  const head: ModuleHead = { root_key: source.root_key, name: "service", location: source.location, snapshot_id: source.snapshot_id, sources: [source] };
  const other: ModuleHead = { ...head, location: "/work/service-copy", snapshot_id: "head-2",
    sources: [{ ...source, location: "/work/service-copy", snapshot_id: "head-2" }] };
  const module = moduleHeadTree([head, other])[0];
  const checkout = module.children[0];
  const folder = checkout.children[0];
  const file = folder.children[0];

  expect([fileSelectionPatch(module), fileSelectionPatch(checkout), fileSelectionPatch(folder)]).toEqual([undefined, undefined, undefined]);
  expect(fileSelectionPatch(file)).toEqual({ location: head.location, snapshot: head.snapshot_id,
    source: source.id, node: "", fileSearch: "", symbolSearch: "", offset: 0 });
  expect(fileSelectionPatch(file)).not.toHaveProperty("module");
});

it("opens a symbol in its selected snapshot at the indexed position", () => {
  const node: ModuleNode = { id: "symbol-1", source_id: "source-1", path: "internal/main.go", symbol: "Run", node_type: "method", kind: "func", visibility: "exported",
    identifier: { method: "Run" }, child_slot: "methods", ordinal: 0, payload: {}, semantic_hash: "hash", line: 12, column: 4, calls: [] };
  expect(symbolSelectionPatch({ location: "/work/service", snapshot: "head-1" }, node)).toEqual({
    location: "/work/service", snapshot: "head-1", source: "source-1", node: "symbol-1", line: 12, column: 4,
    fileSearch: "", symbolSearch: "", offset: 0,
  });
  expect(symbolSelectionPatch({ location: "/work/service", snapshot: "head-1" }, node)).not.toHaveProperty("module");
  expect(() => symbolSelectionPatch({ location: "/work/service", snapshot: "head-1" }, { ...node, line: undefined })).toThrow(/line/);
});
