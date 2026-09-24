import type { ModuleNode, ModuleQueryRow } from "./api";
import type { Route } from "./route";

export type IdentityKey = { nodeType: string; module: string; package: string; type: string; method: string; field: string; signature: string };
export type QueryExample = { id: string; label: string; expression: string };
export type FileRows = { path: string; rows: ModuleQueryRow[] };

export function parseIdentityKey(key: string): IdentityKey {
  if (!key.startsWith("v1:")) throw new Error(`${key} is not a v1 identity key`);
  const components: unknown = JSON.parse(key.slice(3));
  if (!Array.isArray(components) || components.length !== 7 || components.some((component) => typeof component !== "string")) {
    throw new Error(`Identity key ${key} does not have seven string components`);
  }
  const [nodeType, module, packagePath, type, method, field, signature] = components as string[];
  return { nodeType, module, package: packagePath, type, method, field, signature };
}

export function nodeIdentityKey(node: Pick<ModuleNode, "id" | "source_id">): string {
  const prefix = `${node.source_id}:`;
  if (!node.id.startsWith(prefix)) throw new Error(`Node ${node.id} is not scoped to source ${node.source_id}`);
  return node.id.slice(prefix.length);
}

export function identityLabel(key: string): string {
  const identity = parseIdentityKey(key);
  const member = identity.method || identity.field;
  if (member) return identity.type ? `${identity.type}.${member}` : member;
  return identity.type || identity.package;
}

// symbolSelector spells a selected node with its exact Go import path and owner chain.
export function symbolSelector(identity: IdentityKey): string {
  if (!identity.package) throw new Error(`The ${identity.nodeType} identity has no package to select a symbol in`);
  if (!/^[A-Za-z0-9_./-]+$/.test(identity.package)) throw new Error(`Package ${identity.package} cannot be spelled in a compact query`);
  const member = identity.method || identity.field;
  const parts = [identity.type, member].filter(Boolean).flatMap((part) => part.split("."));
  if (parts.some((part) => !/^[A-Za-z_][A-Za-z0-9_]*$/.test(part))) {
    throw new Error(`The ${identity.nodeType} identity has a member that cannot be spelled in a compact query`);
  }
  return [identity.package, ...parts].join(".");
}

export function referencesExpression(selector: string): string {
  return `${selector} <`;
}

function methodSample(nodes: ModuleNode[]): ModuleNode | undefined {
	const methods = nodes.filter((node) => node.identifier.type && node.identifier.method);
  return methods.find((node) => /^[A-Z]/.test(node.identifier.method ?? "")) ?? methods[0];
}

// queryScope is the root and snapshot a query runs against: both empty for every module, and none
// while a chosen module has not resolved its snapshot yet.
export function queryScope(route: Pick<Route, "module" | "snapshot">): { root: string; snapshot: string } | null {
  if (!route.module) return { root: "", snapshot: "" };
  return route.snapshot ? { root: route.module, snapshot: route.snapshot } : null;
}

// queryExamples names symbols from the selected snapshot; query scope is carried by the request.
export function queryExamples(_root: string, nodes: ModuleNode[]): QueryExample[] {
  const examples: QueryExample[] = [];
  const sample = methodSample(nodes);
  if (!sample) return examples;
  const method = parseIdentityKey(nodeIdentityKey(sample));
  const selector = symbolSelector(method);
  const name = `${method.type}.${method.method}`;
  const iface = nodes.find((node) => node.node_type === "interface");
  const type = method.type ? symbolSelector({ ...method, method: "", field: "" }) : "";
  return [...examples,
    { id: "resolve", label: `Resolve ${name}`, expression: selector },
    { id: "incoming", label: `Callers of ${name}`, expression: referencesExpression(selector) },
    { id: "definitions", label: `Definition of ${name}`, expression: `${selector} =` },
    { id: "outgoing", label: `Callees of ${name}`, expression: `${selector} >` },
    { id: "transitive", label: `Transitive callers of ${name}`, expression: `${selector} <<3` },
    ...(type ? [{ id: "methods", label: `Methods of ${method.type}`, expression: `${type} :methods` }] : []),
    ...(iface ? [{ id: "implementations", label: `Implementers of ${iface.identifier.type}`, expression: `${symbolSelector(parseIdentityKey(nodeIdentityKey(iface)))} :impl` }] : []),
  ];
}

// paletteExpression recognizes a qualified Go subject or the explicit `>` command prefix.
export function paletteExpression(input: string): string | undefined {
  const text = input.trim();
  if (text.startsWith(">")) return text.slice(1).trim() || undefined;
  const subject = text.split(/\s/, 1)[0];
  return /^[A-Za-z_][A-Za-z0-9_./*-]*$/.test(subject) && /[./]/.test(subject) ? text : undefined;
}

export function groupRowsByFile(rows: ModuleQueryRow[]): { candidates: ModuleQueryRow[]; files: FileRows[] } {
  const candidates: ModuleQueryRow[] = [];
  const files = new Map<string, FileRows>();
  for (const row of rows) {
    if (row.kind === "candidate") {
      candidates.push(row);
      continue;
    }
    if (!row.path) throw new Error(`The ${row.kind} row ${row.symbol} has no path`);
    const file = files.get(row.path);
    if (file) file.rows.push(row);
    else files.set(row.path, { path: row.path, rows: [row] });
  }
  return { candidates, files: [...files.values()] };
}
