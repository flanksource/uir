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
