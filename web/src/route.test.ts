import { describe, expect, it } from "vitest";
import { ALL_MODULES, applyRoutePatch, readRoute, routeURL, scopePatch, scopeValue } from "./route";

const root = { root_key: "example.org/service", location: "/checkout/service", snapshot_id: "head-1" };
const selectionReset = { source: "", node: "", fileSearch: "", symbolSearch: "", compareFrom: "", compareTo: "", logCommit: "", includeTests: false, offset: 0, graphRoot: "" };

describe("scope", () => {
  it.each([
    ["every module for an empty module", "", ALL_MODULES],
    ["the module root key for a chosen module", root.root_key, root.root_key],
  ])("selects %s", (_, module, expected) => {
    expect(scopeValue({ module })).toBe(expected);
  });

  it("scopes to the root's primary checkout head and resets the selection", () => {
    expect(scopePatch(root)).toEqual({ module: root.root_key, location: root.location, snapshot: root.snapshot_id, ...selectionReset });
  });

  it("keeps the view, expression and open selection when the scope widens to every module", () => {
    const current = readRoute({ pathname: "/query", search: `?module=${root.root_key}&location=${root.location}&snapshot=${root.snapshot_id}&source=src-1&expression=nodes` });
    expect(scopePatch(null)).toEqual({ module: "" });
    expect(applyRoutePatch(current, scopePatch(null))).toMatchObject({ view: "query", expression: "nodes", module: "", location: root.location, snapshot: root.snapshot_id, source: "src-1" });
  });
});

it("opens the task manager at its own route", () => {
  const route = readRoute({ pathname: "/tasks", search: "?module=example.org%2Fservice" });
  expect(route.view).toBe("tasks");
  expect(routeURL(route)).toBe("/tasks?module=example.org%2Fservice");
});

it("round trips a Git comparison and selected log commit through the history URL", () => {
  const route = readRoute({ pathname: "/history", search: "?module=example.org%2Fservice&compareFrom=main&compareTo=pr%3A12&logCommit=abc123&historyGroup=file&historyLayout=sidebar&historySearch=fix" });
  expect(route).toMatchObject({ view: "history", compareFrom: "main", compareTo: "pr:12", logCommit: "abc123", historyGroup: "file", historyLayout: "sidebar", historySearch: "fix" });
  expect(readRoute(new URL(routeURL(route), "http://localhost"))).toEqual(route);
});

it("round trips the selected module, checkout, snapshot, source, node, position, filters, and query through the URL", () => {
  const original = readRoute({ pathname: "/query", search: "?module=example.org%2Fservice&location=%2Fcheckout%2Fservice&snapshot=s1&source=f1&node=n1&line=42&column=7&fileSearch=internal&symbolSearch=Handler&expression=nodes&offset=100" });
  expect(original).toMatchObject({ line: 42, column: 7 });
  const url = new URL(routeURL(original), "http://localhost");
  expect(readRoute(url)).toEqual(original);
});

it("clears a revealed position when the source or node changes without a new one", () => {
  const current = readRoute({ pathname: "/explorer", search: "?snapshot=s1&source=f1&node=n1&line=42&column=7" });
  expect([
    applyRoutePatch(current, { source: "f2" }),
    applyRoutePatch(current, { node: "n2" }),
    applyRoutePatch(current, { source: "f2", line: 9, column: 3 }),
    applyRoutePatch(current, { symbolSearch: "Run" }),
  ].map(({ source, node, line, column }) => ({ source, node, line, column }))).toEqual([
    { source: "f2", node: "n1", line: 0, column: 0 },
    { source: "f1", node: "n2", line: 0, column: 0 },
    { source: "f2", node: "n1", line: 9, column: 3 },
    { source: "f1", node: "n1", line: 42, column: 7 },
  ]);
});

it("moves legacy Symbols links into the explorer with their symbol search", () => {
  const route = readRoute({ pathname: "/nodes", search: "?snapshot=s1&source=f1&node=n1&search=Handler" });
  expect(route).toMatchObject({ view: "explorer", snapshot: "s1", source: "f1", node: "n1", symbolSearch: "Handler", fileSearch: "" });
  expect(routeURL(route)).toBe("/explorer?snapshot=s1&source=f1&node=n1&symbolSearch=Handler");
});

it("keeps a legacy explorer path search as a file search", () => {
  const route = readRoute({ pathname: "/explorer", search: "?snapshot=s1&search=internal" });
  expect(route).toMatchObject({ fileSearch: "internal", symbolSearch: "" });
});

it("defaults to file navigation and round trips symbol navigation through the Explorer URL", () => {
  const files = readRoute({ pathname: "/explorer", search: "" });
  expect(files.explorerMode).toBe("files");
  expect(routeURL(files)).toBe("/explorer");
  const symbols = applyRoutePatch(files, { explorerMode: "symbols" });
  expect(readRoute(new URL(routeURL(symbols), "http://localhost"))).toEqual(symbols);
  expect(routeURL(symbols)).toBe("/explorer?explorerMode=symbols");
});

describe("call graph tab", () => {
  it("opens the Explorer on the Source tab with a depth-2 graph of both directions and the server's exclusions, none of it in the URL", () => {
    const route = readRoute({ pathname: "/explorer", search: "?snapshot=s1" });
    expect({ explorerTab: route.explorerTab, graphDir: route.graphDir, graphDepth: route.graphDepth, graphRoot: route.graphRoot, graphExclude: route.graphExclude, url: routeURL(route) })
      .toEqual({ explorerTab: "source", graphDir: "both", graphDepth: 2, graphRoot: "", graphExclude: "", url: "/explorer?snapshot=s1" });
  });

  it("round trips the tab, direction, depth, pinned root and exclusions through the URL", () => {
    const route = readRoute({ pathname: "/explorer", search: "?snapshot=s1&explorerTab=call-graph&graphDir=callers&graphDepth=4&graphRoot=abc123&graphExclude=std%2Cgorm.io%2F...%2Cexternal" });
    expect(route).toMatchObject({ explorerTab: "call-graph", graphDir: "callers", graphDepth: 4, graphRoot: "abc123", graphExclude: "std,gorm.io/...,external" });
    expect(readRoute(new URL(routeURL(route), "http://localhost"))).toEqual(route);
  });

  it.each([
    ["an unknown tab", "explorerTab=graph", { explorerTab: "source" }],
    ["an unknown direction", "graphDir=up", { graphDir: "both" }],
  ])("falls back to the default for %s", (_, search, expected) => {
    expect(readRoute({ pathname: "/explorer", search: `?${search}` })).toMatchObject(expected);
  });
});

it("round trips included and excluded symbol facets with the tree text filter", () => {
  const route = readRoute({ pathname: "/explorer", search: "?explorerMode=symbols&symbolSearch=Save&symbolVisibility=%2Bexported%2C-internal&symbolKinds=%2Bmethod%2C-field" });
  expect(route).toMatchObject({ symbolSearch: "Save", symbolVisibility: "+exported,-internal", symbolKinds: "+method,-field" });
  expect(readRoute(new URL(routeURL(route), "http://localhost"))).toEqual(route);
});
