import { lazy, Suspense, useEffect, useState } from "react";
import { Button, SegmentedControl } from "@flanksource/clicky-ui/components";
import { DataTable, ServerTimingBadge, type DataTableColumn } from "@flanksource/clicky-ui/data";
import { browseModule, errorMessage, type ModuleQueryResult, type ModuleQueryRow } from "./api";
import { queryMatchRoute } from "./query-palette-model";
import { CoverageWarning } from "./CoverageWarning";
import type { QueryExample } from "./query-model";
import type { Route } from "./route";
import { ErrorMessage, Heading, Muted, PanelForm } from "./ui";
import type { TimedLoad } from "./use-load";

type QueryTableRow = ModuleQueryRow & { id: string };
type ProjectionRow = { values: Record<string, unknown>; index: number };
const QueryExpressionInput = lazy(() => import("./QueryExpressionInput").then(({ QueryExpressionInput }) => ({ default: QueryExpressionInput })));
const QueryGraph = lazy(() => import("./QueryGraph").then(({ QueryGraph }) => ({ default: QueryGraph })));

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
  const [sourceError, setSourceError] = useState("");
  useEffect(() => setDraft(route.expression), [route.expression]);
  const declarations = query.data?.declarations ?? [];
  const symbols = query.data?.symbols ?? [];
  const projectedColumns: DataTableColumn<ProjectionRow>[] = (query.data?.columns ?? []).map((column) => ({
    key: column.name, label: column.name, grow: true, accessor: (row) => row.values[column.name],
    render: (value) => value == null ? "NULL" : typeof value === "object" ? JSON.stringify(value) : String(value),
  }));
  const openSource = async (row: ModuleQueryRow) => {
    setSourceError("");
    try { const { data } = await browseModule(row.snapshot_id); onRoute({ ...queryMatchRoute(row, data.sources), explorerTab: "source" }); }
    catch (error) { setSourceError(errorMessage(error)); }
  };

  return <>
    <Heading>Query</Heading><Muted>Run SQL or compact navigation {route.module ? "against the selected module snapshot." : "across the primary heads of indexed module roots."}</Muted>
    <PanelForm onSubmit={(event) => { event.preventDefault(); onRoute({ expression: draft.trim() }); }}>
      <div><label className="mb-1 block text-sm">Language</label><SegmentedControl aria-label="Query language" value={route.queryLanguage} onChange={(queryLanguage) => onRoute({ queryLanguage })}
        options={[{ id: "auto", label: "Auto" }, { id: "compact", label: "Compact" }, { id: "sql", label: "SQL" }]} /></div>
      <div className="flex flex-wrap items-end gap-3">
        <Suspense fallback={<div className="min-w-64 flex-1 text-sm text-muted-foreground">Loading expression editor…</div>}>
          <QueryExpressionInput draft={draft} route={route} path="file:///uir/query/expression.uirq"
            runError={draft === route.expression ? query.diagnostic ?? query.error : undefined} onChange={setDraft}
            onRun={() => { if (draft.trim()) onRoute({ expression: draft.trim() }); }} />
        </Suspense>
        <Button type="submit" disabled={!draft.trim()}>Run query</Button>
      </div>
      <div className="flex flex-wrap items-center gap-1.5" aria-label="Example queries">
        <Muted>Examples:</Muted>
        <Button type="button" size="sm" variant="outline" onClick={() => { setDraft("SELECT symbol.name, path, line FROM func:* ORDER BY symbol.name LIMIT 20"); onRoute({ queryLanguage: "auto" }); }}>SQL declarations</Button>
        {examples.map((example) => <Button key={example.id} type="button" size="sm" variant="outline" title={example.expression}
          onClick={() => { setDraft(example.expression); onRoute({ queryLanguage: "auto" }); }}>{example.label}</Button>)}
      </div>
    </PanelForm>
    <div className="flex items-center gap-3"><h2>Results {query.data ? `(${query.data.path ? 1 : query.data.graph?.nodes.length ?? query.data.matches.length} of ${query.data.total})` : ""}</h2><ServerTimingBadge metrics={query.timing} /></div>
    <ErrorMessage error={query.error} />
    <ErrorMessage error={sourceError} />
    {query.data && <CoverageWarning coverage={query.data.coverage} />}
    {symbols.length > 0 && <Muted>Resolved {symbols.length === 1 ? "symbol" : "symbols"}: {symbols.map((symbol) =>
      `${symbol.owner ? `${symbol.owner}.` : ""}${symbol.name} (${symbol.kind}, ${symbol.package_path})`).join("; ")}</Muted>}
    {declarations.length > 0 && <section aria-label="Declarations" className="flex flex-col gap-1">
      <h3 className="m-0 text-sm font-semibold">Declarations</h3>
      <ul className="m-0 list-none p-0 text-sm">
        {declarations.map((row, index) => <li key={`${row.source}:${index}`} className="break-all"><strong>{row.symbol}</strong> <Muted>{row.root} · {row.source} · {row.location}</Muted></li>)}
      </ul>
    </section>}
    {query.data?.path && <section aria-label="Call path"><h3>Shortest indexed path</h3><ol>
      {query.data.path.symbols.map((symbol, index) => {
        const witness = query.data?.path?.calls[index];
        return <li key={`${symbol.id}:${index}`}><code>{symbol.query_name ?? symbol.name}</code>
          {witness && <Button variant="ghost" size="sm" onClick={() => void openSource(witness)}>
            {witness.relationship?.kind ?? "call"} at {witness.source}{witness.dispatch ? " (interface dispatch)" : ""}</Button>}</li>;
      })}
    </ol></section>}
    {query.data?.graph && <Suspense fallback={<Muted>Loading graph…</Muted>}><QueryGraph graph={query.data.graph} route={route} onRoute={onRoute} /></Suspense>}
    {!query.data?.path && !query.data?.graph && (query.data?.columns ? <DataTable className="min-h-40 max-h-[28rem]" data={(query.data.rows ?? []).map((values, index) => ({ values, index }))}
      columns={projectedColumns} loading={query.loading} getRowId={(row) => String(row.index)} emptyMessage="No matches"
      onRowClick={(row) => { const match = query.data?.matches[row.index]; if (match?.path) void openSource(match); }} />
      : <DataTable className="min-h-40 max-h-[28rem]" data={tableRows(query.data?.matches ?? [])} loading={query.loading} getRowId={(row) => row.id}
        onRowClick={(row) => { if (row.path) void openSource(row); }} emptyMessage={route.expression ? "No matches" : "Enter a SQL or compact expression"} columns={resultColumns} />)}
  </>;
}
