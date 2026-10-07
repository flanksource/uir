import { describe, expect, it } from "vitest";

import type { ModuleGraphPackage } from "./api";
import { buildPackageTree, type PackageTreeNode } from "./call-graph-package-tree";

const pkg = (path: string, external: boolean, excluded: boolean, nodes = 1): ModuleGraphPackage => ({ path, external, excluded, nodes });
const PROMPTRUN = pkg("github.com/flanksource/captain/pkg/promptrun", false, false, 7);
const AGENT = pkg("github.com/flanksource/captain/pkg/agent", false, false, 3);
const FMT = pkg("fmt", true, true, 5);
const HTTP = pkg("net/http", true, false, 2);
const URL = pkg("net/url", true, true, 1);
const BUILTIN = pkg("builtin", true, true, 4);
const GORM = pkg("gorm.io/gorm", true, true, 4);
const GORM_CLAUSE = pkg("gorm.io/gorm/clause", true, false, 1);

/** The fields that describe a node's place and checkbox, without the package lists the assertions would repeat. */
type Shape = { id: string; label: string; kind: PackageTreeNode["kind"]; pattern: string; state: PackageTreeNode["state"]; nodes: number; external: boolean; children: Shape[] };
function shape(nodes: PackageTreeNode[]): Shape[] {
  return nodes.map(({ id, label, kind, pattern, state, nodes, external, children }) => ({ id, label, kind, pattern, state, nodes, external, children: shape(children) }));
}

const leaf = (label: string, target: ModuleGraphPackage): Shape => ({
  id: target.path, label, kind: "package", pattern: target.path, state: target.excluded ? "hidden" : "shown", nodes: target.nodes, external: target.external, children: [],
});

describe("buildPackageTree", () => {
  it("groups the standard library under one root, nested by path, after builtin and the other roots", () => {
    expect(shape(buildPackageTree([HTTP, BUILTIN, FMT, URL]))).toEqual([
      leaf("builtin", BUILTIN),
      { id: "std", label: "Standard library", kind: "std", pattern: "std", state: "mixed", nodes: 8, external: true, children: [
        leaf("fmt", FMT),
        { id: "net/...", label: "net", kind: "folder", pattern: "net/...", state: "mixed", nodes: 3, external: true, children: [leaf("http", HTTP), leaf("url", URL)] },
      ] },
    ]);
  });

  it("compacts a chain of single-child path elements into one folder", () => {
    expect(shape(buildPackageTree([PROMPTRUN, AGENT]))).toEqual([
      { id: "github.com/flanksource/captain/pkg/...", label: "github.com/flanksource/captain/pkg", kind: "folder", pattern: "github.com/flanksource/captain/pkg/...",
        state: "shown", nodes: 10, external: false, children: [leaf("agent", AGENT), leaf("promptrun", PROMPTRUN)] },
    ]);
  });

  it("gives a package with packages below it a leaf of its own, first in its folder", () => {
    expect(shape(buildPackageTree([GORM_CLAUSE, GORM]))).toEqual([
      { id: "gorm.io/gorm/...", label: "gorm.io/gorm", kind: "folder", pattern: "gorm.io/gorm/...", state: "mixed", nodes: 5, external: true,
        children: [leaf("gorm (package)", GORM), leaf("clause", GORM_CLAUSE)] },
    ]);
  });

  it("collects every reached package of a subtree and leaves out the std group when no std package was reached", () => {
    const tree = buildPackageTree([PROMPTRUN, GORM, AGENT]);
    expect(tree.map((node) => ({ id: node.id, packages: node.packages.map((entry) => entry.path), state: node.state })))
      .toEqual([
        { id: "github.com/flanksource/captain/pkg/...", packages: [AGENT.path, PROMPTRUN.path], state: "shown" },
        { id: GORM.path, packages: [GORM.path], state: "hidden" },
      ]);
  });
});
