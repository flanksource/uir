import { useMemo } from "react";
import { Button } from "@flanksource/clicky-ui/components";
import { Badge, DataTable, type BadgeTone, type DataTableColumn } from "@flanksource/clicky-ui/data";
import type { ModuleSnapshot, Page, SnapshotReason } from "./api";
import { snapshotExplorerPatch } from "./explorer-navigation";
import type { Route } from "./route";
import { shortId, snapshotDelta, snapshotDuration, snapshotDurationMs, snapshotSize } from "./snapshot-format";
import { TaskRunLink } from "./task-links";
import { ErrorMessage } from "./ui";
import type { Load } from "./use-load";

const PAGE_SIZE = 100;

const reasonTones: Record<SnapshotReason, BadgeTone> = {
  unknown: "neutral", add: "success", reindex: "info", refactor: "warning",
  "local-dependency": "neutral", "versioned-dependency": "neutral", historical: "info", "dependency-cycle": "neutral",
};

type Props = { snapshots: Load<Page<ModuleSnapshot>>; route: Route; onRoute: (patch: Partial<Route>) => void };

function snapshotColumns({ route, onRoute }: Omit<Props, "snapshots">): DataTableColumn<ModuleSnapshot>[] {
  return [
    { key: "id", label: "Snapshot", filterable: false, render: (_, row) => <code className="text-xs">{shortId(row.id)}</code> },
    { key: "open", label: "", filterable: false, sortable: false, render: (_, row) => <Button type="button" size="sm" variant="outline"
      onClick={(event: React.MouseEvent<HTMLButtonElement>) => { event.stopPropagation(); onRoute(snapshotExplorerPatch({ location: row.canonical_path, snapshot: row.id })); }}>Open in Explorer</Button> },
    { key: "completed_at", label: "Date", kind: "timestamp", sortable: true, filterable: false },
    { key: "reason", label: "Reason", render: (_, row) => <Badge tone={reasonTones[row.reason]} size="sm" clickToCopy={false}>{row.reason}</Badge> },
    { key: "kind", label: "Kind", render: (_, row) => row.head ? `${row.kind} · current v${row.head_version}` : row.kind },
    { key: "duration", label: "Duration", accessor: snapshotDurationMs, sortable: true, filterable: false, align: "right", render: (_, row) => snapshotDuration(row) },
    { key: "size", label: "Size", accessor: (row) => row.source_bytes, sortable: true, filterable: false, render: (_, row) => snapshotSize(row) },
    { key: "delta", label: "Delta", filterable: false, render: (_, row) => snapshotDelta(row) },
    { key: "revision", label: "Revision", filterable: false,
      render: (_, row) => `${row.module_version || shortId(row.revision)}${row.worktree_state === "dirty" ? " (dirty)" : ""}` },
    { key: "coverage", label: "Coverage", filterable: false },
    { key: "task_run_id", label: "Run", filterable: false,
      render: (_, row) => row.task_run_id ? <TaskRunLink runId={row.task_run_id} route={route} onRoute={onRoute}>{shortId(row.task_run_id)}</TaskRunLink> : "—" },
  ];
}

/** The snapshots of one checkout, a server page at a time; reason and kind filter the loaded page. */
export function SnapshotTable({ snapshots, route, onRoute }: Props) {
  const columns = useMemo(() => snapshotColumns({ route, onRoute }), [route, onRoute]);
  const page = snapshots.data?.page;
  return <>
    <ErrorMessage error={snapshots.error} />
    <DataTable className="min-h-40 max-h-[28rem]" data={snapshots.data?.data ?? []} columns={columns} loading={snapshots.loading} getRowId={(row) => row.id}
      autoFilter emptyMessage="No snapshots match this view" getRowClassName={(row) => row.id === route.snapshot ? "bg-accent/50" : undefined}
      onRowClick={(row) => onRoute({ snapshot: row.id, source: "", node: "", fileSearch: "", symbolSearch: "" })}
      pagination={page ? { page: Math.floor(route.offset / PAGE_SIZE), pageSize: PAGE_SIZE, total: page.total,
        onPageChange: (next) => onRoute({ offset: next * PAGE_SIZE }), onPageSizeChange: () => onRoute({ offset: 0 }), pageSizeOptions: [PAGE_SIZE] } : undefined} />
  </>;
}
