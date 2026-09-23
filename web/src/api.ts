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
  state: string;
  revision: string;
  started_at: string;
  completed_at?: string;
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
  to_root_key?: string;
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
export type ModuleSourceContent = { path: string; content: string; origin: "local" | "git"; revision: string; snapshot_id: string };
export type ModuleQueryRow = { kind: string; root: string; symbol: string; location: string; source: string; snapshot_id: string };
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

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(path, { headers: { Accept: "application/json", ...(options?.body ? { "Content-Type": "application/json" } : {}) }, ...options });
  if (!response.ok) throw new Error(`${response.status} ${response.statusText}: ${await response.text()}`);
  return response.json() as Promise<T>;
}

function moduleURL(operation: string, params?: Record<string, string>): string {
  const query = new URLSearchParams(params);
  return `/api/v1/modules${operation ? `/${operation}` : ""}${query.size ? `?${query}` : ""}`;
}

export function listModuleRoots(): Promise<ModuleRoot[]> {
  return request(moduleURL(""));
}

export function listModuleLocations(root: string): Promise<ModuleLocation[]> {
  return request(moduleURL("locations", { root }));
}

export function listModuleSnapshots(root: string, location: string, offset: number): Promise<Page<ModuleSnapshot>> {
  return request(moduleURL("snapshots", { root, location, offset: String(offset), limit: "100" }));
}

export function browseModule(snapshot: string): Promise<ModuleBrowse> {
  return request(moduleURL("browse", { snapshot }));
}

export function readModuleSource(snapshot: string, path: string): Promise<ModuleSourceContent> {
  return request(moduleURL("content", { snapshot, path }));
}

export function runModuleQuery(expression: string, root: string, snapshot: string): Promise<ModuleQueryRow[]> {
  return request(moduleURL("query"), { method: "POST", body: JSON.stringify({ args: [expression], root, snapshot }) });
}

export function addModules(path: string, includeTests: boolean): Promise<ModuleIndexResult[]> {
  return request(moduleURL("add"), { method: "POST", body: JSON.stringify({ args: [path], "include-tests": includeTests, "no-workspace-uses": true }) });
}

export function reindexModules(path: string, includeTests: boolean, force: boolean): Promise<ModuleIndexResult[]> {
  return request(moduleURL("reindex"), { method: "POST", body: JSON.stringify({ args: [path], "include-tests": includeTests, force }) });
}
