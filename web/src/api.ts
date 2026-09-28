import { parseServerTiming, type ServerTimingMetric } from "@flanksource/clicky-ui/data";

export type TimedResponse<T> = { data: T; timing: ServerTimingMetric[] };

export class ApiError extends Error {
  readonly name = "ApiError";

  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly hint: string,
    readonly context: Record<string, unknown>,
    readonly trace: string,
  ) {
    super(message);
  }
}

export function errorMessage(reason: unknown): string {
  if (reason instanceof ApiError) return reason.hint ? `${reason.message}\nHint: ${reason.hint}` : reason.message;
  return reason instanceof Error ? reason.message : String(reason);
}

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
  git_commit?: string;
  module_version?: string;
  last_modified_at?: string;
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
export type ModuleSourceContent = { path: string; content: string; origin: "local" | "git" | "snapshot"; revision: string; snapshot_id: string };
export type ModuleDependency = { module_path: string; declared_version: string; indirect: boolean; replace_path?: string; replace_version?: string; selected_version?: string; target_snapshot_id?: string; target_root_key?: string; target_location?: string; unresolved_reason?: string };
export type ModuleDependencies = { captured: boolean; items: ModuleDependency[] };
export type ModuleRefactorRequest = { snapshot: string; source: string; node?: string; action: "rename" | "move"; newName?: string; destination?: string };
export type ModuleRefactorPreview = { diff: string; preview_hash: string; files: string[] };
export type ModuleRefactorApply = { applied: boolean; files: string[]; snapshots: ModuleIndexResult[]; index_error?: string };
export type ModuleQueryRow = {
  kind: string;
  node_kind?: string;
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
  relation?: string;
  source_id?: string;
  source_name?: string;
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
export type MissingHeadWarning = { root_key: string; location: string; message: string };
export type ItemsWithWarnings<T> = { items: T[]; warnings: MissingHeadWarning[] };
export type ModuleQueryResult = {
  operation: unknown;
  total: number;
  matches: ModuleQueryRow[];
  declarations: ModuleQueryRow[];
  symbols: ModuleQuerySymbol[];
  coverage: ModuleQueryCoverage[];
  warnings: MissingHeadWarning[];
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
export type GitRef = { name: string; commit: string };
export type GitCommit = { commit: string; parents: string[]; subject: string; authored_at: string };
export type GitHistory = { root_key: string; location: string; branches: GitRef[]; commits: GitCommit[];
  pull_requests: { number: number; commit: string }[]; pull_request_error?: string };
export type SymbolChange = { class: "added" | "removed" | "signature" | "body" | "moved"; kind: string;
  owner?: string; name: string; visibility: string; shape_before?: string; shape_after?: string;
  shape_diff?: { op: string; text: string; paired?: boolean; tokens: { op: string; text: string }[] }[];
  path_before?: string; path_after?: string; lines?: { added: number; removed: number }; coverage?: string[]; note?: string };
export type SymbolDiff = { root_key: string; from: { commit: string; snapshot_id: string; worktree_state: string };
  to: { commit: string; snapshot_id: string; worktree_state: string }; visibility: string; stat: boolean;
  lines_error?: string; packages: { path: string; lines?: { added: number; removed: number };
    files: { path: string; status: string; coverage?: string[]; excluded?: string;
      lines?: { added: number; removed: number }; lines_error?: string; hidden_rows?: number; rows: SymbolChange[] }[] }[] };
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
  if (!response.ok) {
    const body = await response.text();
    if (response.headers.get("Content-Type")?.includes("json")) {
      const diagnostic: unknown = JSON.parse(body);
      if (typeof diagnostic !== "object" || diagnostic === null || Array.isArray(diagnostic)
        || !("code" in diagnostic) || typeof diagnostic.code !== "string"
        || !("message" in diagnostic) || typeof diagnostic.message !== "string") {
        throw new Error(`Invalid API error response from ${path}: ${body}`);
      }
      throw new ApiError(response.status, diagnostic.code, diagnostic.message,
        "hint" in diagnostic && typeof diagnostic.hint === "string" ? diagnostic.hint : "",
        "context" in diagnostic && typeof diagnostic.context === "object" && diagnostic.context !== null && !Array.isArray(diagnostic.context)
          ? diagnostic.context as Record<string, unknown> : {},
        "trace" in diagnostic && typeof diagnostic.trace === "string" ? diagnostic.trace : "");
    }
    throw new Error(body.trim() || `${response.status} ${response.statusText}`);
  }
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

export function listModuleHeads(): Promise<ItemsWithWarnings<ModuleHead>> {
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

export function listModuleDependencies(snapshot: string): Promise<ModuleDependencies> {
  return request(moduleURL("dependencies", { snapshot }));
}

function refactorBody(refactor: ModuleRefactorRequest, previewHash?: string): string {
  return JSON.stringify({ snapshot: refactor.snapshot, source: refactor.source,
    ...(refactor.node ? { node: refactor.node } : {}), action: refactor.action,
    ...(refactor.newName ? { "new-name": refactor.newName } : {}),
    ...(refactor.destination ? { destination: refactor.destination } : {}),
    ...(previewHash ? { "preview-hash": previewHash } : {}),
  });
}

export function previewModuleRefactor(refactor: ModuleRefactorRequest): Promise<ModuleRefactorPreview> {
  return request(moduleURL("refactor/preview"), { method: "POST", body: refactorBody(refactor) });
}

export function applyModuleRefactor(refactor: ModuleRefactorRequest, previewHash: string): Promise<ModuleRefactorApply> {
  return request(moduleURL("refactor/apply"), { method: "POST", body: refactorBody(refactor, previewHash) });
}

export type StructuredQueryOptions = {
  methods?: boolean; vars?: boolean; types?: boolean; modules?: boolean; packages?: boolean;
  callers?: boolean; calls?: boolean; implements?: boolean; inherits?: boolean;
  include?: string[]; exclude?: string[];
};

export async function runModuleQuery(expression: string, root: string, snapshot: string, options: StructuredQueryOptions = {}): Promise<TimedResponse<ModuleQueryResult>> {
  const result = await timedRequest<unknown>(moduleURL("query"), { method: "POST", body: JSON.stringify({ ...(expression ? { args: [expression] } : {}), root, snapshot, ...options }) });
  if (!isQueryResult(result.data)) throw new Error(`Query response is not a result envelope: ${JSON.stringify(result.data).slice(0, 200)}`);
  return { data: result.data, timing: result.timing };
}

export async function suggestModuleSymbols(prefix: string, root: string, snapshot: string, signal?: AbortSignal): Promise<ModuleQuerySymbol[]> {
  return (await request<ItemsWithWarnings<ModuleQuerySymbol>>(moduleURL("suggest", { prefix, root, snapshot }), { signal })).items;
}

export async function suggestTypedSelectors(prefix: string, root: string, snapshot: string, signal?: AbortSignal): Promise<string[]> {
  return (await request<ItemsWithWarnings<string>>(moduleURL("suggest-selectors", { prefix, root, snapshot }), { signal })).items;
}

function isQueryResult(value: unknown): value is ModuleQueryResult {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const result = value as Record<string, unknown>;
  return typeof result.total === "number" && ["matches", "declarations", "symbols", "coverage", "warnings", "stages"].every((key) => Array.isArray(result[key]));
}

export function addModules(path: string, includeTests: boolean): Promise<ModuleIndexResult[]> {
  return request(moduleURL("add"), { method: "POST", body: JSON.stringify({ args: [path], "include-tests": includeTests, "no-workspace-uses": true }) });
}

export function reindexModules(options: { path?: string; includeTests?: boolean; force?: boolean; all?: boolean }): Promise<ModuleIndexResult[]> {
  return request(moduleURL("reindex"), { method: "POST", body: JSON.stringify({
    ...(options.path ? { args: [options.path] } : {}), "include-tests": options.includeTests ?? false,
    force: options.force ?? false, all: options.all ?? false,
  }) });
}

export function listGitHistory(root: string, location: string): Promise<GitHistory> {
  return request(moduleURL("history", { root, location, limit: "50" }));
}

export function compareGitRevisions(root: string, from: string, to: string, visibility: string, includeTests: boolean): Promise<SymbolDiff> {
  return request(moduleURL("diff"), { method: "POST", body: JSON.stringify({ args: [`${from}..${to}`], root, visibility, stat: true, "auto-index": true, "include-tests": includeTests }) });
}
