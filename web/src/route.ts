import type { ModuleGraphDirection, ModuleRoot } from "./api";

export const ALL_MODULES = "*";
/** The depth the Explorer opens a call graph at (graph.DefaultDepth). */
export const DEFAULT_GRAPH_DEPTH = 2;
const GRAPH_DIRECTIONS: readonly ModuleGraphDirection[] = ["callees", "callers", "both"];

export type ScopeRoot = Pick<ModuleRoot, "root_key" | "location" | "snapshot_id">;

export type Route = {
  view: "overview" | "explorer" | "query" | "history" | "tasks";
  explorerMode: "files" | "symbols";
  module: string;
  location: string;
  snapshot: string;
  source: string;
  node: string;
  line: number;
  column: number;
  fileSearch: string;
  symbolSearch: string;
  symbolVisibility: string;
  symbolKinds: string;
  expression: string;
  compareFrom: string;
  compareTo: string;
  logCommit: string;
  diffVisibility: string;
  includeTests: boolean;
  historyGroup: "package" | "file" | "change";
  historyLayout: "inline" | "sidebar";
  historySearch: string;
  offset: number;
  explorerTab: "source" | "call-graph";
  graphDir: ModuleGraphDirection;
  graphDepth: number;
  /** The pinned call graph root, a graph symbol id; empty follows the selected symbol. */
  graphRoot: string;
  /** Comma-separated exclusion patterns; empty takes the server's defaults. */
  graphExclude: string;
};

export function readRoute(location: Pick<Location, "pathname" | "search"> = window.location): Route {
  const params = new URLSearchParams(location.search);
  const path = location.pathname.slice(1);
  return {
    view: path === "explorer" || path === "nodes" ? "explorer" : path === "query" ? "query" : path === "history" ? "history" : path === "tasks" ? "tasks" : "overview",
    explorerMode: params.get("explorerMode") === "symbols" ? "symbols" : "files",
    module: params.get("module") ?? "",
    location: params.get("location") ?? "",
    snapshot: params.get("snapshot") ?? "",
    source: params.get("source") ?? "",
    node: params.get("node") ?? "",
    line: Number(params.get("line") ?? 0),
    column: Number(params.get("column") ?? 0),
    fileSearch: params.get("fileSearch") ?? (path === "explorer" ? params.get("search") ?? "" : ""),
    symbolSearch: params.get("symbolSearch") ?? (path === "nodes" ? params.get("search") ?? "" : ""),
    symbolVisibility: params.get("symbolVisibility") ?? "",
    symbolKinds: params.get("symbolKinds") ?? "",
    expression: params.get("expression") ?? "",
    compareFrom: params.get("compareFrom") ?? "",
    compareTo: params.get("compareTo") ?? "",
    logCommit: params.get("logCommit") ?? "",
    diffVisibility: params.get("diffVisibility") ?? "",
    includeTests: params.get("includeTests") === "1",
    historyGroup: params.get("historyGroup") === "file" || params.get("historyGroup") === "change" ? params.get("historyGroup") as "file" | "change" : "package",
    historyLayout: params.get("historyLayout") === "sidebar" ? "sidebar" : "inline",
    historySearch: params.get("historySearch") ?? "",
    offset: Number(params.get("offset") ?? 0),
    explorerTab: params.get("explorerTab") === "call-graph" ? "call-graph" : "source",
    graphDir: GRAPH_DIRECTIONS.find((direction) => direction === params.get("graphDir")) ?? "both",
    graphDepth: Number(params.get("graphDepth") ?? DEFAULT_GRAPH_DEPTH),
    graphRoot: params.get("graphRoot") ?? "",
    graphExclude: params.get("graphExclude") ?? "",
  };
}

export function routeURL(route: Route): string {
  const params = new URLSearchParams();
  for (const key of ["module", "location", "snapshot", "source", "node", "fileSearch", "symbolSearch", "symbolVisibility", "symbolKinds", "expression", "compareFrom", "compareTo", "logCommit", "diffVisibility", "historySearch", "graphRoot", "graphExclude"] as const) {
    if (route[key]) params.set(key, route[key]);
  }
  if (route.historyGroup !== "package") params.set("historyGroup", route.historyGroup);
  if (route.explorerMode === "symbols") params.set("explorerMode", "symbols");
  if (route.historyLayout !== "inline") params.set("historyLayout", route.historyLayout);
  if (route.explorerTab !== "source") params.set("explorerTab", route.explorerTab);
  if (route.graphDir !== "both") params.set("graphDir", route.graphDir);
  if (route.graphDepth !== DEFAULT_GRAPH_DEPTH) params.set("graphDepth", String(route.graphDepth));
  for (const key of ["line", "column", "offset"] as const) {
    if (route[key] > 0) params.set(key, String(route[key]));
  }
  if (route.includeTests) params.set("includeTests", "1");
  const query = params.toString();
  return `/${route.view === "overview" ? "" : route.view}${query ? `?${query}` : ""}`;
}

// applyRoutePatch drops a revealed line and column once the source or node changes, unless the patch
// reveals a new position itself.
export function applyRoutePatch(current: Route, patch: Partial<Route>): Route {
  const moved = ("source" in patch || "node" in patch) && !("line" in patch);
  return { ...current, ...(moved ? { line: 0, column: 0 } : {}), ...patch };
}

// scopeValue names every module with a sentinel, since the scope picker treats "" as no selection.
export function scopeValue(route: Pick<Route, "module">): string {
  return route.module || ALL_MODULES;
}

// scopePatch widens to every module without disturbing the open selection, since it still belongs to
// the scope; a root scope resets to that root's primary head because the selection may not.
export function scopePatch(root: ScopeRoot | null): Partial<Route> {
  if (!root) return { module: "" };
  return { module: root.root_key, location: root.location, snapshot: root.snapshot_id,
    source: "", node: "", fileSearch: "", symbolSearch: "", compareFrom: "", compareTo: "", logCommit: "", includeTests: false, offset: 0, graphRoot: "" };
}
