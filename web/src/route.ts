export type Route = {
  view: "overview" | "explorer" | "nodes" | "query";
  module: string;
  location: string;
  snapshot: string;
  source: string;
  node: string;
  search: string;
  expression: string;
  offset: number;
};

export function readRoute(location: Pick<Location, "pathname" | "search"> = window.location): Route {
  const params = new URLSearchParams(location.search);
  const path = location.pathname.slice(1);
  return {
    view: path === "explorer" || path === "nodes" || path === "query" ? path : "overview",
    module: params.get("module") ?? "",
    location: params.get("location") ?? "",
    snapshot: params.get("snapshot") ?? "",
    source: params.get("source") ?? "",
    node: params.get("node") ?? "",
    search: params.get("search") ?? "",
    expression: params.get("expression") ?? "",
    offset: Number(params.get("offset") ?? 0),
  };
}

export function routeURL(route: Route): string {
  const params = new URLSearchParams();
  for (const key of ["module", "location", "snapshot", "source", "node", "search", "expression"] as const) {
    if (route[key]) params.set(key, route[key]);
  }
  if (route.offset > 0) params.set("offset", String(route.offset));
  const query = params.toString();
  return `/${route.view === "overview" ? "" : route.view}${query ? `?${query}` : ""}`;
}
