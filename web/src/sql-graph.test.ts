import { describe, expect, it } from "vitest";
import type { ModuleGraph, ModuleGraphEdge, ModuleGraphNode } from "./api";
import { canRevealSites, mergeGraph, siteRevealPatch } from "./call-graph-model";

const node: ModuleGraphNode = { id: "route", label: "Orders", kind: "http.route", identifier: {}, depth: 0, in: 0, out: 2 };
const edge: ModuleGraphEdge = { id: "selects", from: "route", to: "handler", type: "reference", kind: "http.handled_by", sites: [] };
const graph: ModuleGraph = { roots: [node.id], nodes: [node, { ...node, id: "handler", depth: 1 }], edges: [edge], omitted: {} };

describe("SQL graph witnesses", () => {
  it("preserves parallel native names when merging the same endpoints and coarse type", () => {
    const expansion = { ...graph, edges: [{ ...edge, id: "requests", kind: "http.requests" }] };
    expect(mergeGraph(graph, expansion).edges.map((item) => item.kind)).toEqual(["http.handled_by", "http.requests"]);
  });

  it("opens a native witness in its recorded snapshot and file without a source declaration", () => {
    const site = { path: "routes.yaml", line: 3, column: 2, location: { root_key: "orders", checkout_path: "producer://orders", snapshot_id: "snapshot", source_id: "source", path: "routes.yaml" } };
    const witnessed = { ...edge, sites: [site] };
    expect(canRevealSites(graph, witnessed)).toBe(true);
    expect(siteRevealPatch(graph, witnessed, site, "previous")).toEqual({ location: "producer://orders", snapshot: "snapshot", source: "source", line: 3, column: 2, explorerTab: "source", node: "", fileSearch: "", symbolSearch: "" });
  });
});
