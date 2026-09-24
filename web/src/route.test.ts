import { describe, expect, it } from "vitest";
import { ALL_MODULES, applyRoutePatch, readRoute, routeURL, scopePatch, scopeValue } from "./route";

const root = { root_key: "example.org/service", location: "/checkout/service", snapshot_id: "head-1" };
const selectionReset = { source: "", node: "", fileSearch: "", symbolSearch: "", offset: 0 };

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
