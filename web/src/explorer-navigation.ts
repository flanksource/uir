import type { ModuleNode } from "./api";
import type { HeadFileItem } from "./explorer-model";
import type { Route } from "./route";

// fileSelectionPatch never touches the module: the scope picker owns it, and navigation only changes
// which checkout head and source are open within that scope.
export function fileSelectionPatch(item: HeadFileItem): Pick<Route, "location" | "snapshot" | "source" | "node" | "fileSearch" | "symbolSearch" | "offset"> | undefined {
  if (item.kind !== "file") return undefined;
  if (!item.head || !item.source) throw new Error(`File ${item.path} has no indexed head or source`);
  return { location: item.head.location, snapshot: item.head.snapshot_id,
    source: item.source.id, node: "", fileSearch: "", symbolSearch: "", offset: 0 };
}

export function symbolSelectionPatch(head: Pick<Route, "location" | "snapshot">, node: ModuleNode): Pick<Route, "location" | "snapshot" | "source" | "node" | "line" | "column" | "fileSearch" | "symbolSearch" | "offset"> {
  if (!head.location || !head.snapshot) throw new Error(`Symbol ${node.id} has no selected checkout head`);
  if (!node.source_id || !node.line || node.line < 1) throw new Error(`Symbol ${node.id} has no indexed source line`);
  return { location: head.location, snapshot: head.snapshot, source: node.source_id, node: node.id,
    line: node.line, column: Math.max(1, node.column ?? 1), fileSearch: "", symbolSearch: "", offset: 0 };
}
