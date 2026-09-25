import type { ModuleQueryResult, ModuleQueryRow, ModuleSource } from "./api";
import type { Route } from "./route";

export function searchInputMode(value: string): { mode: "search" | "expression"; value: string } {
  return value.startsWith(">") ? { mode: "expression", value: value.slice(1).trimStart() } : { mode: "search", value };
}

export function queryPaletteRows(result: ModuleQueryResult): { matches: ModuleQueryRow[]; candidates: ModuleQueryRow[] } {
  if (result.path) return { matches: result.path.calls, candidates: [] };
  const candidates = result.matches.filter((row) => !row.path);
  for (const row of candidates) {
    if (!result.symbols.some((symbol) => symbol.id === row.symbol_id)) {
      throw new Error(`Candidate ${row.symbol_id} has no resolved symbol`);
    }
  }
  return { matches: result.matches.filter((row) => Boolean(row.path)), candidates };
}

export function queryMatchLabel(row: ModuleQueryRow): string {
  const qualified = row.symbol.slice(row.symbol.indexOf(":") + 1).split("#", 1)[0];
  return qualified.slice(qualified.lastIndexOf("/") + 1).replace(":", ".");
}

export function queryMatchRoute(row: ModuleQueryRow, sources: ModuleSource[]): Partial<Route> {
  if (!row.path) throw new Error(`Query result ${row.symbol} has no indexed file`);
  const matches = sources.filter((source) => source.root_key === row.root && source.location === row.location &&
    source.snapshot_id === row.snapshot_id && source.path === row.path);
  if (matches.length !== 1) throw new Error(`Expected one source for ${row.path} in ${row.snapshot_id}, found ${matches.length}`);
  return {
    view: "explorer", module: row.root, location: row.location, snapshot: row.snapshot_id,
    source: matches[0].id, node: "", line: row.line ?? 0, column: row.column ?? 0,
    fileSearch: "", symbolSearch: "",
  };
}
