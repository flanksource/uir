import { useState } from "react";
import { Button } from "@flanksource/clicky-ui/components";
import { browseModule, errorMessage, type SymbolChange, type SymbolDiff } from "./api";
import { snapshotExplorerPatch, symbolSelectionPatch } from "./explorer-navigation";
import { FileTypeIcon, FolderTypeIcon } from "./file-icons";
import { changeTarget, findChangedNode } from "./history-model";
import type { Route } from "./route";
import { shortId } from "./snapshot-format";
import { SymbolIcon } from "./symbol-icons";
import { ErrorMessage, Muted } from "./ui";

type OpenChange = (row: SymbolChange) => void;

export function countChanges(diff: SymbolDiff): number {
  return diff.packages.reduce((count, pkg) => count + pkg.files.reduce((fileCount, file) => fileCount + file.rows.length, 0), 0);
}

function ChangeRow({ row, onOpen }: { row: SymbolChange; onOpen: OpenChange }) {
  const name = `${row.owner ? `${row.owner}.` : ""}${row.name}`;
  return <div className="flex min-w-0 items-start gap-2 py-1 pl-5 text-sm" data-testid="change-row">
    <span className="mt-0.5 text-sky-600"><SymbolIcon nodeType={row.kind} /></span>
    <div className="min-w-0 flex-1">
      <div className="flex flex-wrap items-baseline gap-2">
        <button type="button" className="break-all text-left font-mono font-medium hover:underline" title={`Open ${name} in Explorer`} aria-label={`Open ${name} in Explorer`}
          onClick={() => onOpen(row)}>{name}</button>
        <span className={`text-xs font-medium ${row.class === "added" ? "text-emerald-600" : row.class === "removed" ? "text-red-600" : "text-sky-600"}`}>{row.class}</span>
        {row.lines && <span className="ml-auto font-mono text-xs text-muted-foreground">+{row.lines.added} −{row.lines.removed}</span>}
      </div>
      {row.shape_diff?.length ? <div className="mt-1 font-mono text-xs">{row.shape_diff.map((line, index) => <div key={index}
        className={line.op === "insert" ? "text-emerald-700" : line.op === "delete" ? "text-red-700 line-through" : "text-muted-foreground"}>{line.text}</div>)}</div>
        : row.shape_after || row.shape_before ? <code className={`block break-all text-xs text-muted-foreground ${row.class === "removed" ? "line-through" : ""}`}>{row.shape_after || row.shape_before}</code> : null}
      {row.path_before && row.path_after && row.path_before !== row.path_after && <div className="text-xs text-muted-foreground">{row.path_before} → {row.path_after}</div>}
      {row.note && <div className="text-xs text-muted-foreground">{row.note}</div>}
      {row.coverage?.length ? <div className="text-xs text-amber-700">Coverage: {row.coverage.join(", ")}</div> : null}
    </div>
  </div>;
}

function FileContents({ file, onOpen }: { file: SymbolDiff["packages"][number]["files"][number]; onOpen: OpenChange }) {
  return <div className="pb-2"><ErrorMessage error={file.lines_error} />
    {file.excluded && <div className="text-xs text-amber-700">{file.excluded}</div>}
    {file.rows.map((row, index) => <ChangeRow key={`${row.class}:${row.owner}:${row.name}:${index}`} row={row} onOpen={onOpen} />)}
    {file.hidden_rows ? <div className="pl-5 text-xs text-muted-foreground">{file.hidden_rows} changes hidden by visibility</div> : null}
  </div>;
}

function DiffTree({ diff, group, onOpen }: { diff: SymbolDiff; group: Route["historyGroup"]; onOpen: OpenChange }) {
  if (countChanges(diff) === 0) return <div className="rounded-md border border-border bg-card p-4 text-sm">No symbol changes in this range.</div>;
  if (group === "change") {
    const rows = diff.packages.flatMap((pkg) => pkg.files.flatMap((file) => file.rows.map((row) => ({ pkg: pkg.path, file: file.path, row }))));
    return <div className="rounded-md border border-border bg-card p-3">{(["added", "removed", "signature", "body", "moved"] as const).map((kind) => {
      const matching = rows.filter(({ row }) => row.class === kind);
      return matching.length ? <details key={kind} open className="border-b border-border last:border-0">
        <summary className="cursor-pointer py-2 text-sm font-semibold capitalize">{kind} <span className="font-normal text-muted-foreground">{matching.length}</span></summary>
        {matching.map(({ pkg, file, row }, index) => <div key={`${pkg}:${file}:${index}`}><div className="pl-5 text-xs text-muted-foreground">{pkg} · {file}</div><ChangeRow row={row} onOpen={onOpen} /></div>)}
      </details> : null;
    })}</div>;
  }
  if (group === "file") return <div className="rounded-md border border-border bg-card p-3">{diff.packages.flatMap((pkg) => pkg.files.map((file) => ({ pkg, file }))).map(({ pkg, file }) => <details key={`${pkg.path}:${file.path}`} open className="border-b border-border last:border-0">
    <summary className="flex cursor-pointer items-center gap-2 py-2 text-sm"><FileTypeIcon filename={file.path} /><span className="font-mono">{file.path}</span><span className="text-muted-foreground">· {pkg.path}</span><span className="ml-auto text-xs text-muted-foreground">{file.rows.length} changes</span></summary>
    <FileContents file={file} onOpen={onOpen} />
  </details>)}</div>;
  return <div className="rounded-md border border-border bg-card p-3">{diff.packages.map((pkg) => <details key={pkg.path} open className="border-b border-border last:border-0">
    <summary className="flex cursor-pointer items-center gap-2 py-2 text-sm font-semibold"><FolderTypeIcon open /><span className="font-mono">{pkg.path}</span><span className="ml-auto text-xs font-normal text-muted-foreground">{pkg.files.reduce((sum, file) => sum + file.rows.length, 0)} changes</span></summary>
    <div className="pl-3">{pkg.files.map((file) => <details key={file.path} open className="border-l border-border pl-3">
      <summary className="flex cursor-pointer items-center gap-2 py-1 text-sm"><FileTypeIcon filename={file.path} /><span className="font-mono">{file.path}</span><span className="ml-auto text-xs text-muted-foreground">{file.rows.length}</span></summary>
      <FileContents file={file} onOpen={onOpen} />
    </details>)}</div>
  </details>)}</div>;
}

function DiffSide({ label, side, location, onRoute }: { label: "From" | "To"; side: SymbolDiff["from"]; location: string; onRoute: (patch: Partial<Route>) => void }) {
  return <span className="inline-flex items-center gap-1.5" data-testid={`diff-side-${label.toLowerCase()}`}>
    <Muted>{label} {side.commit.slice(0, 12)}</Muted><code className="rounded bg-muted px-1.5 py-0.5 text-xs" title={side.snapshot_id}>{shortId(side.snapshot_id)}</code>
    <Button type="button" size="sm" variant="outline" aria-label={`Open ${label.toLowerCase()} snapshot in Explorer`}
      onClick={() => onRoute(snapshotExplorerPatch({ location, snapshot: side.snapshot_id }))}>Open in Explorer</Button>
  </span>;
}

/** A diff of the checkout at location; its snapshots and changed symbols open in the Explorer there. */
export function DiffResult({ diff, group, location, onRoute }: { diff: SymbolDiff; group: Route["historyGroup"]; location: string; onRoute: (patch: Partial<Route>) => void }) {
  const [opening, setOpening] = useState<{ pending: boolean; error?: string }>({ pending: false });
  async function openChange(row: SymbolChange) {
    setOpening({ pending: true });
    try {
      const target = changeTarget(diff, row);
      const browse = await browseModule(target.snapshot);
      const node = findChangedNode(browse.data.nodes, row, target.path);
      onRoute({ view: "explorer", ...symbolSelectionPatch({ location, snapshot: target.snapshot }, node) });
    } catch (reason) {
      setOpening({ pending: false, error: errorMessage(reason) });
    }
  }
  return <div className="flex min-w-0 flex-col gap-2">
    <div className="flex flex-wrap items-center gap-3 text-sm"><strong>{countChanges(diff)} changes · {diff.packages.length} packages</strong>
      <DiffSide label="From" side={diff.from} location={location} onRoute={onRoute} /><span aria-hidden>→</span><DiffSide label="To" side={diff.to} location={location} onRoute={onRoute} />
      {opening.pending && <Muted>Opening symbol…</Muted>}</div>
    <ErrorMessage error={opening.error} />
    <ErrorMessage error={diff.lines_error} /><DiffTree diff={diff} group={group} onOpen={openChange} />
  </div>;
}
