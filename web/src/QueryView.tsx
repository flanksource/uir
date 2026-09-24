import { useEffect, useState } from "react";
import { Button } from "@flanksource/clicky-ui/components";
import { DataTable, type DataTableColumn } from "@flanksource/clicky-ui/data";
import type { ModuleQueryResult, ModuleQueryRow } from "./api";
import { CoverageWarning } from "./CoverageWarning";
import type { QueryExample } from "./query-model";
import type { Route } from "./route";
import { ErrorMessage, Field, Heading, Muted, PanelForm, TextInput } from "./ui";
import type { Load } from "./use-load";

type QueryTableRow = ModuleQueryRow & { id: string };

const resultColumns: DataTableColumn<QueryTableRow>[] = [
  { key: "kind", label: "Kind" },
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
  query: Load<ModuleQueryResult>;
  examples: QueryExample[];
  onRoute: (patch: Partial<Route>) => void;
}) {
  const [draft, setDraft] = useState(route.expression);
  useEffect(() => setDraft(route.expression), [route.expression]);
  const declarations = query.data?.declarations ?? [];
  const symbols = query.data?.symbols ?? [];

  return <>
    <Heading>PEG query</Heading><Muted>Run a symbol or call query against the selected module snapshot.</Muted>
    <PanelForm onSubmit={(event) => { event.preventDefault(); onRoute({ expression: draft.trim() }); }}>
      <div className="flex flex-wrap items-end gap-3">
        <Field label="Expression"><TextInput required value={draft} onChange={(event) => setDraft(event.target.value)} /></Field>
        <Button type="submit">Run query</Button>
      </div>
      <div className="flex flex-wrap items-center gap-1.5" aria-label="Example queries">
        <Muted>Examples:</Muted>
        {examples.map((example) => <Button key={example.id} type="button" size="sm" variant="outline" title={example.expression}
          onClick={() => setDraft(example.expression)}>{example.label}</Button>)}
      </div>
    </PanelForm>
    <h2>Results {query.data ? `(${query.data.matches.length} of ${query.data.total})` : ""}</h2>
    <ErrorMessage error={query.error} />
    {query.data && <CoverageWarning coverage={query.data.coverage} />}
    {symbols.length > 0 && <Muted>Resolved {symbols.length === 1 ? "symbol" : "symbols"}: {symbols.map((symbol) =>
      `${symbol.owner ? `${symbol.owner}.` : ""}${symbol.name} (${symbol.kind}, ${symbol.package_path})`).join("; ")}</Muted>}
    {declarations.length > 0 && <section aria-label="Declarations" className="flex flex-col gap-1">
      <h3 className="m-0 text-sm font-semibold">Declarations</h3>
      <ul className="m-0 list-none p-0 text-sm">
        {declarations.map((row, index) => <li key={`${row.source}:${index}`} className="break-all"><strong>{row.symbol}</strong> <Muted>{row.source} · {row.location}</Muted></li>)}
      </ul>
    </section>}
    <DataTable className="min-h-40 max-h-[28rem]" data={tableRows(query.data?.matches ?? [])} loading={query.loading} getRowId={(row) => row.id}
      emptyMessage={route.expression ? "No matches" : "Enter a PEG expression"} columns={resultColumns} />
  </>;
}
