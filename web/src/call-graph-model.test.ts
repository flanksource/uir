import { describe, expect, it } from "vitest";

import type { ModuleGraph, ModuleGraphNode } from "./api";
import {
  columnGap,
  expandCount,
  expansionDirection,
  explorerNodeId,
  graphRoot,
  hiddenNeighbours,
  mergeGraph,
  nodeRevealPatch,
  siteRevealPatch,
  toDiagram,
  visibleGraph,
  walkedTotals,
  type DiagramGlyphs,
  type DiagramOptions,
  type GraphView,
} from "./call-graph-model";

const LONG_GUARD = "a.very.long.condition(value) == expected";
const PKG = "example.com/mod/pkg";
const CMD = "example.com/mod/cmd/app";
const SNAPSHOT = "snap-1";
const CHECKOUT = "/checkout/mod";

function node(id: string, depth: number, totals: { in: number; out: number }, extra: Partial<ModuleGraphNode> = {}): ModuleGraphNode {
  return { id, identifier: {}, kind: "func", label: id, group: PKG, depth, ...totals,
    location: { path: `${id}.go`, line: 1, column: 6, source_id: `src-${id}`, snapshot_id: SNAPSHOT, checkout_path: CHECKOUT,
      identity_key: `v1:["method","","${PKG}","","${id}","","()"]` }, ...extra };
}

// far → caller → root → a → c, and root → b (unresolved), root and a → len (builtin), a → Errorf (external).
// As the server reports them: `out` is a total for the root and callees, `in` for the root and callers,
// and the other count is the edges in the response. `a` has two callees and `far` two callers beyond it.
const TINY: ModuleGraph = {
  roots: ["root"],
  nodes: [
    node("far", -2, { in: 2, out: 1 }, { group: CMD }),
    node("caller", -1, { in: 1, out: 1 }, { group: CMD }),
    node("root", 0, { in: 1, out: 3 }),
    node("a", 1, { in: 1, out: 5 }),
    { id: "b", identifier: {}, kind: "unresolved", label: "fn", group: PKG, depth: 1, in: 1, out: 0, unresolved: true },
    { id: "errorf", identifier: {}, kind: "func", label: "Errorf", group: "fmt", depth: 1, in: 1, out: 0 },
    { id: "len", identifier: {}, kind: "builtin", label: "len", group: "builtin", depth: 1, in: 2, out: 0 },
    node("c", 2, { in: 1, out: 0 }),
  ],
  edges: [
    { id: "far|caller|call", from: "far", to: "caller", type: "call", sites: [{ path: "far.go", line: 4 }] },
    { id: "caller|root|call", from: "caller", to: "root", type: "call", sites: [{ path: "caller.go", line: 7 }, { path: "caller.go", line: 8 }] },
    { id: "root|a|call", from: "root", to: "a", type: "call", sites: [
      { path: "root.go", line: 3, text: "a()", guards: ["x > 0"] },
      { path: "root.go", line: 5, text: "a()", guards: ["!(x > 0)", LONG_GUARD] },
    ] },
    { id: "root|b|call", from: "root", to: "b", type: "call", sites: [{ path: "root.go", line: 9, text: "fn()" }] },
    { id: "root|len|call", from: "root", to: "len", type: "call", sites: [{ path: "root.go", line: 2 }, { path: "root.go", line: 4, guards: ["x > 0"] }] },
    { id: "a|c|dispatch", from: "a", to: "c", type: "dispatch", sites: [{ path: "a.go", line: 2, guards: [LONG_GUARD] }] },
    { id: "a|errorf|call", from: "a", to: "errorf", type: "call", sites: [
      { path: "a.go", line: 5, column: 9, guards: ["err != nil"] },
      { path: "a.go", line: 9, column: 9, guards: ["y", "err != nil"] },
    ] },
    { id: "a|len|call", from: "a", to: "len", type: "call", sites: [{ path: "a.go", line: 3 }] },
  ],
  groups: [{ id: CMD, label: CMD }, { id: "fmt", label: "fmt" }, { id: PKG, label: PKG }, { id: "builtin", label: "builtin" }],
  omitted: { beyond_depth: 2 },
};

const view = (overrides: Partial<GraphView> = {}): GraphView => ({ direction: "both", depth: 1, revealed: [], ...overrides });
const ids = (items: readonly { id: string }[]) => items.map((item) => item.id);
const shown = (overrides: Partial<GraphView> = {}) => {
  const visible = visibleGraph(TINY, view(overrides));
  return { nodes: ids(visible.nodes), edges: ids(visible.edges) };
};
const requireIn = (graph: ModuleGraph, id: string) => graph.nodes.find((entry) => entry.id === id)!;

describe("visibleGraph", () => {
  it("keeps only the root and callees within the depth for the callees direction", () => {
    expect(shown({ direction: "callees" })).toEqual({
      nodes: ["root", "a", "b", "errorf", "len"],
      edges: ["root|a|call", "root|b|call", "root|len|call", "a|errorf|call", "a|len|call"],
    });
  });

  it("keeps only the root and callers within the depth for the callers direction", () => {
    expect(shown({ direction: "callers", depth: 2 })).toEqual({ nodes: ["far", "caller", "root"], edges: ["far|caller|call", "caller|root|call"] });
  });

  it("keeps a revealed node, and its edges, beyond the depth limit", () => {
    expect(shown({ revealed: ["c"] })).toEqual({
      nodes: ["caller", "root", "a", "b", "errorf", "len", "c"],
      edges: ["caller|root|call", "root|a|call", "root|b|call", "root|len|call", "a|c|dispatch", "a|errorf|call", "a|len|call"],
    });
  });
});

describe("expansion", () => {
  const at = (overrides: Partial<GraphView> = {}) => visibleGraph(TINY, view(overrides));
  const count = (id: string, overrides: Partial<GraphView> = {}) => expandCount(TINY, at(overrides), id, overrides.direction ?? "both");

  it("lists the hidden callees of a callee and the hidden callers of a caller", () => {
    expect([hiddenNeighbours(TINY, at(), "a"), hiddenNeighbours(TINY, at(), "caller")]).toEqual([["c"], ["far"]]);
  });

  it("counts the walked side's total minus the edges drawn there", () => {
    expect({ a: count("a"), aDeep: count("a", { depth: 2 }), caller: count("caller"), callerDeep: count("caller", { depth: 2 }), far: count("far", { depth: 2 }) })
      .toEqual({ a: 3, aDeep: 2, caller: 1, callerDeep: 0, far: 2 });
  });

  it("offers no expansion for a node without a source, however many calls it has", () => {
    expect([count("b"), count("errorf"), count("len")]).toEqual([0, 0, 0]);
  });

  it("offers no expansion on the side the direction filter hides", () => {
    expect(count("root", { direction: "callers" })).toBe(0);
  });

  it("never goes negative when a total is short of the edges drawn", () => {
    const short = { ...TINY, nodes: TINY.nodes.map((entry) => (entry.id === "a" ? { ...entry, out: 1 } : entry)) };
    expect(expandCount(short, visibleGraph(short, view({ depth: 2 })), "a", "both")).toBe(0);
  });

  it("walks an expansion toward the node's outer side: callers for a caller, callees for the root and callees", () => {
    expect(["far", "caller", "root", "a"].map((id) => expansionDirection(requireIn(TINY, id)))).toEqual(["callers", "callers", "callees", "callees"]);
  });
});

describe("mergeGraph", () => {
  // `symbol=a&depth=1&direction=callees`: a is its root, so its depths count from a.
  const fromA: ModuleGraph = {
    roots: ["a"],
    nodes: [
      { ...requireIn(TINY, "a"), depth: 0 },
      { ...requireIn(TINY, "c"), depth: 1 },
      { ...requireIn(TINY, "errorf"), depth: 1 },
      node("d", 1, { in: 1, out: 4 }, { group: "example.com/mod/other" }),
    ],
    edges: [
      { id: "a|c|dispatch", from: "a", to: "c", type: "dispatch", sites: [{ path: "a.go", line: 2, guards: [LONG_GUARD] }] },
      { id: "a|errorf|call", from: "a", to: "errorf", type: "call", sites: [
        { path: "a.go", line: 5, column: 9, guards: ["err != nil"] },
        { path: "a.go", line: 12, column: 9 },
      ] },
      { id: "a|d|call", from: "a", to: "d", type: "call", sites: [{ path: "a.go", line: 14 }] },
    ],
    groups: [{ id: PKG, label: PKG }, { id: "example.com/mod/other", label: "example.com/mod/other" }],
    omitted: {},
  };

  it("adds the new nodes at depths re-signed from the expanded node, keeping the held ones as they were", () => {
    const merged = mergeGraph(TINY, fromA);
    expect(merged.nodes.map(({ id, depth }) => [id, depth])).toEqual([
      ...TINY.nodes.map(({ id, depth }) => [id, depth]),
      ["d", 2],
    ]);
  });

  it("deduplicates edges by from, to and type, concatenating the call sites not already held", () => {
    const merged = mergeGraph(TINY, fromA);
    expect({
      edges: ids(merged.edges).slice(TINY.edges.length),
      errorf: merged.edges.find((edge) => edge.id === "a|errorf|call")!.sites.map((site) => site.line),
      dispatch: merged.edges.find((edge) => edge.id === "a|c|dispatch")!.sites.length,
    }).toEqual({ edges: ["a|d|call"], errorf: [5, 9, 12], dispatch: 1 });
  });

  it("adds the groups the expansion brought and keeps the held response's other fields", () => {
    const merged = mergeGraph(TINY, fromA);
    expect({ groups: merged.groups?.map((group) => group.id), roots: merged.roots, omitted: merged.omitted })
      .toEqual({ groups: [CMD, "fmt", PKG, "builtin", "example.com/mod/other"], roots: ["root"], omitted: { beyond_depth: 2 } });
  });

  it("re-signs a caller expansion further left", () => {
    const fromCaller: ModuleGraph = { roots: ["caller"], omitted: {}, nodes: [
      { ...requireIn(TINY, "caller"), depth: 0 }, { ...requireIn(TINY, "far"), depth: -1 }, node("other", -1, { in: 0, out: 1 }),
    ], edges: [] };
    expect(mergeGraph(TINY, fromCaller).nodes.at(-1)).toMatchObject({ id: "other", depth: -2 });
  });

  it("fails when the expansion's root is not in the held graph", () => {
    expect(() => mergeGraph(TINY, { ...fromA, roots: ["missing"] })).toThrow(/expansion root "missing" is not in the held graph/);
  });
});

describe("navigation", () => {
  it("names a located node by its Explorer id and has none for a node without a source", () => {
    expect([explorerNodeId(requireIn(TINY, "a")), explorerNodeId(requireIn(TINY, "errorf"))])
      .toEqual([`src-a:v1:["method","","${PKG}","","a","","()"]`, undefined]);
  });

  it("fails for a location that does not name its source and identity", () => {
    expect(() => explorerNodeId({ ...requireIn(TINY, "a"), location: { path: "a.go" } })).toThrow(/a has a location without a source_id or identity_key/);
  });

  it("reveals a node's declaration in the Source tab of its own checkout and snapshot", () => {
    expect(nodeRevealPatch(requireIn(TINY, "a"))).toEqual({
      location: CHECKOUT, snapshot: SNAPSHOT, source: "src-a", node: explorerNodeId(requireIn(TINY, "a")),
      line: 1, column: 6, explorerTab: "source", fileSearch: "", symbolSearch: "",
    });
  });

  it("reveals a call site in the caller's file, keeping the selection while the snapshot is the same", () => {
    const edge = TINY.edges.find((entry) => entry.id === "a|errorf|call")!;
    expect([siteRevealPatch(TINY, edge, edge.sites[1]!, SNAPSHOT), siteRevealPatch(TINY, edge, edge.sites[1]!, "snap-0")]).toEqual([
      { location: CHECKOUT, snapshot: SNAPSHOT, source: "src-a", line: 9, column: 9, explorerTab: "source" },
      { location: CHECKOUT, snapshot: SNAPSHOT, source: "src-a", line: 9, column: 9, explorerTab: "source", node: explorerNodeId(requireIn(TINY, "a")) },
    ]);
  });

  it("fails to reveal a site outside the caller's file, or the declaration of a node without a source", () => {
    const edge = TINY.edges.find((entry) => entry.id === "a|errorf|call")!;
    expect([
      () => siteRevealPatch(TINY, edge, { path: "elsewhere.go", line: 1 }, SNAPSHOT),
      () => nodeRevealPatch(requireIn(TINY, "errorf")),
    ].map((reveal) => { try { reveal(); return "revealed"; } catch (error) { return String(error); } })).toEqual([
      "Error: call site elsewhere.go:1 is not in a.go, the file of its caller a",
      "Error: Errorf has no indexed source to reveal",
    ]);
  });
});

describe("graphRoot", () => {
  const selected = (kind: string, identity: string[]) => ({ id: `src-1:v1:${JSON.stringify(identity)}`, source_id: "src-1", kind, symbol: "query.Pipeline.RunExpr" });
  const runExpr = selected("method", ["method", "", "github.com/flanksource/uir/query", "Pipeline", "RunExpr", "", "()"]);

  it("roots the graph at the pinned symbol, else at the selected function or method by its selector", () => {
    expect([graphRoot("sym-1", runExpr), graphRoot("", runExpr), graphRoot("sym-1", undefined)]).toEqual([
      { symbol: "sym-1" }, { selector: "github.com/flanksource/uir/query.Pipeline.RunExpr" }, { symbol: "sym-1" },
    ]);
  });

  it("explains why there is no graph for no selection or a symbol that is not callable", () => {
    expect([graphRoot("", undefined), graphRoot("", { ...selected("type", ["type", "", "github.com/flanksource/uir/query", "Pipeline", "", "", ""]), symbol: "query.Pipeline" })]).toEqual([
      { empty: "Select a function or method to draw its call graph." },
      { empty: "query.Pipeline is a type: only a function or method has a call graph." },
    ]);
  });
});

describe("graph facts", () => {
  it("reports a node's totals only on the side the response walked", () => {
    const calleesOnly = { ...TINY, nodes: TINY.nodes.filter((entry) => entry.depth >= 0) };
    expect([
      walkedTotals(TINY, requireIn(TINY, "root")),
      walkedTotals(TINY, requireIn(TINY, "a")),
      walkedTotals(TINY, requireIn(TINY, "caller")),
      walkedTotals(calleesOnly, requireIn(calleesOnly, "root")),
    ]).toEqual([{ callers: 1, callees: 3 }, { callees: 5 }, { callers: 1 }, { callees: 3 }]);
  });
});

describe("toDiagram", () => {
  const options: DiagramOptions = { guardLabel: "innermost", truncateAt: 24, grouped: true };
  const glyphs: DiagramGlyphs = { node: (glyph, words) => `[${glyph}|${words}]`, dispatch: () => "[dispatch]", group: (caption) => `[group|${caption}]` };
  const diagram = (overrides: Partial<DiagramOptions> = {}, viewOverrides: Partial<GraphView> = { revealed: ["c"] }) =>
    toDiagram({ graph: TINY, visible: visibleGraph(TINY, view(viewOverrides)), direction: "both", options: { ...options, ...overrides }, glyphs });
  const FUNC = "[func|function]";

  it("maps nodes to icons, tooltips, levels, groups, tones, muting and expand counts", () => {
    expect(diagram().nodes).toEqual([
      { id: "root", label: "root", icon: FUNC, title: `function root in ${PKG}`, level: 0, group: PKG, tone: "info" },
      { id: "a", label: "a", icon: FUNC, title: `function a in ${PKG}`, level: 1, group: PKG, expandCount: 2 },
      { id: "b", label: "fn", icon: "[unresolved|unresolved call]", title: `unresolved call fn in ${PKG}`, level: 1, group: PKG, tone: "warning", muted: true },
      { id: "c", label: "c", icon: FUNC, title: `function c in ${PKG}`, level: 2, group: PKG },
      { id: "caller", label: "caller", icon: FUNC, title: `function caller in ${CMD}`, level: -1, group: CMD, expandCount: 1 },
      { id: "errorf", label: "Errorf", icon: "[external_func|external function]", title: "external function Errorf in fmt", level: 1, group: "fmt", muted: true },
      { id: "len", label: "len", icon: "[builtin|builtin]", title: "builtin len in builtin", level: 1, group: "builtin", muted: true },
    ]);
  });

  it("orders nodes so the root's package comes first, then indexed packages, then the others", () => {
    expect(diagram({}, { depth: 2 }).nodes.map((entry) => entry.group)).toEqual([PKG, PKG, PKG, PKG, CMD, CMD, "fmt", "builtin"]);
  });

  it("maps edges to dashed, labelled and titled diagram edges, with an icon on a dispatch edge", () => {
    expect(diagram().edges).toEqual([
      { id: "caller|root|call", from: "caller", to: "root", dashed: false, label: "×2" },
      { id: "root|a|call", from: "root", to: "a", dashed: true, label: "2 conditions ×2", title: `x > 0\n!(x > 0) ∧ ${LONG_GUARD}` },
      { id: "root|b|call", from: "root", to: "b", dashed: false },
      { id: "root|len|call", from: "root", to: "len", dashed: false, label: "×2", title: "x > 0" },
      { id: "a|c|dispatch", from: "a", to: "c", dashed: true, tone: "info", icon: "[dispatch]", iconLabel: "via interface", label: "a.very.long.conditio…", title: LONG_GUARD },
      { id: "a|errorf|call", from: "a", to: "errorf", dashed: true, label: "err != nil ×2", title: "err != nil\ny ∧ err != nil" },
      { id: "a|len|call", from: "a", to: "len", dashed: false },
    ]);
  });

  it("captions each group with its shortened package path and keeps the full path as its tooltip", () => {
    expect(diagram().groups).toEqual([
      { id: CMD, label: "[group|cmd/app]", title: CMD },
      { id: "fmt", label: "[group|fmt]", title: "fmt" },
      { id: PKG, label: "[group|mod/pkg]", title: PKG },
      { id: "builtin", label: "[group|builtin]", title: "builtin" },
    ]);
  });

  it("drops groups when grouping is off", () => {
    const ungrouped = diagram({ grouped: false });
    expect({ groups: ungrouped.groups, grouped: ungrouped.nodes.filter((entry) => entry.group !== undefined) }).toEqual({ groups: [], grouped: [] });
  });
});

describe("columnGap", () => {
  const nodes = [{ id: "root", label: "root", level: 0 }, { id: "a", label: "a", level: 1 }, { id: "b", label: "b", level: 1 }];
  const twentyChars = "x".repeat(20);

  it("fits the longest pill between two columns: 5.2px a character plus 22px", () => {
    expect(columnGap({ nodes, edges: [{ id: "root-a", from: "root", to: "a", label: "short" }, { id: "root-b", from: "root", to: "b", label: twentyChars }] })).toBe(126);
  });

  it("ignores the pill of an edge inside one column, which hangs outside the column instead", () => {
    expect(columnGap({ nodes, edges: [{ id: "root-a", from: "root", to: "a", label: twentyChars }, { id: "a-b", from: "a", to: "b", label: "x".repeat(40) }] })).toBe(126);
  });

  it("counts an edge's icon as three characters", () => {
    expect(columnGap({ nodes, edges: [{ id: "root-a", from: "root", to: "a", label: twentyChars, icon: "[dispatch]" }] })).toBe(142);
  });

  it("is never narrower than 96px, the room an edge needs to curve", () => {
    expect(columnGap({ nodes, edges: [{ id: "root-a", from: "root", to: "a" }] })).toBe(96);
  });
});
