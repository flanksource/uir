import { Button } from "@flanksource/clicky-ui/components";
import { listModuleDependencies } from "./api";
import type { Route } from "./route";
import { ErrorMessage, Muted } from "./ui";
import { useLoad } from "./use-load";

export function ExplorerDependencies({ snapshot, onRoute }: { snapshot: string; onRoute: (patch: Partial<Route>) => void }) {
  const dependencies = useLoad(snapshot ? () => listModuleDependencies(snapshot) : null, snapshot);
  if (!snapshot) return <div className="p-3"><Muted>Select a snapshot to view dependencies.</Muted></div>;
  if (dependencies.loading) return <div className="p-3"><Muted>Loading dependencies…</Muted></div>;
  if (dependencies.error) return <div className="p-3"><ErrorMessage error={dependencies.error} /></div>;
  if (!dependencies.data?.captured) return <div className="p-3"><Muted>Dependencies were not captured for this snapshot. Reindex to record them.</Muted></div>;
  if (!dependencies.data.items.length) return <div className="p-3"><Muted>No go.mod requirements.</Muted></div>;
  return <ul className="divide-y divide-border overflow-auto text-sm">
    {dependencies.data.items.map((edge) => <li key={edge.module_path} className="space-y-1 p-3">
      <div className="font-medium break-all">{edge.module_path}</div>
      <div className="text-xs text-muted-foreground">{edge.indirect ? "Indirect" : "Direct"} · requested {edge.declared_version}
        {edge.selected_version && <> · selected {edge.selected_version}</>}
        {edge.replace_path && <> · replace {edge.replace_path}{edge.replace_version && `@${edge.replace_version}`}</>}
      </div>
      {edge.target_snapshot_id && edge.target_root_key && edge.target_location ? <Button type="button" size="sm" variant="outline" onClick={() => onRoute({
        module: edge.target_root_key, location: edge.target_location, snapshot: edge.target_snapshot_id, source: "", node: "", fileSearch: "", symbolSearch: "", offset: 0,
      })}>Open snapshot {edge.target_snapshot_id.slice(0, 12)}</Button> :
        <div className="text-xs text-muted-foreground">Unresolved: {edge.unresolved_reason}</div>}
    </li>)}
  </ul>;
}
