import type { HeadFileItem } from "./explorer-model";
import type { Route } from "./route";

export function fileSelectionPatch(item: HeadFileItem): Pick<Route, "module" | "location" | "snapshot" | "source" | "node" | "fileSearch" | "symbolSearch" | "offset"> | undefined {
  if (item.kind !== "file") return undefined;
  if (!item.head || !item.source) throw new Error(`File ${item.path} has no indexed head or source`);
  return { module: item.head.root_key, location: item.head.location, snapshot: item.head.snapshot_id,
    source: item.source.id, node: "", fileSearch: "", symbolSearch: "", offset: 0 };
}
