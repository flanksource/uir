import { formatBytes, formatDuration } from "@flanksource/clicky-ui/utils";
import type { ModuleSnapshot } from "./api";

/** The id prefix the UI shows for snapshots and commits. */
export function shortId(id: string): string {
  return id.slice(0, 12);
}

function count(value: number, unit: string): string {
  return `${value} ${unit}${value === 1 ? "" : "s"}`;
}

/** The time from the start of indexing to publication; undefined for snapshots that predate index_started_at. */
export function snapshotDurationMs(row: Pick<ModuleSnapshot, "index_started_at" | "completed_at">): number | undefined {
  if (!row.index_started_at) return undefined;
  const elapsed = Date.parse(row.completed_at) - Date.parse(row.index_started_at);
  if (!Number.isFinite(elapsed) || elapsed < 0) {
    throw new Error(`Invalid snapshot duration from index_started_at ${row.index_started_at} to completed_at ${row.completed_at}`);
  }
  return elapsed;
}

export function snapshotDuration(row: Pick<ModuleSnapshot, "index_started_at" | "completed_at">): string {
  const elapsed = snapshotDurationMs(row);
  return elapsed === undefined ? "—" : formatDuration(elapsed);
}

export function snapshotSize(row: Pick<ModuleSnapshot, "file_count" | "symbol_count" | "source_bytes">): string {
  return `${count(row.file_count, "file")} · ${count(row.symbol_count, "symbol")} · ${formatBytes(row.source_bytes)}`;
}

/** The file and symbol changes against base_snapshot_id, which the counts are relative to. */
export function snapshotDelta(row: Pick<ModuleSnapshot, "files_added" | "files_changed" | "files_deleted" | "symbols_changed" | "base_snapshot_id">): string {
  const changes = `+${row.files_added} ~${row.files_changed} −${row.files_deleted} files · ${count(row.symbols_changed, "symbol")}`;
  return row.base_snapshot_id ? `${changes} vs ${shortId(row.base_snapshot_id)}` : `${changes} (no base)`;
}
