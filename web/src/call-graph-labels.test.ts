import { describe, expect, it } from "vitest";

import type { ModuleGraph, ModuleGraphEdge, ModuleGraphNode, ModuleGraphSite } from "./api";
import {
  edgeLabel,
  edgeTitle,
  excludedTally,
  groupCaption,
  interfaceMethods,
  isBuiltin,
  isExternal,
  nodeGlyph,
  nodeKind,
  nodeTitle,
  omittedParts,
  truncate,
  usedGlyphs,
  type PillOptions,
} from "./call-graph-labels";

const LONG_GUARD = "a.very.long.condition(value) == expected";
const PKG = "example.com/mod/pkg";
const located = { location: { path: "x.go", line: 1 } };

function node(id: string, kind: string, extra: Partial<ModuleGraphNode> = {}): ModuleGraphNode {
  return { id, identifier: {}, kind, label: id, depth: 1, in: 1, out: 0, ...extra };
}

function edge(from: string, to: string, type: ModuleGraphEdge["type"], sites: ModuleGraphSite[]): ModuleGraphEdge {
  return { id: `${from}|${to}|${type}`, from, to, type, sites };
}

const at = (line: number, guards?: string[]): ModuleGraphSite => ({ path: "x.go", line, column: 3, ...(guards ? { guards } : {}) });

// `caller` calls the interface method `iface` at line 7, which dispatches to `impl` from that same site.
const KINDS: ModuleGraph = {
  roots: ["caller"],
  nodes: [
    node("caller", "func", { ...located, depth: 0, group: PKG }),
    node("fn", "func", { ...located, group: PKG }),
    node("meth", "method", { ...located, group: PKG }),
    node("iface", "method", { ...located }),
    node("impl", "method", { ...located }),
    node("loop", "func", { ...located, group: PKG }),
    node("len", "builtin", { group: "builtin" }),
    node("scope", "package", { group: PKG }),
    node("typ", "type", { ...located }),
    node("fld", "field", { ...located }),
    node("v", "var", { ...located }),
    node("c", "const", { ...located }),
    node("extFn", "func", { group: "fmt" }),
    node("extMeth", "method", { group: "gorm.io/gorm" }),
    node("lost", "unresolved", { unresolved: true, group: PKG }),
    node("odd", "closure", { ...located }),
  ],
  edges: [
    edge("caller", "iface", "call", [at(7)]),
    edge("caller", "impl", "dispatch", [at(7)]),
    edge("caller", "fn", "call", [at(9)]),
    edge("loop", "loop", "call", [at(4)]),
  ],
  groups: [{ id: PKG, label: PKG }],
  omitted: {},
};
const each = <T>(read: (graph: ModuleGraph, node: ModuleGraphNode) => T) =>
  Object.fromEntries(KINDS.nodes.map((entry) => [entry.id, read(KINDS, entry)]));

describe("node classification", () => {
  it("finds the interface methods: call targets whose call site also dispatches", () => {
    expect([...interfaceMethods(KINDS)]).toEqual(["iface"]);
  });

  it("calls only a builtin kind a builtin", () => {
    expect(KINDS.nodes.filter(isBuiltin).map((entry) => entry.id)).toEqual(["len"]);
  });

  it("calls a node external when it has no source and is not a builtin, a package scope or unresolved", () => {
    expect(KINDS.nodes.filter(isExternal).map((entry) => entry.id)).toEqual(["extFn", "extMeth"]);
  });

  it("picks one glyph per node: unresolved, builtin and package scope first, then recursion, then the kind", () => {
    expect(each(nodeGlyph)).toEqual({
      caller: "func", fn: "func", meth: "method", iface: "interface_method", impl: "method", loop: "recursive",
      len: "builtin", scope: "package", typ: "type", fld: "field", v: "var", c: "const",
      extFn: "external_func", extMeth: "method", lost: "unresolved", odd: "symbol",
    });
  });

  it("says in words what a node is, keeping the kind its glyph may not show", () => {
    expect(each(nodeKind)).toEqual({
      caller: "function", fn: "function", meth: "method", iface: "interface method", impl: "method",
      loop: "recursive function", len: "builtin", scope: "package scope", typ: "type", fld: "field",
      v: "variable", c: "constant", extFn: "external function", extMeth: "external method",
      lost: "unresolved call", odd: "closure",
    });
  });

  it("titles a node with its kind, its label and its full package path", () => {
    const titled = ["fn", "extMeth"].map((id) => nodeTitle(KINDS, KINDS.nodes.find((entry) => entry.id === id)!));
    expect(titled).toEqual([`function fn in ${PKG}`, "external method extMeth in gorm.io/gorm"]);
  });

  it("lists the glyphs a response uses once each, in legend order", () => {
    expect(usedGlyphs(KINDS)).toEqual([
      "func", "method", "interface_method", "recursive", "external_func", "builtin", "type", "field", "var",
      "const", "package", "unresolved", "symbol",
    ]);
  });
});

describe("omittedParts", () => {
  it("says what the response left out, totalling the excluded nodes and leaving out what is zero", () => {
    expect([
      omittedParts({ beyond_depth: 9 }),
      omittedParts({ node_limit: true, beyond_depth: 2, unresolved: 1, unreadable_source: ["a.go"], excluded: { fmt: 3, builtin: 4 } }),
      omittedParts({ excluded: { "gorm.io/gorm": 1 } }),
      omittedParts({}),
    ]).toEqual([
      ["9 beyond depth"],
      ["node limit reached", "2 beyond depth", "1 unresolved", "1 unreadable file", "7 excluded"],
      ["1 excluded"],
      [],
    ]);
  });

  it("tallies the excluded nodes per package, most first, then by path", () => {
    expect(excludedTally({ excluded: { strings: 2, fmt: 5, "gorm.io/gorm": 2 } })).toEqual([
      { group: "fmt", count: 5 },
      { group: "gorm.io/gorm", count: 2 },
      { group: "strings", count: 2 },
    ]);
  });
});

describe("groupCaption", () => {
  it.each([
    ["fmt", "fmt"],
    ["gorm.io/gorm", "gorm.io/gorm"],
    ["github.com/flanksource/uir/query", "uir/query"],
    ["golang.org/x/sync/errgroup", "sync/errgroup"],
  ])("shortens %j to its last two segments: %j", (path, caption) => {
    expect(groupCaption(path)).toBe(caption);
  });
});

describe("truncate", () => {
  it.each([
    ["x > 0", 8, "x > 0"],
    ["exactly8", 8, "exactly8"],
    [LONG_GUARD, 16, "a.very.long.con…"],
    ["trailing space cut", 10, "trailing…"],
  ])("cuts %j to at most %i characters", (text, max, expected) => {
    expect(truncate(text, max)).toBe(expected);
  });
});

describe("edgeLabel", () => {
  const options: PillOptions = { guardLabel: "innermost", truncateAt: 24 };
  const label = (type: ModuleGraphEdge["type"], sites: ModuleGraphSite[], overrides: Partial<PillOptions> = {}) =>
    edgeLabel(edge("a", "b", type, sites), { ...options, ...overrides });

  it("gives an unguarded single-site edge no pill text, dispatch or not", () => {
    expect([label("call", [at(1)]), label("dispatch", [at(1)])]).toEqual([undefined, undefined]);
  });

  it("shows only the site count when one site of several is unguarded", () => {
    expect(label("call", [at(1), at(2, ["err != nil"])])).toBe("×2");
  });

  it("shows the guard every site shares, cut to length, and the site count", () => {
    expect([
      label("call", [at(1, ["x > 0"])]),
      label("call", [at(1, [LONG_GUARD])]),
      label("call", [at(1, ["ok", "err != nil"]), at(2, ["err != nil"])]),
    ]).toEqual(["x > 0", "a.very.long.condition(v…", "err != nil ×2"]);
  });

  it("counts the distinct conditions when the sites' guards differ, rather than show one of them", () => {
    const sites = [at(1, ["ok", "err != nil"]), at(2, ["err != nil"]), at(3, ["ready"])];
    expect([label("call", sites), label("call", sites, { guardLabel: "outermost" })]).toEqual(["2 conditions ×3", "3 conditions ×3"]);
  });

  it("shows how many guards nest at a site on request", () => {
    expect([
      label("call", [at(1, ["a"])], { guardLabel: "count" }),
      label("call", [at(1, ["a"]), at(2, ["a", "b"])], { guardLabel: "count" }),
    ]).toEqual(["1 guard", "1–2 guards ×2"]);
  });

  it("cuts a dispatch guard three characters short of a call's, the room its icon takes", () => {
    expect(label("dispatch", [at(1, [LONG_GUARD])])).toBe("a.very.long.conditio…");
  });
});

describe("edgeTitle", () => {
  it("lists each distinct guard chain once, outermost first, joined with ∧", () => {
    const sites = [at(1, ["ok", "err != nil"]), at(2), at(3, ["err != nil"]), at(4, ["ok", "err != nil"])];
    expect(edgeTitle(edge("a", "b", "call", sites))).toBe("ok ∧ err != nil\nerr != nil");
  });

  it("has nothing to say about an edge with no guarded site", () => {
    expect(edgeTitle(edge("a", "b", "call", [at(1)]))).toBeUndefined();
  });
});
