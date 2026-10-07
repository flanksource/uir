// The call graph's package facets as a checkbox tree: packages nested by import path, the standard
// library grouped under one root. A folder's checkbox shows or hides its whole subtree.
import type { ModuleGraphPackage } from "./api";
import { EXCLUDE_STD, isStdPackage } from "./call-graph-exclude";

export type PackageTreeNode = {
  /** "std" for the standard library group, a folder's pattern, or a package's path. */
  id: string;
  /** The path elements this node adds below its parent, a compacted chain joined by "/". */
  label: string;
  /** The folder's prefix or the package's path; empty for the standard library group. */
  path: string;
  kind: "std" | "folder" | "package";
  pkg?: ModuleGraphPackage;
  /** What unchecking the node excludes: `std`, `prefix/...`, or the package's exact path. */
  pattern: string;
  /** Every reached package in the subtree. */
  packages: ModuleGraphPackage[];
  state: "shown" | "hidden" | "mixed";
  nodes: number;
  /** Every package in the subtree is outside the scope. */
  external: boolean;
  children: PackageTreeNode[];
};

type Trie = { segment: string; path: string; pkg?: ModuleGraphPackage; children: Map<string, Trie> };
type Shape = Omit<PackageTreeNode, "packages" | "state" | "nodes" | "external">;

/**
 * The package tree. A chain of path elements with one child each and no package of its own is compacted
 * into one folder; a package with packages below it becomes a folder whose first child is the package
 * itself, so either can be toggled. Each node's state is computed here, on the full tree, so filtering
 * the tree later never changes a folder's checkbox.
 */
export function buildPackageTree(packages: readonly ModuleGraphPackage[]): PackageTreeNode[] {
  const std = new Map<string, Trie>();
  const others = new Map<string, Trie>();
  packages.forEach((pkg) => insert(isStdPackage(pkg) ? std : others, pkg));
  const roots = toNodes(others);
  if (std.size === 0) return roots;
  return [...roots, summarise({ id: EXCLUDE_STD, label: "Standard library", path: "", kind: "std", pattern: EXCLUDE_STD, children: toNodes(std) })];
}

function insert(roots: Map<string, Trie>, pkg: ModuleGraphPackage) {
  let level = roots;
  let node: Trie | undefined;
  for (const segment of pkg.path.split("/")) {
    const path: string = node ? `${node.path}/${segment}` : segment;
    node = level.get(segment) ?? { segment, path, children: new Map() };
    level.set(segment, node);
    level = node.children;
  }
  if (!node) throw new Error(`Package ${JSON.stringify(pkg.path)} has no path`);
  node.pkg = pkg;
}

function toNodes(tries: Map<string, Trie>): PackageTreeNode[] {
  return [...tries.values()].map((trie) => toNode(trie, trie.segment)).sort((a, b) => a.label < b.label ? -1 : a.label > b.label ? 1 : 0);
}

function toNode(trie: Trie, label: string): PackageTreeNode {
  const children = [...trie.children.values()];
  if (!trie.pkg && children.length === 1) return toNode(children[0], `${label}/${children[0].segment}`);
  if (children.length === 0) {
    if (!trie.pkg) throw new Error(`Package path ${trie.path} ends without a package`);
    return summarise({ id: trie.path, label, path: trie.path, kind: "package", pkg: trie.pkg, pattern: trie.path, children: [] });
  }
  const own = trie.pkg ? [toNode({ ...trie, children: new Map() }, `${trie.segment} (package)`)] : [];
  const pattern = `${trie.path}/...`;
  return summarise({ id: pattern, label, path: trie.path, kind: "folder", pattern, children: [...own, ...toNodes(trie.children)] });
}

function summarise(shape: Shape): PackageTreeNode {
  const packages = shape.pkg ? [shape.pkg] : shape.children.flatMap((child) => child.packages);
  const hidden = packages.filter((pkg) => pkg.excluded).length;
  return {
    ...shape,
    packages,
    state: hidden === 0 ? "shown" : hidden === packages.length ? "hidden" : "mixed",
    nodes: packages.reduce((sum, pkg) => sum + pkg.nodes, 0),
    external: packages.every((pkg) => pkg.external),
  };
}
