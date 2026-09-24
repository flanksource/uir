import { expect, it } from "vitest";
import type { ModuleHead, ModuleSource } from "./api";
import { moduleHeadTree } from "./explorer-model";
import { fileSelectionPatch } from "./explorer-navigation";

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
