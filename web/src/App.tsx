import { lazy, Suspense, useCallback, useEffect, useState } from "react";
import { AppShell, Button, Select } from "@flanksource/clicky-ui/components";
import { DataTable, type DataTableColumn } from "@flanksource/clicky-ui/data";
import { addModules, browseModule, listModuleLocations, listModuleRoots, listModuleSnapshots, reindexModules, runModuleQuery, type ModuleBrowse, type ModuleIndexResult, type ModuleLocation, type ModuleQueryRow, type ModuleRoot, type ModuleSnapshot, type Page } from "./api";
import { readRoute, routeURL, type Route } from "./route";
import { Card, ErrorMessage, Field, Heading, Muted, PageLayout, PanelForm, Row, Section, TextInput } from "./ui";
import { useLoad } from "./use-load";

const ExplorerView = lazy(() => import("./ExplorerView").then((module) => ({ default: module.ExplorerView })));

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
      {root && <Button type="button" variant="outline" disabled={pending} onClick={(event: React.MouseEvent<HTMLButtonElement>) => submit(event, "add")}>Add another directory</Button>}
    </Row>
    <ErrorMessage error={error} />
    {message && <Muted>{message}</Muted>}
  </PanelForm>;
}

export function App() {
  const [route, setRouteState] = useState(readRoute);
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    const onPop = () => setRouteState(readRoute());
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);
  const setRoute = useCallback((patch: Partial<Route>, replace = false) => {
    setRouteState((current) => {
      const next = { ...current, ...patch };
      window.history[replace ? "replaceState" : "pushState"]({}, "", routeURL(next));
      return next;
    });
  }, []);
  useEffect(() => {
    if (window.location.pathname === "/nodes" || window.location.search.includes("search=")) {
      window.history.replaceState({}, "", routeURL(route));
    }
  }, [route]);

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
  const browse = useLoad<ModuleBrowse>(route.snapshot && route.view === "explorer" ? () => browseModule(route.snapshot) : null,
    `browse:${route.snapshot}:${route.view}:${refresh}`);
  const query = useLoad<ModuleQueryRow[]>(route.snapshot && route.view === "query" && route.expression ? () => runModuleQuery(route.expression, route.module, route.snapshot) : null,
    `query:${route.module}:${route.snapshot}:${route.expression}:${route.view}`);
  const [queryDraft, setQueryDraft] = useState(route.expression);
  useEffect(() => setQueryDraft(route.expression), [route.expression]);

  const queryRows = (query.data ?? []).map((row, index) => ({ ...row, id: `${row.snapshot_id}:${row.source}:${row.symbol}:${index}` }));
  const nav = ["overview", "explorer", "query"] as const;

  return <AppShell brand={<strong>UIR</strong>} contentWidth="full" contentClassName={route.view === "explorer" && route.snapshot ? "overflow-hidden" : undefined}
    navSections={[{ label: "Module browser", items: nav.map((view) => ({ key: view, label: view[0].toUpperCase() + view.slice(1), to: routeURL({ ...route, view, offset: 0 }), active: route.view === view })) }]}
    sidebarHeader={<Field label="Module root"><Select aria-label="Module root" value={route.module} onChange={(event) => {
      const next = roots.data?.find((item) => item.root_key === event.target.value);
      setRoute({ module: event.target.value, location: next?.location ?? "", snapshot: next?.snapshot_id ?? "", source: "", node: "", fileSearch: "", symbolSearch: "", offset: 0 });
    }} options={(roots.data ?? []).map((item) => ({ value: item.root_key, label: item.name || item.root_key }))} /></Field>}
    bodyHeader={<span>{selectedRoot?.name || route.module || "Module roots"} {route.snapshot && <Muted>/ {route.snapshot.slice(0, 12)}</Muted>}</span>}
    bodyActions={<Button variant="outline" onClick={() => setRefresh((current) => current + 1)}>Refresh</Button>}>
    {route.view === "explorer" && route.snapshot ? <Suspense fallback={<div className="p-3"><Muted>Loading explorer…</Muted></div>}><ExplorerView route={route} browse={browse} locations={locations} onRoute={setRoute} /></Suspense> : <PageLayout>
      {roots.loading && <Muted>Loading module roots…</Muted>}
      <ErrorMessage error={roots.error} />
      {route.view === "overview" && <>
        <Section><Heading>Indexed module roots</Heading><Muted>Each Go module path has one root and can have multiple registered checkouts.</Muted>
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">{(roots.data ?? []).map((item) => <Card key={item.root_key}><strong>{item.name || item.root_key}</strong><div><Muted>{item.root_key}</Muted></div><div>{item.location}</div><Muted>Primary head v{item.head_version}</Muted></Card>)}</div>
          {roots.data?.length === 0 && <p>No modules are indexed yet. Add a local directory below.</p>}
        </Section>
        {route.module && <Section><h2>Checkouts</h2><DataList rows={locations.data ?? []} loading={locations.loading} error={locations.error} columns={locationColumns}
          onClick={(row) => setRoute({ location: row.canonical_path, snapshot: row.head_snapshot_id, source: "", node: "", fileSearch: "", symbolSearch: "", offset: 0 })} /></Section>}
        {route.location && <Section><h2>Snapshots for {route.location}</h2><DataList rows={snapshots.data?.data ?? []} loading={snapshots.loading} error={snapshots.error} columns={snapshotColumns}
          onClick={(row) => setRoute({ snapshot: row.id, source: "", node: "", fileSearch: "", symbolSearch: "", offset: 0 })} total={snapshots.data?.page.total} offset={route.offset} onPage={(offset) => setRoute({ offset })} /></Section>}
        <IndexForm root={route.module} defaultPath={route.location} onSuccess={(results) => {
          const first = results[0];
          setRoute({ module: first.root_key, location: first.location, snapshot: first.snapshot_id, source: "", node: "", fileSearch: "", symbolSearch: "", offset: 0 });
          setRefresh((current) => current + 1);
        }} />
      </>}
      {route.view !== "overview" && !route.snapshot && <Card>Select or add a module snapshot on the Overview page.</Card>}
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
    </PageLayout>}
  </AppShell>;
}
