import { useEffect, useState } from "react";
import { AppShell, Button, Select, Tabs } from "@flanksource/clicky-ui/components";
import { DataTable, type DataTableColumn } from "@flanksource/clicky-ui/data";
import { addModules, browseModule, listModuleLocations, listModuleRoots, listModuleSnapshots, readModuleSource, reindexModules, runModuleQuery, type ModuleBrowse, type ModuleIndexResult, type ModuleLocation, type ModuleNode, type ModuleQueryRow, type ModuleRoot, type ModuleSnapshot, type ModuleSource, type ModuleSourceContent, type Page } from "./api";
import { readRoute, routeURL, type Route } from "./route";
import { Card, CodeBlock, Detail, DetailGrid, Field, Heading, Muted, PageLayout, PanelForm, Row, Section, TextInput } from "./ui";

type Load<T> = { data?: T; error?: string; loading: boolean };

function useLoad<T>(load: (() => Promise<T>) | null, key: string): Load<T> {
  const [result, setResult] = useState<Load<T>>({ loading: Boolean(load) });
  useEffect(() => {
    if (!load) {
      setResult({ loading: false });
      return;
    }
    let active = true;
    setResult({ loading: true });
    load().then((data) => { if (active) setResult({ data, loading: false }); }, (error: unknown) => {
      if (active) setResult({ error: String(error), loading: false });
    });
    return () => { active = false; };
    // key names every input that changes the request.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  return result;
}

function ErrorMessage({ error }: { error?: string }) {
  return error ? <div role="alert" className="whitespace-pre-wrap rounded-md border border-destructive p-3 text-destructive">{error}</div> : null;
}

function DataList<T extends { id: string }>({ rows, loading, error, columns, onClick, total, offset, onPage }: {
  rows: T[];
  loading: boolean;
  error?: string;
  columns: DataTableColumn<T>[];
  onClick?: (row: T) => void;
  total?: number;
  offset?: number;
  onPage?: (offset: number) => void;
}) {
  return <>
    <ErrorMessage error={error} />
    <DataTable className="min-h-40 max-h-[28rem]" data={rows} columns={columns} loading={loading} getRowId={(row) => row.id} onRowClick={onClick}
      emptyMessage="No saved records match this view"
      pagination={total !== undefined && onPage ? { page: Math.floor((offset ?? 0) / 100), pageSize: 100, total,
        onPageChange: (next) => onPage(next * 100), onPageSizeChange: () => onPage(0), pageSizeOptions: [100] } : undefined} />
  </>;
}

const locationColumns: DataTableColumn<ModuleLocation>[] = [
  { key: "canonical_path", label: "Checkout", sortable: true, grow: true },
  { key: "kind", label: "Kind" },
  { key: "primary", label: "Primary", render: (_, row) => row.primary ? "Yes" : "" },
  { key: "head_version", label: "Head", render: (_, row) => `v${row.head_version}` },
];
const snapshotColumns: DataTableColumn<ModuleSnapshot>[] = [
  { key: "id", label: "Snapshot", render: (_, row) => row.id.slice(0, 12), sortable: true },
  { key: "state", label: "State", sortable: true },
  { key: "head", label: "Published", render: (_, row) => row.head ? `Head v${row.head_version}` : "" },
  { key: "revision", label: "Git revision", render: (_, row) => row.revision?.slice(0, 12) ?? "" },
  { key: "started_at", label: "Started", sortable: true },
];
const sourceColumns: DataTableColumn<ModuleSource>[] = [
  { key: "path", label: "Source", sortable: true, grow: true },
  { key: "package_path", label: "Package", grow: true },
  { key: "size_bytes", label: "Bytes" },
];
const nodeColumns: DataTableColumn<ModuleNode>[] = [
  { key: "symbol", label: "Symbol", sortable: true, grow: true },
  { key: "node_type", label: "Kind", sortable: true },
  { key: "path", label: "Source", grow: true },
  { key: "line", label: "Line" },
];

function IndexForm({ root, defaultPath, onSuccess }: { root: string; defaultPath: string; onSuccess: (results: ModuleIndexResult[]) => void }) {
  const [path, setPath] = useState(defaultPath);
  const [includeTests, setIncludeTests] = useState(false);
  const [force, setForce] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  useEffect(() => setPath(defaultPath), [defaultPath]);

  async function submit(event: React.FormEvent, action: "add" | "reindex") {
    event.preventDefault();
    setPending(true);
    setError("");
    setMessage("");
    try {
      const results = action === "add" ? await addModules(path.trim(), includeTests) : await reindexModules(path.trim(), includeTests, force);
      if (!results.length) throw new Error(`No Go modules found under ${path}`);
      setMessage(`${action === "add" ? "Added" : "Reindexed"} ${results.length} module ${results.length === 1 ? "root" : "roots"}`);
      onSuccess(results);
    } catch (reason) { setError(String(reason)); }
    finally { setPending(false); }
  }

  return <PanelForm onSubmit={(event) => submit(event, root ? "reindex" : "add")}>
    <h2>{root ? "Reindex a checkout" : "Add a directory"}</h2>
    <Muted>Discover Go modules from a local directory. Each module path becomes a root; each checkout keeps its own snapshot history.</Muted>
    <Field label="Local path"><TextInput required value={path} onChange={(event) => setPath(event.target.value)} /></Field>
    <Row>
      <label className="inline-flex items-center gap-1.5 text-sm"><input type="checkbox" checked={includeTests} onChange={(event) => setIncludeTests(event.target.checked)} />Include Go tests</label>
      {root && <label className="inline-flex items-center gap-1.5 text-sm"><input type="checkbox" checked={force} onChange={(event) => setForce(event.target.checked)} />Force reparse</label>}
      <Button type="submit" loading={pending}>{root ? "Reindex" : "Add"}</Button>
      {root && <Button type="button" variant="outline" disabled={pending} onClick={(event) => submit(event, "add")}>Add another directory</Button>}
    </Row>
    <ErrorMessage error={error} />
    {message && <Muted>{message}</Muted>}
  </PanelForm>;
}

function SourceView({ snapshot, source }: { snapshot: string; source?: ModuleSource }) {
  const content = useLoad<ModuleSourceContent>(source ? () => readModuleSource(snapshot, source.path) : null, `${snapshot}:${source?.id}`);
  if (!source) return <Muted>Select a source to view its verified content.</Muted>;
  return <Card>
    <strong>{source.path}</strong>
    <div><Muted>{source.package_path} · SHA-256 {source.content_hash.slice(0, 12)}</Muted></div>
    {content.loading && <Muted>Loading source…</Muted>}
    <ErrorMessage error={content.error} />
    {content.data && <><Muted>{content.data.origin === "git" ? `Pinned Git revision ${content.data.revision}` : "Local file matches indexed hash"}</Muted><CodeBlock>{content.data.content}</CodeBlock></>}
  </Card>;
}

function NodeView({ node, onSource }: { node?: ModuleNode; onSource: (sourceID: string) => void }) {
  const [tab, setTab] = useState("overview");
  if (!node) return <Muted>Select a symbol for its payload, field, and outgoing calls.</Muted>;
  return <Card>
    <strong>{node.symbol || node.id}</strong>
    <Tabs value={tab} onChange={setTab} tabs={[
      { id: "overview", label: "Overview" }, { id: "field", label: "Field", count: node.field ? 1 : 0 },
      { id: "calls", label: "Calls", count: node.calls.length }, { id: "raw", label: "Raw" },
    ]} />
    {tab === "overview" && <>
      <DetailGrid>
        <Detail label="Kind">{node.node_type}</Detail>
        <Detail label="Source"><button type="button" className="cursor-pointer border-0 bg-transparent p-0 text-primary underline" onClick={() => onSource(node.source_id)}>{node.path}{node.line ? `:${node.line}` : ""}</button></Detail>
        <Detail label="Child slot">{node.child_slot}</Detail>
        <Detail label="Semantic hash">{node.semantic_hash.slice(0, 12)}</Detail>
      </DetailGrid>
      <h3>UIR payload</h3><CodeBlock>{JSON.stringify(node.payload, null, 2)}</CodeBlock>
    </>}
    {tab === "field" && (node.field ? <CodeBlock>{JSON.stringify(node.field, null, 2)}</CodeBlock> : <Muted>No field projection</Muted>)}
    {tab === "calls" && (node.calls.length ? <CodeBlock>{JSON.stringify(node.calls, null, 2)}</CodeBlock> : <Muted>No outgoing calls</Muted>)}
    {tab === "raw" && <CodeBlock>{JSON.stringify(node, null, 2)}</CodeBlock>}
  </Card>;
}

function pageRows<T>(rows: T[], offset: number): T[] { return rows.slice(offset, offset + 100); }

export function App() {
  const [route, setRouteState] = useState(readRoute);
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    const onPop = () => setRouteState(readRoute());
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);
  function setRoute(patch: Partial<Route>, replace = false) {
    const next = { ...route, ...patch };
    window.history[replace ? "replaceState" : "pushState"]({}, "", routeURL(next));
    setRouteState(next);
  }

  const roots = useLoad<ModuleRoot[]>(listModuleRoots, String(refresh));
  const selectedRoot = roots.data?.find((item) => item.root_key === route.module);
  useEffect(() => {
    if (!route.module && roots.data?.length) setRoute({ module: roots.data[0].root_key, location: roots.data[0].location, snapshot: roots.data[0].snapshot_id }, true);
    // A missing selection resolves to the first indexed module.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [roots.data, route.module]);
  const locations = useLoad<ModuleLocation[]>(route.module ? () => listModuleLocations(route.module) : null, `locations:${route.module}:${refresh}`);
  useEffect(() => {
    if (route.module && !route.location && selectedRoot) setRoute({ location: selectedRoot.location, snapshot: selectedRoot.snapshot_id }, true);
    // The root's primary checkout is the default URL location.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [route.module, route.location, selectedRoot]);
  const selectedLocation = locations.data?.find((item) => item.canonical_path === route.location);
  useEffect(() => {
    if (route.location && !route.snapshot && selectedLocation) setRoute({ snapshot: selectedLocation.head_snapshot_id }, true);
    // A selected checkout resolves to its current head only without an explicit snapshot.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [route.location, route.snapshot, selectedLocation]);
  const snapshots = useLoad<Page<ModuleSnapshot>>(route.module && route.location ? () => listModuleSnapshots(route.module, route.location, route.offset) : null,
    `snapshots:${route.module}:${route.location}:${route.offset}:${refresh}`);
  const browse = useLoad<ModuleBrowse>(route.snapshot && (route.view === "explorer" || route.view === "nodes") ? () => browseModule(route.snapshot) : null,
    `browse:${route.snapshot}:${route.view}:${refresh}`);
  const query = useLoad<ModuleQueryRow[]>(route.snapshot && route.view === "query" && route.expression ? () => runModuleQuery(route.expression, route.module, route.snapshot) : null,
    `query:${route.module}:${route.snapshot}:${route.expression}:${route.view}`);
  const [queryDraft, setQueryDraft] = useState(route.expression);
  useEffect(() => setQueryDraft(route.expression), [route.expression]);

  const sources = (browse.data?.sources ?? []).filter((source) => source.path.toLowerCase().includes(route.search.toLowerCase()));
  const nodes = (browse.data?.nodes ?? []).filter((node) => (!route.source || node.source_id === route.source) && node.symbol.toLowerCase().includes(route.search.toLowerCase()));
  const selectedSource = browse.data?.sources.find((source) => source.id === route.source);
  const selectedNode = browse.data?.nodes.find((node) => node.id === route.node);
  const queryRows = (query.data ?? []).map((row, index) => ({ ...row, id: `${row.snapshot_id}:${row.source}:${row.symbol}:${index}` }));
  const nav = ["overview", "explorer", "nodes", "query"] as const;

  return <AppShell brand={<strong>UIR</strong>} contentWidth="full"
    navSections={[{ label: "Module browser", items: nav.map((view) => ({ key: view, label: view[0].toUpperCase() + view.slice(1), to: routeURL({ ...route, view, offset: 0 }), active: route.view === view })) }]}
    sidebarHeader={<Field label="Module root"><Select aria-label="Module root" value={route.module} onChange={(event) => {
      const next = roots.data?.find((item) => item.root_key === event.target.value);
      setRoute({ module: event.target.value, location: next?.location ?? "", snapshot: next?.snapshot_id ?? "", source: "", node: "", offset: 0 });
    }} options={(roots.data ?? []).map((item) => ({ value: item.root_key, label: item.name || item.root_key }))} /></Field>}
    bodyHeader={<span>{selectedRoot?.name || route.module || "Module roots"} {route.snapshot && <Muted>/ {route.snapshot.slice(0, 12)}</Muted>}</span>}
    bodyActions={<Button variant="outline" onClick={() => setRefresh((current) => current + 1)}>Refresh</Button>}>
    <PageLayout>
      {roots.loading && <Muted>Loading module roots…</Muted>}
      <ErrorMessage error={roots.error} />
      {route.view === "overview" && <>
        <Section><Heading>Indexed module roots</Heading><Muted>Each Go module path has one root and can have multiple registered checkouts.</Muted>
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">{(roots.data ?? []).map((item) => <Card key={item.root_key}><strong>{item.name || item.root_key}</strong><div><Muted>{item.root_key}</Muted></div><div>{item.location}</div><Muted>Primary head v{item.head_version}</Muted></Card>)}</div>
          {roots.data?.length === 0 && <p>No modules are indexed yet. Add a local directory below.</p>}
        </Section>
        {route.module && <Section><h2>Checkouts</h2><DataList rows={locations.data ?? []} loading={locations.loading} error={locations.error} columns={locationColumns}
          onClick={(row) => setRoute({ location: row.canonical_path, snapshot: row.head_snapshot_id, source: "", node: "", offset: 0 })} /></Section>}
        {route.location && <Section><h2>Snapshots for {route.location}</h2><DataList rows={snapshots.data?.data ?? []} loading={snapshots.loading} error={snapshots.error} columns={snapshotColumns}
          onClick={(row) => setRoute({ snapshot: row.id, source: "", node: "", offset: 0 })} total={snapshots.data?.page.total} offset={route.offset} onPage={(offset) => setRoute({ offset })} /></Section>}
        <IndexForm root={route.module} defaultPath={route.location} onSuccess={(results) => {
          const first = results[0];
          setRoute({ module: first.root_key, location: first.location, snapshot: first.snapshot_id, source: "", node: "", offset: 0 });
          setRefresh((current) => current + 1);
        }} />
      </>}
      {route.view !== "overview" && !route.snapshot && <Card>Select or add a module snapshot on the Overview page.</Card>}
      {route.view === "explorer" && route.snapshot && <>
        <Heading>Source explorer</Heading>
        <Row><Field label="Checkout"><Select value={route.location} onChange={(event) => {
          const next = locations.data?.find((item) => item.canonical_path === event.target.value);
          setRoute({ location: event.target.value, snapshot: next?.head_snapshot_id ?? "", source: "", node: "", offset: 0 });
        }} options={(locations.data ?? []).map((item) => ({ value: item.canonical_path, label: item.canonical_path }))} /></Field>
          <Field label="Path search"><TextInput value={route.search} onChange={(event) => setRoute({ search: event.target.value, offset: 0 })} /></Field></Row>
        <Section><h2>Sources</h2><DataList rows={pageRows(sources, route.offset)} loading={browse.loading} error={browse.error} columns={sourceColumns}
          onClick={(row) => setRoute({ source: row.id })} total={sources.length} offset={route.offset} onPage={(offset) => setRoute({ offset })} /></Section>
        <SourceView snapshot={route.snapshot} source={selectedSource} />
      </>}
      {route.view === "nodes" && route.snapshot && <>
        <Heading>Symbols</Heading>
        <Row><Field label="Symbol search"><TextInput value={route.search} onChange={(event) => setRoute({ search: event.target.value, offset: 0 })} /></Field></Row>
        {route.source && <Button variant="outline" onClick={() => setRoute({ source: "", offset: 0 })}>Clear source filter</Button>}
        <DataList rows={pageRows(nodes, route.offset)} loading={browse.loading} error={browse.error} columns={nodeColumns}
          onClick={(row) => setRoute({ node: row.id })} total={nodes.length} offset={route.offset} onPage={(offset) => setRoute({ offset })} />
        <NodeView node={selectedNode} onSource={(source) => setRoute({ view: "explorer", source, offset: 0 })} />
      </>}
      {route.view === "query" && route.snapshot && <>
        <Heading>PEG query</Heading><Muted>Run a symbol or call query against the selected module snapshot.</Muted>
        <PanelForm layout="row" onSubmit={(event) => { event.preventDefault(); setRoute({ expression: queryDraft.trim() }); }}>
          <Field label="Expression"><TextInput required value={queryDraft} onChange={(event) => setQueryDraft(event.target.value)} /></Field>
          <Button type="submit">Run query</Button>
        </PanelForm>
        <h2>Results {query.data ? `(${query.data.length})` : ""}</h2>
        <ErrorMessage error={query.error} />
        <DataTable className="min-h-40 max-h-[28rem]" data={queryRows} loading={query.loading} getRowId={(row) => row.id} emptyMessage={route.expression ? "No matches" : "Enter a PEG expression"}
          columns={[{ key: "kind", label: "Kind" }, { key: "symbol", label: "Symbol", grow: true }, { key: "root", label: "Root" }, { key: "location", label: "Checkout", grow: true }, { key: "source", label: "Source", grow: true }]} />
      </>}
    </PageLayout>
  </AppShell>;
}
