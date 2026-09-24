import { lazy, Suspense, useEffect, useState } from "react";
import { Button } from "@flanksource/clicky-ui/components";
import { DataTable, ServerTimingBadge, type DataTableColumn } from "@flanksource/clicky-ui/data";
import type { ModuleQueryResult, ModuleQueryRow } from "./api";
import { CoverageWarning } from "./CoverageWarning";
import type { QueryExample } from "./query-model";
import type { Route } from "./route";
import { ErrorMessage, Heading, Muted, PanelForm } from "./ui";
import type { TimedLoad } from "./use-load";

type QueryTableRow = ModuleQueryRow & { id: string };
const QueryExpressionInput = lazy(() => import("./QueryExpressionInput").then(({ QueryExpressionInput }) => ({ default: QueryExpressionInput })));

const resultColumns: DataTableColumn<QueryTableRow>[] = [
  { key: "kind", label: "Kind" },
  { key: "root", label: "Root", grow: true },
  { key: "symbol", label: "Symbol", grow: true },
  { key: "role", label: "Role", render: (_, row) => `${row.role ?? ""}${row.dispatch ? " (dispatch)" : ""}` },
  { key: "source", label: "Source", grow: true },
  { key: "location", label: "Checkout", grow: true },
];

function tableRows(rows: ModuleQueryRow[]): QueryTableRow[] {
  return rows.map((row, index) => ({ ...row, id: `${row.snapshot_id}:${row.source}:${row.symbol_id ?? row.symbol}:${index}` }));
}

export function QueryView({ route, query, examples, onRoute }: {
  route: Route;
  query: TimedLoad<ModuleQueryResult>;
  examples: QueryExample[];
  onRoute: (patch: Partial<Route>) => void;
}) {
  const [draft, setDraft] = useState(route.expression);
  useEffect(() => setDraft(route.expression), [route.expression]);
  const declarations = query.data?.declarations ?? [];
  const symbols = query.data?.symbols ?? [];

  return <>
    <Heading>Compact query</Heading><Muted>Run a symbol or call query {route.module ? "against the selected module snapshot." : "across the primary heads of indexed module roots."}</Muted>
    <PanelForm onSubmit={(event) => { event.preventDefault(); onRoute({ expression: draft.trim() }); }}>
      <div className="flex flex-wrap items-end gap-3">
        <Suspense fallback={<div className="min-w-64 flex-1 text-sm text-muted-foreground">Loading expression editor…</div>}>
          <QueryExpressionInput draft={draft} route={route} onChange={setDraft} />
        </Suspense>
        <Button type="submit" disabled={!draft.trim()}>Run query</Button>
      </div>
      <div className="flex flex-wrap items-center gap-1.5" aria-label="Example queries">
        <Muted>Examples:</Muted>
        {examples.map((example) => <Button key={example.id} type="button" size="sm" variant="outline" title={example.expression}
          onClick={() => setDraft(example.expression)}>{example.label}</Button>)}
      </div>
    </PanelForm>
    <div className="flex items-center gap-3"><h2>Results {query.data ? `(${query.data.path ? 1 : query.data.matches.length} of ${query.data.total})` : ""}</h2><ServerTimingBadge metrics={query.timing} /></div>
    <ErrorMessage error={query.error} />
    {query.data && <CoverageWarning coverage={query.data.coverage} />}
    {symbols.length > 0 && <Muted>Resolved {symbols.length === 1 ? "symbol" : "symbols"}: {symbols.map((symbol) =>
      `${symbol.owner ? `${symbol.owner}.` : ""}${symbol.name} (${symbol.kind}, ${symbol.package_path})`).join("; ")}</Muted>}
    {declarations.length > 0 && <section aria-label="Declarations" className="flex flex-col gap-1">
      <h3 className="m-0 text-sm font-semibold">Declarations</h3>
      <ul className="m-0 list-none p-0 text-sm">
        {declarations.map((row, index) => <li key={`${row.source}:${index}`} className="break-all"><strong>{row.symbol}</strong> <Muted>{row.root} · {row.source} · {row.location}</Muted></li>)}
      </ul>
    </section>}
    {query.data?.path && <section aria-label="Call path"><h3>Shortest indexed call path</h3><ol>
      {query.data.path.symbols.map((symbol, index) => <li key={`${symbol.id}:${index}`}><code>{symbol.query_name ?? symbol.name}</code>
        {query.data?.path?.calls[index] && <Muted> calls at {query.data.path.calls[index].source}{query.data.path.calls[index].dispatch ? " (interface dispatch)" : ""}</Muted>}</li>)}
    </ol></section>}
    {!query.data?.path && <DataTable className="min-h-40 max-h-[28rem]" data={tableRows(query.data?.matches ?? [])} loading={query.loading} getRowId={(row) => row.id}
      emptyMessage={route.expression ? (query.data?.operation === "path" ? "No indexed call path within the depth bound" : "No matches") : "Enter a compact expression"} columns={resultColumns} />}
  </>;
}
