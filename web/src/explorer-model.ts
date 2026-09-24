import type { ModuleHead, ModuleNode, ModuleSource } from "./api";

export type FileItem = { id: string; label: string; path: string; children: FileItem[]; source?: ModuleSource };
export type SymbolItem = { id: string; label: string; children: SymbolItem[]; node: ModuleNode };
export type HeadFileItem = { id: string; label: string; path: string; kind: "module" | "checkout" | "folder" | "file"; children: HeadFileItem[]; head?: ModuleHead; source?: ModuleSource };

// scopedHeads applies the module scope as a filter: an empty scope keeps every head.
export function scopedHeads(heads: ModuleHead[], scope: string): ModuleHead[] {
  return scope ? heads.filter((head) => head.root_key === scope) : heads;
}

export function moduleHeadTree(heads: ModuleHead[]): HeadFileItem[] {
  const modules = new Map<string, { name: string; heads: ModuleHead[] }>();
  for (const head of heads) {
    if (!head.root_key || !head.location || !head.snapshot_id) throw new Error("A module head is missing its root, checkout, or snapshot");
    const module = modules.get(head.root_key);
    if (module && module.name !== head.name) throw new Error(`Conflicting name for module ${head.root_key}`);
    if (module?.heads.some((item) => item.location === head.location)) throw new Error(`Duplicate checkout head ${head.root_key} at ${head.location}`);
    for (const source of head.sources) {
      if (source.root_key !== head.root_key || source.location !== head.location || source.snapshot_id !== head.snapshot_id) {
        throw new Error(`Source ${source.path} does not belong to head ${head.snapshot_id}`);
      }
    }
    if (module) module.heads.push(head);
    else modules.set(head.root_key, { name: head.name, heads: [head] });
  }
  return [...modules].sort(([left], [right]) => left.localeCompare(right)).map(([rootKey, module]) => {
    const checkouts = module.heads.sort((left, right) => left.location.localeCompare(right.location)).map((head) => {
      const toItem = (item: FileItem): HeadFileItem => ({
        id: JSON.stringify([item.source ? "file" : "folder", rootKey, head.location, head.snapshot_id, item.path]),
        label: item.label, path: item.path, kind: item.source ? "file" : "folder", head, source: item.source,
        children: item.children.map(toItem),
      });
      return {
        id: JSON.stringify(["checkout", rootKey, head.location, head.snapshot_id]),
        label: head.location.split("/").filter(Boolean).at(-1) ?? head.location,
        path: head.location, kind: "checkout" as const, head, children: fileTree(head.sources).map(toItem),
      };
    });
    return {
      id: JSON.stringify(["module", rootKey]), label: module.name || rootKey, path: rootKey, kind: "module" as const,
      head: module.heads.length === 1 ? module.heads[0] : undefined,
      children: checkouts.length === 1 ? checkouts[0].children : checkouts,
    };
  });
}

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
