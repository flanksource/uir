import type { ModuleNode, ModuleQueryRow } from "./api";

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

function lastSegment(path: string): string {
  return path.slice(path.lastIndexOf(".") + 1);
}

// symbolSelector spells an identity as the predicates of a symbol operation. A dotted owner path
// names its innermost owner through `owner`, and a nested type itself through `name` and
// `kind = "type"`, since `type` only matches a top-level type name.
export function symbolSelector(identity: IdentityKey): string {
  if (!identity.package) throw new Error(`The ${identity.nodeType} identity has no package to select a symbol in`);
  const predicates: [string, string][] = [["package", identity.package]];
  const owner: [string, string][] = !identity.type ? [] : identity.type.includes(".") ? [["owner", lastSegment(identity.type)]] : [["type", identity.type]];
  if (identity.method) {
    predicates.push(...owner, ["method", identity.method], ...(identity.type ? [] : [["kind", "func"] as [string, string]]));
  } else if (identity.field && !identity.type) {
    predicates.push(["name", identity.field]);
  } else if (identity.field.includes(".")) {
    const path = identity.field.split(".");
    predicates.push(["owner", path[path.length - 2]], ["field", path[path.length - 1]]);
  } else if (identity.field) {
    predicates.push(...owner, ["field", identity.field]);
  } else if (identity.type.includes(".")) {
    predicates.push(["name", lastSegment(identity.type)], ["kind", "type"]);
  } else if (identity.type) {
    predicates.push(["type", identity.type]);
  } else {
    throw new Error(`A ${identity.nodeType} identity names no symbol`);
  }
  return predicates.map(([field, value]) => `${field} = ${JSON.stringify(value)}`).join(" and ");
}

export function referencesExpression(selector: string): string {
  return `references of node where ${selector}`;
}

function methodSample(nodes: ModuleNode[]): ModuleNode | undefined {
  const methods = nodes.filter((node) => node.identifier.type && node.identifier.method);
  return methods.find((node) => /^[A-Z]/.test(node.identifier.method ?? "")) ?? methods[0];
}

// queryExamples writes one runnable expression per operation, naming symbols the snapshot declares.
export function queryExamples(root: string, nodes: ModuleNode[]): QueryExample[] {
  const scope = `root = ${JSON.stringify(root)}`;
  const examples: QueryExample[] = [
    { id: "nodes", label: "Methods in this module", expression: `nodes where ${scope} and node_type = "method"` },
    { id: "unresolved", label: "Unresolved calls", expression: `unresolved calls where ${scope}` },
  ];
  const sample = methodSample(nodes);
  if (!sample) return examples;
  const method = parseIdentityKey(nodeIdentityKey(sample));
  const selector = symbolSelector(method);
  const name = `${method.type}.${method.method}`;
  const iface = nodes.find((node) => node.node_type === "interface");
  const target = iface ? parseIdentityKey(nodeIdentityKey(iface)) : { ...method, method: "", signature: "" };
  return [...examples,
    { id: "references", label: `References of ${name}`, expression: referencesExpression(selector) },
    { id: "definitions", label: `Definitions of ${name}`, expression: `definitions of node where ${selector}` },
    { id: "implementations", label: `Implementations of ${target.type}`, expression: `implementations of node where ${symbolSelector(target)}` },
    { id: "callers", label: `Callers of ${name}`, expression: `callers of node where ${selector}` },
    { id: "dispatch", label: `Callers of ${name} including dispatch`, expression: `callers of node where ${selector} including dispatch` },
    { id: "callees", label: `Callees of ${name}`, expression: `callees of node where ${selector}` },
    { id: "search", label: `Search "${method.method.slice(0, 3)}"`, expression: `search ${JSON.stringify(method.method.slice(0, 3))} where ${scope}` },
    { id: "member-search", label: `Search members of ${method.type}`, expression: `search ${JSON.stringify(`${method.type}.${method.method.slice(0, 2)}`)} where ${scope}` },
  ];
}

const operationPrefix = /^(nodes where |unresolved calls\b|(references|definitions|implementations|callers|callees) of |search )/;

// paletteExpression returns the query a palette input spells: anything after `>`, or input that
// starts with an operation keyword.
export function paletteExpression(input: string): string | undefined {
  const text = input.trim();
  if (text.startsWith(">")) return text.slice(1).trim() || undefined;
  return operationPrefix.test(text) ? text : undefined;
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
