import type { ModuleRoot } from "./api";

export const ALL_MODULES = "*";

export type ScopeRoot = Pick<ModuleRoot, "root_key" | "location" | "snapshot_id">;

export type Route = {
  view: "overview" | "explorer" | "query" | "tasks";
  module: string;
  location: string;
  snapshot: string;
  source: string;
  node: string;
  line: number;
  column: number;
  fileSearch: string;
  symbolSearch: string;
  expression: string;
  offset: number;
};

export function readRoute(location: Pick<Location, "pathname" | "search"> = window.location): Route {
  const params = new URLSearchParams(location.search);
  const path = location.pathname.slice(1);
  return {
    view: path === "explorer" || path === "nodes" ? "explorer" : path === "query" ? "query" : path === "tasks" ? "tasks" : "overview",
    module: params.get("module") ?? "",
    location: params.get("location") ?? "",
    snapshot: params.get("snapshot") ?? "",
    source: params.get("source") ?? "",
    node: params.get("node") ?? "",
    line: Number(params.get("line") ?? 0),
    column: Number(params.get("column") ?? 0),
    fileSearch: params.get("fileSearch") ?? (path === "explorer" ? params.get("search") ?? "" : ""),
    symbolSearch: params.get("symbolSearch") ?? (path === "nodes" ? params.get("search") ?? "" : ""),
    expression: params.get("expression") ?? "",
    offset: Number(params.get("offset") ?? 0),
  };
}

export function routeURL(route: Route): string {
  const params = new URLSearchParams();
  for (const key of ["module", "location", "snapshot", "source", "node", "fileSearch", "symbolSearch", "expression"] as const) {
    if (route[key]) params.set(key, route[key]);
  }
  for (const key of ["line", "column", "offset"] as const) {
    if (route[key] > 0) params.set(key, String(route[key]));
  }
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
    source: "", node: "", fileSearch: "", symbolSearch: "", offset: 0 };
}
