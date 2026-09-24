import { useMemo, useState } from "react";
import { Button, Tabs } from "@flanksource/clicky-ui/components";
import { runModuleQuery, type ModuleNode, type ModuleQueryResult, type ModuleQueryRow, type ModuleSource } from "./api";
import { CoverageWarning } from "./CoverageWarning";
import { FileTypeIcon } from "./file-icons";
import { groupRowsByFile, identityLabel, nodeIdentityKey, parseIdentityKey, referencesExpression, symbolSelector } from "./query-model";
import type { Route } from "./route";
import { CodeBlock, Detail, DetailGrid, ErrorMessage, Muted } from "./ui";
import { useLoad } from "./use-load";

type ReferenceRow = { row: ModuleQueryRow; sourceId: string; enclosing: string };
type References = { result: ModuleQueryResult; candidates: { row: ModuleQueryRow; queryName: string }[]; files: { path: string; rows: ReferenceRow[] }[] };
type OnRoute = (patch: Partial<Route>, replace?: boolean) => void;

// loadReferences runs the query and resolves every occurrence to a source of the explorer snapshot,
// so a row that cannot be opened fails the tab instead of failing on click.
async function loadReferences(expression: string, route: Route, sources: ModuleSource[]): Promise<References> {
  const { data: result } = await runModuleQuery(expression, route.module, route.snapshot);
  const { candidates, files } = groupRowsByFile(result.matches);
  const sourceIDs = new Map(sources.map((source) => [source.path, source.id]));
  return { result, candidates: candidates.map((row) => {
    if (!row.symbol_id) throw new Error(`Candidate ${row.symbol} has no symbol_id to select`);
    const symbol = result.symbols.find((candidate) => candidate.id === row.symbol_id);
    if (!symbol?.query_name) throw new Error(`Candidate ${row.symbol_id} has no compact query spelling`);
    return { row, queryName: symbol.query_name };
  }), files: files.map((file) => ({ path: file.path, rows: file.rows.map((row) => {
    if (row.snapshot_id !== route.snapshot) throw new Error(`Reference in ${row.path} is from snapshot ${row.snapshot_id}, not ${route.snapshot}`);
    const sourceId = sourceIDs.get(file.path);
    if (!sourceId) throw new Error(`Reference source ${file.path} is not in snapshot ${route.snapshot}`);
    return { row, sourceId, enclosing: row.enclosing_key ? identityLabel(row.enclosing_key) : "" };
  }) })) };
}

function useReferences(node: ModuleNode, route: Route, sources: ModuleSource[], queryName: string) {
  const selector = useMemo<{ expression?: string; error?: string }>(() => {
    if (queryName) return { expression: referencesExpression(queryName) };
    try { return { expression: referencesExpression(symbolSelector(parseIdentityKey(nodeIdentityKey(node)))) }; } catch (error) { return { error: `Cannot select ${node.symbol}: ${String(error)}` }; }
  }, [node, queryName]);
  const expression = selector.expression;
  const load = useLoad<References>(expression ? () => loadReferences(expression, route, sources) : null, `references:${route.module}:${route.snapshot}:${expression}`);
  return { expression, loading: load.loading, error: selector.error ?? load.error, data: load.data };
}

function ReferencesPanel({ references, onRoute, onCandidate }: { references: ReturnType<typeof useReferences>; onRoute: OnRoute; onCandidate: (queryName: string) => void }) {
  const { expression, data } = references;
  return <div className="flex flex-col gap-2 text-sm">
    {expression && <div className="flex items-start gap-2"><code className="min-w-0 flex-1 break-all text-xs text-muted-foreground">{expression}</code>
      <Button type="button" size="sm" variant="link" onClick={() => onRoute({ view: "query", expression })}>Open in query</Button></div>}
    {references.loading && <Muted>Finding references…</Muted>}
    <ErrorMessage error={references.error} />
    {data && <CoverageWarning coverage={data.result.coverage} />}
    {data && data.candidates.length > 0 && <section aria-label="Candidates" className="flex flex-col gap-1">
      <Muted>{data.candidates.length} symbols match this selector. Choose one:</Muted>
      {data.candidates.map(({ row, queryName }) => <Button key={`${queryName}:${row.source}`} type="button" size="sm" variant="outline" className="justify-start"
        onClick={() => onCandidate(queryName)}>{queryName} <Muted>{row.source}</Muted></Button>)}
    </section>}
    {data && !data.candidates.length && !data.files.length && <Muted>No references in this snapshot</Muted>}
    {data && data.result.total > data.result.matches.length && <Muted>Showing {data.result.matches.length} of {data.result.total} references</Muted>}
    {data?.files.map((file) => <section key={file.path} aria-label={file.path} className="flex flex-col">
      <div className="flex items-center gap-1.5 font-medium"><FileTypeIcon filename={file.path} /><span className="truncate" title={file.path}>{file.path}</span></div>
      {file.rows.map(({ row, sourceId, enclosing }, index) => <button key={`${row.line}:${row.column}:${index}`} type="button"
        className="flex min-w-0 items-baseline gap-2 rounded px-2 py-0.5 text-left hover:bg-muted"
        onClick={() => onRoute({ source: sourceId, line: row.line ?? 0, column: row.column ?? 0 })}>
        <span className="shrink-0 font-mono text-xs">{row.line}:{row.column}</span>
        <span className="shrink-0 text-xs text-muted-foreground">{row.role}{row.dispatch ? " (dispatch)" : ""}</span>
        <span className="truncate" title={row.enclosing_key}>{enclosing}</span>
      </button>)}
    </section>)}
  </div>;
}

export function SymbolDetails({ node, route, sources, onRoute }: { node?: ModuleNode; route: Route; sources: ModuleSource[]; onRoute: OnRoute }) {
  if (!node) return <div className="p-3"><Muted>Select a symbol to inspect its payload, field, calls, and references.</Muted></div>;
  return <SelectedSymbol node={node} route={route} sources={sources} onRoute={onRoute} />;
}

function SelectedSymbol({ node, route, sources, onRoute }: { node: ModuleNode; route: Route; sources: ModuleSource[]; onRoute: OnRoute }) {
  const [tab, setTab] = useState("overview");
  const [candidate, setCandidate] = useState({ node: "", queryName: "" });
  const references = useReferences(node, route, sources, candidate.node === node.id ? candidate.queryName : "");
  return <div className="flex min-w-0 flex-col gap-3 p-3">
    <strong className="break-all text-sm">{node.symbol || node.id}</strong>
    <Tabs value={tab} onChange={setTab} tabs={[
      { id: "overview", label: "Overview" }, { id: "field", label: "Field", count: node.field ? 1 : 0 },
      { id: "calls", label: "Calls", count: node.calls.length }, { id: "references", label: "References", count: references.data?.result.total }, { id: "raw", label: "Raw" },
    ]} />
    {tab === "overview" && <>
      <DetailGrid>
        <Detail label="Kind">{node.node_type}</Detail>
        <Detail label="Source">{node.path}{node.line ? `:${node.line}` : ""}</Detail>
        <Detail label="Child slot">{node.child_slot}</Detail>
        <Detail label="Semantic hash">{node.semantic_hash.slice(0, 12)}</Detail>
      </DetailGrid>
      <h3>UIR payload</h3><CodeBlock>{JSON.stringify(node.payload, null, 2)}</CodeBlock>
    </>}
    {tab === "field" && (node.field ? <CodeBlock>{JSON.stringify(node.field, null, 2)}</CodeBlock> : <Muted>No field projection</Muted>)}
    {tab === "calls" && (node.calls.length ? <CodeBlock>{JSON.stringify(node.calls, null, 2)}</CodeBlock> : <Muted>No outgoing calls</Muted>)}
    {tab === "references" && <ReferencesPanel references={references} onRoute={onRoute} onCandidate={(queryName) => setCandidate({ node: node.id, queryName })} />}
    {tab === "raw" && <CodeBlock>{JSON.stringify(node, null, 2)}</CodeBlock>}
  </div>;
}
