// API responses for call-graph.spec.ts, in the shape of a captured
// `GET /api/v1/modules/graph?selector=github.com/flanksource/uir/query.Pipeline.RunExpr` response
// (clicky-ui playground `_call-graph/run-expr.json`), trimmed to the nodes the spec walks through and
// extended with the `exclude` and `packages` fields the server now returns.
import { excludesPackage } from "../src/call-graph-exclude";

export const root = "github.com/flanksource/uir";
export const location = "/checkout/uir";
export const snapshot = "11111111-1111-1111-1111-111111111111";
const QUERY = `${root}/query`;
const UUID = "github.com/google/uuid";

const sources = {
  modules: { id: "src-modules", path: "query/modules.go" },
  scope: { id: "src-scope", path: "query/modules_scope.go" },
  compact: { id: "src-compact", path: "query/compact_execute.go" },
};

function identity(type: string, method: string, signature: string): string {
  return `v1:${JSON.stringify(["method", "", QUERY, type, method, "", signature])}`;
}

type Declared = { id: string; type: string; method: string; signature: string; source: { id: string; path: string }; line: number; depth: number; in: number; out: number };

const declared: Record<string, Declared> = {
  runModules: { id: "19cf64795b7bb354e90016138c549f55d00d8a4b7809c655883a30b6284d92ea", type: "Pipeline", method: "RunModules", signature: "(ctx context.Context, input string) (ModuleQueryResult, error)", source: sources.modules, line: 79, depth: -1, in: 0, out: 1 },
  runExpr: { id: "eb0565301d8bbb6cf8e4a460f7be2e619bc754931176ee749a12f2e386eba30f", type: "Pipeline", method: "RunExpr", signature: "(ctx context.Context, input string) (ModuleQueryResult, error)", source: sources.modules, line: 120, depth: 0, in: 1, out: 2 },
  moduleScopes: { id: "5f2b7c3e9d1a4b6c8e0f2a4c6e8b0d2f4a6c8e0b2d4f6a8c0e2b4d6f8a0c2e4b", type: "Pipeline", method: "moduleScopes", signature: "(ctx context.Context, options ModuleScopeOptions) (moduleSelection, error)", source: sources.scope, line: 30, depth: 1, in: 1, out: 4 },
  runCompact: { id: "a4c6e8b0d2f4a6c8e0b2d4f6a8c0e2b45f2b7c3e9d1a4b6c8e0f2a4c6e8b0d2f", type: "Pipeline", method: "runCompact", signature: "(ctx context.Context, expression string) (ModuleQueryResult, error)", source: sources.compact, line: 40, depth: 1, in: 1, out: 0 },
  headError: { id: "7cc59ef9a9e8f3c584f6c8918fe2c2f6211a8c44cc62799d3c51ace39bec5f3b", type: "", method: "headError", signature: "(location string) error", source: sources.scope, line: 200, depth: 2, in: 1, out: 0 },
  broad: { id: "0d2f4a6c8e0b2d4f6a8c0e2b4d6f8a0c2e4b5f2b7c3e9d1a4b6c8e0f2a4c6e8b", type: "Pipeline", method: "broadModuleScopes", signature: "(ctx context.Context) ([]moduleScope, error)", source: sources.scope, line: 90, depth: 1, in: 1, out: 0 },
  fromHeads: { id: "c8e0f2a4c6e8b0d2f5f2b7c3e9d1a4b6a4c6e8b0d2f4a6c8e0b2d4f6a8c0e2b4", type: "Pipeline", method: "scopesFromHeads", signature: "(ctx context.Context) ([]moduleScope, error)", source: sources.scope, line: 140, depth: 1, in: 1, out: 0 },
};
const uuidParse = { id: "b2d4f6a8c0e2b4d6f8a0c2e4b5f2b7c3e9d1a4b6c8e0f2a4c6e8b0d2f4a6c8e0", identifier: { package: UUID, method: "Parse", signature: "(s string) (UUID, error)", node_type: "method" },
  kind: "func", label: "Parse", group: UUID, depth: 2, in: 1, out: 0 };

function label(symbol: Declared): string {
  return symbol.type ? `${symbol.type}.${symbol.method}` : symbol.method;
}

export function explorerNodeId(symbol: Declared): string {
  return `${symbol.source.id}:${identity(symbol.type, symbol.method, symbol.signature)}`;
}

function graphNode(symbol: Declared, depth = symbol.depth) {
  return { id: symbol.id, identifier: { package: QUERY, ...(symbol.type ? { type: symbol.type } : {}), method: symbol.method, signature: symbol.signature, node_type: "method" },
    kind: symbol.type ? "method" : "func", label: label(symbol), group: QUERY, depth, in: symbol.in, out: symbol.out,
    location: { root_key: root, checkout_path: location, snapshot_id: snapshot, source_id: symbol.source.id, path: symbol.source.path,
      identity_key: identity(symbol.type, symbol.method, symbol.signature), line: symbol.line, column: 6 } };
}

function edge(from: Declared, to: { id: string }, sites: { path: string; line: number; text: string; guards?: string[] }[]) {
  return { id: `${from.id}|${to.id}|call`, from: from.id, to: to.id, type: "call", sites: sites.map((site) => ({ column: 2, ...site })) };
}

export const RUN_EXPR = declared.runExpr;
export const MODULE_SCOPES = declared.moduleScopes;
export const SCOPE_SITE_LINE = 126;
export const DEFAULT_EXCLUDE = ["std", "builtin", "gorm.io/..."];

export function graphResponse(exclude: string[]) {
  const { runModules, runExpr, moduleScopes, runCompact, headError } = declared;
  return {
    roots: [runExpr.id],
    nodes: [graphNode(runModules), graphNode(runExpr), graphNode(moduleScopes), graphNode(runCompact), graphNode(headError), uuidParse],
    edges: [
      edge(runModules, runExpr, [{ path: "query/modules.go", line: 84, text: "pipeline.RunExpr(ctx, input)" }]),
      edge(runExpr, moduleScopes, [{ path: "query/modules.go", line: SCOPE_SITE_LINE, text: "pipeline.moduleScopes(ctx,\n\toptions.Scope)", guards: ["options.Scope != nil"] }]),
      edge(runExpr, runCompact, [{ path: "query/modules.go", line: 131, text: "pipeline.runCompact(ctx, input)" }]),
      edge(moduleScopes, headError, [{ path: "query/modules_scope.go", line: 44, text: "headError(location)", guards: ["err != nil"] }]),
      edge(moduleScopes, uuidParse, [{ path: "query/modules_scope.go", line: 38, text: "uuid.Parse(options.Snapshot)" }]),
    ],
    groups: [{ id: QUERY, label: QUERY }, { id: UUID, label: UUID }],
    omitted: { beyond_depth: 4, excluded: { fmt: 3, builtin: 2, "gorm.io/gorm": 2, strings: 1 } },
    exclude,
    packages: [
      { path: QUERY, external: false, nodes: 5 },
      { path: "fmt", external: true, nodes: 3 },
      { path: "builtin", external: true, nodes: 2 },
      { path: "gorm.io/gorm", external: true, nodes: 2 },
      { path: UUID, external: true, nodes: 1 },
      { path: "strings", external: true, nodes: 1 },
    ].map((pkg) => ({ ...pkg, excluded: exclude.some((pattern) => excludesPackage(pattern, { ...pkg, excluded: false })) })),
    stages: [{ name: "scope", value: "1 snapshots" }, { name: "resolve", value: `${QUERY}.Pipeline.RunExpr` }, { name: "graph", value: "6 nodes, 5 edges" }],
    warnings: [],
    candidates: [],
  };
}

/** `symbol=<moduleScopes>&depth=1&direction=callees`: rooted at moduleScopes, so its depths count from it. */
export function moduleScopesExpansion(exclude: string[]) {
  const { moduleScopes, headError, broad, fromHeads } = declared;
  return {
    roots: [moduleScopes.id],
    nodes: [graphNode(moduleScopes, 0), graphNode(headError, 1), { ...uuidParse, depth: 1 }, graphNode(broad), graphNode(fromHeads)],
    edges: [
      edge(moduleScopes, headError, [{ path: "query/modules_scope.go", line: 44, text: "headError(location)", guards: ["err != nil"] }]),
      edge(moduleScopes, uuidParse, [{ path: "query/modules_scope.go", line: 38, text: "uuid.Parse(options.Snapshot)" }]),
      edge(moduleScopes, broad, [{ path: "query/modules_scope.go", line: 52, text: "pipeline.broadModuleScopes(ctx)", guards: ["options.All"] }]),
      edge(moduleScopes, fromHeads, [{ path: "query/modules_scope.go", line: 57, text: "pipeline.scopesFromHeads(ctx)" }]),
    ],
    groups: [{ id: QUERY, label: QUERY }, { id: UUID, label: UUID }],
    omitted: {},
    exclude,
    packages: [],
    stages: [],
    warnings: [],
    candidates: [],
  };
}

function moduleSource(source: { id: string; path: string }) {
  return { id: source.id, root_key: root, location, snapshot_id: snapshot, path: source.path, package_path: QUERY, content_hash: "0123456789abcdef0123", size_bytes: 4096 };
}

const browseNode = (symbol: Declared) => ({
  id: explorerNodeId(symbol), source_id: symbol.source.id, path: symbol.source.path, symbol: `query.${label(symbol)}`, node_type: "method",
  kind: symbol.type ? "method" : "func", visibility: /^[A-Z]/.test(symbol.method) ? "exported" : "internal",
  identifier: { package: QUERY, ...(symbol.type ? { type: symbol.type } : {}), method: symbol.method, signature: symbol.signature },
  child_slot: "decls", ordinal: symbol.line, payload: {}, semantic_hash: "abcdef0123456789", line: symbol.line, column: 6, calls: [],
});

export const moduleSources = Object.values(sources).map(moduleSource);

export const responses: Record<string, unknown> = {
  "/api/v1/modules": [{ root_key: root, name: "uir", location, snapshot_id: snapshot, head_version: 1 }],
  "/api/v1/modules/locations": [{ id: "checkout-1", root_key: root, canonical_path: location, kind: "worktree", primary: true, head_snapshot_id: snapshot, head_version: 1 }],
  "/api/v1/modules/snapshots": { data: [], page: { limit: 100, offset: 0, total: 0 } },
  "/api/v1/modules/heads": { items: [{ root_key: root, name: "uir", location, snapshot_id: snapshot, sources: moduleSources }], warnings: [] },
  "/api/v1/modules/browse": { sources: moduleSources, nodes: [declared.runModules, declared.runExpr, declared.moduleScopes, declared.runCompact, declared.headError].map(browseNode) },
  "/api/v1/modules/dependencies": { captured: true, items: [] },
  "/api/v1/modules/query": { operation: "incoming", total: 0, matches: [], declarations: [], symbols: [], coverage: [], warnings: [], stages: [] },
  "/api/v1/modules/content": { path: "query/modules.go", origin: "snapshot", revision: "", snapshot_id: snapshot,
    content: Array.from({ length: 220 }, (_, index) => `// query/modules.go line ${index + 1}`).join("\n") },
};
