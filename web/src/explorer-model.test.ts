import { expect, it } from "vitest";
import type { ModuleHead, ModuleNode, ModuleSource } from "./api";
import { fileTree, moduleHeadTree, packageSymbolTree, scopedHeads, symbolModuleHeads, symbolTree, type PackageSymbolItem } from "./explorer-model";

function source(id: string, path: string): ModuleSource {
  return { id, path, root_key: "example.org/service", location: "/checkout/service", snapshot_id: "snapshot-1",
    package_path: "example.org/service", content_hash: "hash", size_bytes: 12 };
}

function node(id: string, sourceId: string, parentIdentity = ""): ModuleNode {
  return { id: `${sourceId}:${id}`, source_id: sourceId, path: "internal/server.go", symbol: id,
    node_type: "method", kind: "method", visibility: "exported", identifier: { method: id, signature: "()" }, parent_identity: parentIdentity,
    child_slot: "", ordinal: 0, payload: {}, semantic_hash: "hash", line: 2, calls: [] };
}

it("groups source paths into folders with stable file selections", () => {
  expect(fileTree([source("b", "internal/server.go"), source("a", "main.go"), source("c", "internal/client.go")])).toEqual([
    { id: "folder:internal", label: "internal", path: "internal", children: [
      { id: "file:c", label: "client.go", path: "internal/client.go", children: [], source: source("c", "internal/client.go") },
      { id: "file:b", label: "server.go", path: "internal/server.go", children: [], source: source("b", "internal/server.go") },
    ] },
    { id: "file:a", label: "main.go", path: "main.go", children: [], source: source("a", "main.go") },
  ]);
});

it("groups every checkout head under its module and keeps reused source IDs distinct", () => {
  const heads: ModuleHead[] = [
    { root_key: "example.org/service", name: "service", location: "/work/service-a", snapshot_id: "head-a", sources: [{ ...source("shared", "internal/run.go"), location: "/work/service-a", snapshot_id: "head-a" }] },
    { root_key: "example.org/service", name: "service", location: "/work/service-b", snapshot_id: "head-b", sources: [{ ...source("shared", "internal/run.go"), location: "/work/service-b", snapshot_id: "head-b" }] },
    { root_key: "example.org/worker", name: "worker", location: "/work/worker", snapshot_id: "head-c", sources: [{ ...source("worker", "main.go"), root_key: "example.org/worker", location: "/work/worker", snapshot_id: "head-c" }] },
  ];
  const tree = moduleHeadTree(heads);
  expect(tree.map((root) => [root.label, root.children.map((checkout) => checkout.label)])).toEqual([
    ["service", ["service-a", "service-b"]], ["worker", ["main.go"]],
  ]);
  expect(tree[1].head).toBe(heads[2]);
  const first = tree[0].children[0].children[0].children[0];
  const second = tree[0].children[1].children[0].children[0];
  expect(first).toMatchObject({ kind: "file", label: "run.go", head: heads[0] });
  expect(second).toMatchObject({ kind: "file", label: "run.go", head: heads[1] });
  expect(first.id).not.toBe(second.id);
});

it.each([
  ["every head for the every-module scope", "", ["head-a", "head-b", "head-c"]],
  ["only the scoped root's heads for a module scope", "example.org/worker", ["head-c"]],
])("keeps %s", (_, scope, expected) => {
  const heads: ModuleHead[] = [
    { root_key: "example.org/service", name: "service", location: "/work/service-a", snapshot_id: "head-a", sources: [] },
    { root_key: "example.org/service", name: "service", location: "/work/service-b", snapshot_id: "head-b", sources: [] },
    { root_key: "example.org/worker", name: "worker", location: "/work/worker", snapshot_id: "head-c", sources: [] },
  ];
  expect(scopedHeads(heads, scope).map((head) => head.snapshot_id)).toEqual(expected);
});

it("rejects source metadata from a different checkout head", () => {
  const head: ModuleHead = { root_key: "example.org/service", name: "service", location: "/work/service", snapshot_id: "head-a", sources: [source("a", "main.go")] };
  expect(() => moduleHeadTree([head])).toThrow(/does not belong to head/);
});

it("nests symbols by the source-local parent identity", () => {
  const parent = node("Service", "source-1");
  parent.identifier = { type: "Service" };
  parent.node_type = "type";
  parent.kind = "type";
  const child = node("Run", "source-1", "Service");
  expect(symbolTree([child, parent])).toEqual([
    { id: parent.id, label: "Service", node: parent, children: [
      { id: child.id, label: "Run()", node: child, children: [] },
    ] },
  ]);
});

it("rejects a symbol whose parent is absent from its indexed source", () => {
  expect(() => symbolTree([node("Run", "source-1", "missing")])).toThrow(/missing parent/);
});

it("shows a method at the file root when its type is declared in another indexed source", () => {
  const parentIdentity = 'v1:["class","","example.org/service","Cost","","",""]';
  const parent = node(parentIdentity, "cost-source");
  parent.node_type = "class";
  parent.kind = "type";
  parent.identifier = { package: "example.org/service", type: "Cost" };
  const child = node("Add", "agent-source", parentIdentity);
  child.identifier = { package: "example.org/service", type: "Cost", method: "Add", signature: "(other Cost) Cost" };

  expect(symbolTree([child], [parent, child])).toEqual([
    { id: child.id, label: "Add(other Cost) Cost", node: child, children: [] },
  ]);
});

it("places package-level declarations at the outline root", () => {
  const build = node("Build", "source-1", 'v1:["package","","example.org/service","","","",""]');
  build.identifier.package = "example.org/service";
  expect(symbolTree([build])).toEqual([{ id: build.id, label: "Build()", node: build, children: [] }]);
});

it("groups symbols from multiple files by indexed package without file rows", () => {
  const root = "example.org/service";
  const sources = [source("root", "main.go"), { ...source("type", "internal/type.go"), package_path: `${root}/internal` },
    { ...source("method", "internal/method.go"), package_path: `${root}/internal` }];
  const run = { ...node("Run", "root"), path: "main.go", identifier: { package: root, method: "Run", signature: "()" } };
  const service = { ...node("Service", "type"), path: "internal/type.go", identifier: { package: `${root}/internal`, type: "Service" } };
  service.kind = "type";
  const serve = { ...node("Serve", "method"), path: "internal/method.go", identifier: { package: `${root}/internal`, type: "Service", method: "Serve", signature: "()" } };
  expect(packageSymbolTree(root, { sources, nodes: [serve, run, service] })).toEqual([
    { id: `package:${root}`, kind: "package", label: "service", packagePath: root, children: [
      { id: run.id, kind: "symbol", label: "Run()", node: run, children: [] },
      { id: `package:${root}/internal`, kind: "package", label: "internal", packagePath: `${root}/internal`, children: [
        { id: serve.id, kind: "symbol", label: "Serve()", node: serve, children: [] },
        { id: service.id, kind: "symbol", label: "Service", node: service, children: [] },
      ] },
    ] },
  ]);
});

it("sorts symbols by name before nested subpackages at every package level", () => {
  const root = "example.org/service";
  const sources = [source("z", "z.go"), source("a", "a.go"),
    { ...source("internal", "internal/main.go"), package_path: `${root}/internal` },
    { ...source("zeta", "internal/zeta/main.go"), package_path: `${root}/internal/zeta` },
    { ...source("alpha", "internal/alpha/main.go"), package_path: `${root}/internal/alpha` }];
  const zoo = node("Zoo", "z");
  const alpha = node("Alpha", "a");
  const inside = node("Inside", "internal");
  const zeta = node("Zeta", "zeta");
  const alphaPackage = node("AlphaPackage", "alpha");
  const item = (symbol: ModuleNode): PackageSymbolItem => ({ id: symbol.id, label: `${symbol.identifier.method}()`, kind: "symbol", node: symbol, children: [] });
  const pkg = (path: string, children: PackageSymbolItem[]): PackageSymbolItem => ({
    id: `package:${path}`, label: path.split("/").at(-1)!, kind: "package", packagePath: path, children,
  });
  expect(packageSymbolTree(root, { sources, nodes: [zoo, zeta, inside, alphaPackage, alpha] })).toEqual([
    pkg(root, [item(alpha), item(zoo), pkg(`${root}/internal`, [
      item(inside), pkg(`${root}/internal/alpha`, [item(alphaPackage)]), pkg(`${root}/internal/zeta`, [item(zeta)]),
    ])]),
  ]);
});

it("sorts nested type members by name instead of source order", () => {
  const root = "example.org/service";
  const owner = node("Store", "source-1");
  owner.identifier = { package: root, type: "Store" };
  owner.kind = "type";
  const zulu = node("Zulu", "source-1", "Store");
  zulu.line = 2;
  const alpha = node("Alpha", "source-1", "Store");
  alpha.line = 3;
  expect(packageSymbolTree(root, { sources: [source("source-1", "store.go")], nodes: [zulu, owner, alpha] })).toEqual([
    { id: `package:${root}`, kind: "package", label: "service", packagePath: root, children: [
      { id: owner.id, kind: "symbol", label: "Store", node: owner, children: [
        { id: alpha.id, kind: "symbol", label: "Alpha()", node: alpha, children: [] },
        { id: zulu.id, kind: "symbol", label: "Zulu()", node: zulu, children: [] },
      ] },
    ] },
  ]);
});

it("rejects a symbol whose source is absent from the selected snapshot", () => {
  expect(() => packageSymbolTree("example.org/service", { sources: [source("root", "main.go")], nodes: [node("Run", "missing")] }))
    .toThrow(/missing source/);
});

it("uses each primary head across all modules and the chosen head when scoped", () => {
  const roots = [
    { root_key: "example.org/alpha", name: "alpha", location: "/primary/alpha", snapshot_id: "alpha-head", head_version: 1 },
    { root_key: "example.org/beta", name: "beta", location: "/primary/beta", snapshot_id: "beta-head", head_version: 2 },
  ];
  expect(symbolModuleHeads(roots, { module: "", location: "/other/alpha", snapshot: "alpha-old" })).toEqual([
    { rootKey: "example.org/alpha", name: "alpha", location: "/primary/alpha", snapshot: "alpha-head" },
    { rootKey: "example.org/beta", name: "beta", location: "/primary/beta", snapshot: "beta-head" },
  ]);
  expect(symbolModuleHeads(roots, { module: "example.org/alpha", location: "/other/alpha", snapshot: "alpha-old" })).toEqual([
    { rootKey: "example.org/alpha", name: "alpha", location: "/other/alpha", snapshot: "alpha-old" },
  ]);
});
