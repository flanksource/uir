import type { ModuleNode, ModuleSource } from "./api";

export type FileItem = { id: string; label: string; path: string; children: FileItem[]; source?: ModuleSource };
export type SymbolItem = { id: string; label: string; children: SymbolItem[]; node: ModuleNode };

export function fileTree(sources: ModuleSource[]): FileItem[] {
  const roots: FileItem[] = [];
  const folders = new Map<string, FileItem>();
  const paths = new Set<string>();
  for (const source of sources) {
    if (paths.has(source.path)) throw new Error(`Duplicate indexed source ${source.path}`);
    paths.add(source.path);
    const parts = source.path.split("/");
    if (parts.some((part) => !part || part === "." || part === "..")) throw new Error(`Invalid indexed source path ${source.path}`);
    let siblings = roots;
    for (let index = 0; index < parts.length - 1; index++) {
      const path = parts.slice(0, index + 1).join("/");
      let folder = folders.get(path);
      if (!folder) {
        folder = { id: `folder:${path}`, label: parts[index], path, children: [] };
        folders.set(path, folder);
        siblings.push(folder);
      }
      siblings = folder.children;
    }
    siblings.push({ id: `file:${source.id}`, label: parts[parts.length - 1], path: source.path, children: [], source });
  }
  function sort(items: FileItem[]) {
    items.sort((left, right) => Number(Boolean(left.source)) - Number(Boolean(right.source)) || left.label.localeCompare(right.label));
    items.forEach((item) => sort(item.children));
  }
  sort(roots);
  return roots;
}

export function symbolTree(nodes: ModuleNode[], snapshotNodes: ModuleNode[] = nodes): SymbolItem[] {
  const items = new Map<string, SymbolItem>();
  const snapshotSources = new Map(snapshotNodes.map((node) => [node.id.slice(node.source_id.length + 1), node.source_id]));
  const roots: SymbolItem[] = [];
  for (const node of nodes) {
    if (items.has(node.id)) throw new Error(`Duplicate indexed symbol ${node.id}`);
    const name = node.identifier.method || node.identifier.field || node.identifier.type || node.symbol;
    items.set(node.id, { id: node.id, label: node.identifier.method ? `${name}${node.identifier.signature ?? ""}` : name, node, children: [] });
  }
  for (const node of nodes) {
    const item = items.get(node.id)!;
    if (!node.parent_identity || (node.identifier.package && node.parent_identity === `v1:${JSON.stringify(["package", "", node.identifier.package, "", "", "", ""])}`)) {
      roots.push(item);
      continue;
    }
    const parent = items.get(`${node.source_id}:${node.parent_identity}`);
    if (!parent) {
      if (snapshotSources.get(node.parent_identity) === node.source_id || !snapshotSources.has(node.parent_identity)) {
        throw new Error(`Symbol ${node.id} has missing parent ${node.parent_identity}`);
      }
      roots.push(item);
      continue;
    }
    parent.children.push(item);
  }
  function sort(items: SymbolItem[]) {
    items.sort((left, right) => (left.node.line ?? Infinity) - (right.node.line ?? Infinity)
      || (left.node.column ?? Infinity) - (right.node.column ?? Infinity) || left.node.ordinal - right.node.ordinal || left.label.localeCompare(right.label));
    items.forEach((item) => sort(item.children));
  }
  sort(roots);
  return roots;
}
