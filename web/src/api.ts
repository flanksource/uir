import { parseServerTiming, type ServerTimingMetric } from "@flanksource/clicky-ui/data";

export type TimedResponse<T> = { data: T; timing: ServerTimingMetric[] };

export type ModuleRoot = {
  root_key: string;
  name: string;
  location: string;
  snapshot_id: string;
  head_version: number;
};

export type ModuleLocation = {
  id: string;
  root_key: string;
  canonical_path: string;
  kind: string;
  mount_path: string;
  primary: boolean;
  head_snapshot_id: string;
  head_version: number;
};

export type ModuleSnapshot = {
  id: string;
  root_key: string;
  canonical_path: string;
  base_snapshot_id?: string;
  revision: string;
  worktree_state: "clean" | "dirty" | "unknown";
  coverage: "indexed" | "partial" | "syntax" | "excluded";
  started_at: string;
  completed_at: string;
  head: boolean;
  head_version?: number;
};

export type ModuleSource = {
  id: string;
  root_key: string;
  location: string;
  snapshot_id: string;
  path: string;
  package_path: string;
  content_hash: string;
  size_bytes: number;
};

export type ModuleCall = {
  to_identifier: Record<string, unknown>;
  resolvable: boolean;
  statement_path: string;
  line?: number;
  text: string;
};

export type ModuleNode = {
  id: string;
  source_id: string;
  path: string;
  symbol: string;
  node_type: string;
  identifier: { module?: string; package?: string; type?: string; method?: string; field?: string; signature?: string; node_type?: string };
  parent_identity?: string;
  child_slot: string;
  ordinal: number;
  payload: unknown;
  semantic_hash: string;
  field?: unknown;
  line?: number;
  end_line?: number;
  column?: number;
  calls: ModuleCall[];
};

export type ModuleBrowse = { sources: ModuleSource[]; nodes: ModuleNode[] };
export type ModuleHead = { root_key: string; name: string; location: string; snapshot_id: string; sources: ModuleSource[] };
export type ModuleSourceContent = { path: string; content: string; origin: "local" | "git"; revision: string; snapshot_id: string };
export type ModuleQueryRow = {
  kind: string;
  symbol: string;
  root: string;
  location: string;
  source: string;
  snapshot_id: string;
  path?: string;
  package_path?: string;
  line?: number;
  column?: number;
  end_line?: number;
  end_column?: number;
  role?: string;
  symbol_id?: string;
  enclosing_id?: string;
  enclosing_key?: string;
  coverage?: string;
  dispatch?: boolean;
  depth?: number;
};
export type ModuleQuerySymbol = {
  id: string;
  module_key: string;
  package_path: string;
  kind: string;
  owner_id?: string;
  owner?: string;
  name: string;
  query_name: string;
  visibility: string;
  parameter_types: unknown;
};
export type ModuleQueryCoverage = { root_key: string; location: string; snapshot_id: string; package_path: string; coverage: string; diagnostics: number };
export type ModuleQueryResult = {
  operation: unknown;
  total: number;
  matches: ModuleQueryRow[];
  declarations: ModuleQueryRow[];
  symbols: ModuleQuerySymbol[];
  coverage: ModuleQueryCoverage[];
  stages: { name: string; value: string }[];
  path?: { symbols: ModuleQuerySymbol[]; calls: ModuleQueryRow[] };
};
export type ModuleIndexResult = {
  root_key: string;
  location: string;
  snapshot_id: string;
  head_version: number;
  files: number;
  parsed_files: number;
  reused_files: number;
  unchanged: boolean;
};
export type Page<T> = { data: T[]; page: { limit: number; offset: number; total: number } };
export type SystemInfo = {
  backend_version: string;
  database_type: string;
  database_version: string;
  database_location: string;
  database_size_bytes: number;
};

async function requestResponse(path: string, options?: RequestInit): Promise<Response> {
  const response = await fetch(path, { headers: { Accept: "application/json", ...(options?.body ? { "Content-Type": "application/json" } : {}) }, ...options });
  if (!response.ok) throw new Error(`${response.status} ${response.statusText}: ${await response.text()}`);
  return response;
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  return (await requestResponse(path, options)).json() as Promise<T>;
}

async function timedRequest<T>(path: string, options?: RequestInit): Promise<TimedResponse<T>> {
  const response = await requestResponse(path, options);
  const header = response.headers.get("Server-Timing");
  if (!header) throw new Error(`Server-Timing header missing from ${path}`);
  return { data: await response.json() as T, timing: parseServerTiming(header) };
}

function moduleURL(operation: string, params?: Record<string, string>): string {
  const query = new URLSearchParams(params);
  return `/api/v1/modules${operation ? `/${operation}` : ""}${query.size ? `?${query}` : ""}`;
}

export function listModuleRoots(): Promise<ModuleRoot[]> {
  return request(moduleURL(""));
}

export function listModuleHeads(): Promise<ModuleHead[]> {
  return request(moduleURL("heads"));
}

export function getSystemInfo(): Promise<SystemInfo> {
  return request("/api/v1/system/info");
}

export function listModuleLocations(root: string): Promise<ModuleLocation[]> {
  return request(moduleURL("locations", { root }));
}

export function listModuleSnapshots(root: string, location: string, offset: number): Promise<Page<ModuleSnapshot>> {
  return request(moduleURL("snapshots", { root, location, offset: String(offset), limit: "100" }));
}

export function browseModule(snapshot: string): Promise<TimedResponse<ModuleBrowse>> {
  return timedRequest(moduleURL("browse", { snapshot }));
}

export function readModuleSource(snapshot: string, path: string): Promise<ModuleSourceContent> {
  return request(moduleURL("content", { snapshot, path }));
}

export async function runModuleQuery(expression: string, root: string, snapshot: string): Promise<TimedResponse<ModuleQueryResult>> {
  const result = await timedRequest<unknown>(moduleURL("query"), { method: "POST", body: JSON.stringify({ args: [expression], root, snapshot }) });
  if (!isQueryResult(result.data)) throw new Error(`Query response is not a result envelope: ${JSON.stringify(result.data).slice(0, 200)}`);
  return { data: result.data, timing: result.timing };
}

export function suggestModuleSymbols(prefix: string, root: string, snapshot: string, signal?: AbortSignal): Promise<ModuleQuerySymbol[]> {
  return request(moduleURL("suggest", { prefix, root, snapshot }), { signal });
}

export function suggestTypedSelectors(prefix: string, root: string, snapshot: string, signal?: AbortSignal): Promise<string[]> {
  return request(moduleURL("suggest-selectors", { prefix, root, snapshot }), { signal });
}

function isQueryResult(value: unknown): value is ModuleQueryResult {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const result = value as Record<string, unknown>;
  return typeof result.total === "number" && ["matches", "declarations", "symbols", "coverage", "stages"].every((key) => Array.isArray(result[key]));
}

export function addModules(path: string, includeTests: boolean): Promise<ModuleIndexResult[]> {
  return request(moduleURL("add"), { method: "POST", body: JSON.stringify({ args: [path], "include-tests": includeTests, "no-workspace-uses": true }) });
}

export function reindexModules(path: string, includeTests: boolean, force: boolean): Promise<ModuleIndexResult[]> {
  return request(moduleURL("reindex"), { method: "POST", body: JSON.stringify({ args: [path], "include-tests": includeTests, force }) });
}
