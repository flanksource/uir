export type Project = Record<string, unknown> & {
  key: string;
  name: string;
  snapshot_id?: string;
  head_version: number;
  roots: number;
  sources: number;
  nodes: number;
};

export type BrowseRow = Record<string, unknown> & {
  id: string;
  name: string;
  project_key?: string;
  snapshot_id?: string;
  root_id?: string;
  source_id?: string;
  path?: string;
  node_type?: string;
  symbol?: string;
  language?: string;
  state?: string;
  head?: boolean;
  version?: number;
  kind?: string;
  repository?: string;
  revision?: string;
  local_path?: string;
  started_at?: string;
};

export type Page<T> = { data: T[]; page: { limit: number; offset: number; total: number } };
export type SourceContent = { source_id: string; path: string; content?: string; repository?: string; revision?: string; origin: "git" | "local" | "remote" };
export type QueryRow = Record<string, unknown> & { id: string; operation: string; root?: string; node_type: string; symbol: string; location?: string };
export type ReindexResult = { snapshot_id: string; unchanged: boolean; nodes: number; files: number; head_version: number };
export type NodeDetails = { node: Record<string, unknown>; fields: Record<string, unknown>[]; locations: Record<string, unknown>[]; relationships: Record<string, unknown>[] };

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(path, { headers: { Accept: "application/json", ...(options?.body ? { "Content-Type": "application/json" } : {}) }, ...options });
  if (!response.ok) {
    const body = await response.text();
    throw new Error(`${response.status} ${response.statusText}: ${body}`);
  }
  return response.json() as Promise<T>;
}

function apiPath(entity: string, id?: string, action?: string): string {
  return `/api/v1/${entity}${id ? `/${encodeURIComponent(id)}` : ""}${action ? `/${action}` : ""}`;
}

export function listProjects(): Promise<Project[]> {
  return request(apiPath("project"));
}

export function listRows(entity: "snapshot" | "root" | "source" | "node", params: Record<string, string>): Promise<Page<BrowseRow>> {
  const query = new URLSearchParams({ limit: "100", ...params });
  return request(`${apiPath(entity)}?${query}`);
}

export function getRow(entity: "node" | "source", id: string): Promise<NodeDetails | BrowseRow> {
  return request(apiPath(entity, id));
}

export function getSourceContent(id: string): Promise<SourceContent> {
  return request(apiPath("source", id, "content"));
}

export function runQuery(project: string, expression: string, snapshot: string): Promise<QueryRow[]> {
  return request(apiPath("project", project, "query"), { method: "POST", body: JSON.stringify({ expression, snapshot }) });
}

export function runReindex(project: string, options: { path: string; root?: string; name?: string; "include-tests": boolean; force: boolean }): Promise<ReindexResult> {
  return request(apiPath("project", project, "reindex"), { method: "POST", body: JSON.stringify(options) });
}
