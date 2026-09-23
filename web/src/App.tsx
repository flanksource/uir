import { useEffect, useState } from "react";
import { AppShell, Button, Select, Tabs } from "@flanksource/clicky-ui/components";
import { DataTable, type DataTableColumn } from "@flanksource/clicky-ui/data";
import { getRow, getSourceContent, listProjects, listRows, runQuery, runReindex, type BrowseRow, type NodeDetails, type Page, type Project, type QueryRow, type SourceContent } from "./api";
import { readRoute, routeURL, type Route } from "./route";
import { repositoryURL } from "./repository";
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

function DataList({ rows, loading, error, columns, onClick, page, onPage }: {
  rows: BrowseRow[];
  loading: boolean;
  error?: string;
  columns: DataTableColumn<BrowseRow>[];
  onClick?: (row: BrowseRow) => void;
  page?: Page<BrowseRow>["page"];
  onPage?: (offset: number) => void;
}) {
  return <>
    <ErrorMessage error={error} />
    <DataTable className="min-h-40 max-h-[28rem]" data={rows} columns={columns} loading={loading} getRowId={(row) => row.id} onRowClick={onClick}
      emptyMessage="No saved records match this view"
      pagination={page && onPage ? { page: Math.floor(page.offset / page.limit), pageSize: page.limit, total: page.total,
        onPageChange: (next) => onPage(next * page.limit), onPageSizeChange: () => onPage(0), pageSizeOptions: [100] } : undefined} />
  </>;
}

const snapshotColumns: DataTableColumn<BrowseRow>[] = [
  { key: "id", label: "Snapshot", render: (_, row) => row.id.slice(0, 12), sortable: true },
  { key: "state", label: "State", sortable: true },
  { key: "head", label: "Published", render: (_, row) => row.head ? `Head v${row.version}` : "", sortable: true },
  { key: "started_at", label: "Started", sortable: true },
];
const rootColumns: DataTableColumn<BrowseRow>[] = [
  { key: "name", label: "Root", sortable: true, grow: true },
  { key: "kind", label: "Kind" },
  { key: "revision", label: "Revision", render: (_, row) => row.revision?.slice(0, 12) ?? "" },
  { key: "local_path", label: "Local path", grow: true },
];
const sourceColumns: DataTableColumn<BrowseRow>[] = [
  { key: "path", label: "Source", sortable: true, grow: true },
  { key: "language", label: "Language" },
  { key: "kind", label: "Kind" },
];
const nodeColumns: DataTableColumn<BrowseRow>[] = [
  { key: "name", label: "Symbol", sortable: true, grow: true },
  { key: "node_type", label: "Kind", sortable: true },
  { key: "language", label: "Language" },
  { key: "root_id", label: "Root", render: (_, row) => row.root_id?.slice(0, 8) ?? "" },
];

function ReindexForm({ project, defaultPath, onSuccess }: { project: string; defaultPath: string; onSuccess: (snapshot: string, project: string) => void }) {
  const [projectKey, setProjectKey] = useState(project);
  const [path, setPath] = useState(defaultPath);
  const [root, setRoot] = useState("");
  const [name, setName] = useState("");
  const [includeTests, setIncludeTests] = useState(false);
  const [force, setForce] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => setProjectKey(project), [project]);
  useEffect(() => { if (!path) setPath(defaultPath); }, [defaultPath, path]);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setPending(true);
    setError("");
    try {
      const result = await runReindex(projectKey.trim(), { path: path.trim(), root: root.trim(), name: name.trim(), "include-tests": includeTests, force });
      onSuccess(result.snapshot_id, projectKey.trim());
    } catch (reason) { setError(String(reason)); }
    finally { setPending(false); }
  }

  return <PanelForm onSubmit={submit}>
    <h2>Reindex a workspace</h2>
    <Muted>Publish an updated snapshot from a local checkout. The selected snapshot changes when indexing succeeds.</Muted>
    <Row>
      <Field label="Project key"><TextInput required value={projectKey} onChange={(event) => setProjectKey(event.target.value)} /></Field>
      <Field label="Local path"><TextInput required value={path} onChange={(event) => setPath(event.target.value)} /></Field>
    </Row>
    <Row>
      <Field label="Root key (optional)"><TextInput value={root} onChange={(event) => setRoot(event.target.value)} /></Field>
      <Field label="Project name (optional)"><TextInput value={name} onChange={(event) => setName(event.target.value)} /></Field>
    </Row>
    <Row>
      <label className="inline-flex items-center gap-1.5 text-sm"><input type="checkbox" checked={includeTests} onChange={(event) => setIncludeTests(event.target.checked)} />Include Go tests</label>
      <label className="inline-flex items-center gap-1.5 text-sm"><input type="checkbox" checked={force} onChange={(event) => setForce(event.target.checked)} />Force reparse</label>
      <Button type="submit" loading={pending}>Reindex</Button>
    </Row>
    <ErrorMessage error={error} />
  </PanelForm>;
}

function SourceView({ source }: { source: string }) {
  const content = useLoad<SourceContent>(source ? () => getSourceContent(source) : null, source);
  if (!source) return <Muted>Select a source to view its saved reference.</Muted>;
  if (content.loading) return <Muted>Loading source…</Muted>;
  if (content.error) return <ErrorMessage error={content.error} />;
  if (!content.data) return null;
  const item = content.data;
  return <Card>
    <strong>{item.path}</strong>
    <div><Muted>{item.origin === "git" ? `Git revision ${item.revision}` : item.origin === "local" ? "Current local file; no hash verification" : "Remote Git source"}</Muted></div>
    {item.origin === "remote" ? <div className="break-all">
      <p>Source content is unavailable locally. Open the repository to inspect this revision.</p>
      {item.repository && repositoryURL(item.repository) ? <a className="break-all text-primary underline" href={repositoryURL(item.repository)!} target="_blank" rel="noreferrer">{item.repository}</a> : <span>{item.repository}</span>}
      <p>Revision: {item.revision}</p>
    </div> : <CodeBlock>{item.content}</CodeBlock>}
  </Card>;
}

function NodeView({ node, onSource }: { node: string; onSource: (source: string) => void }) {
  const details = useLoad<NodeDetails>(node ? () => getRow("node", node) as Promise<NodeDetails> : null, node);
  const [tab, setTab] = useState("overview");
  if (!node) return <Muted>Select a node for fields, locations, and relationships.</Muted>;
  if (details.loading) return <Muted>Loading node…</Muted>;
  if (details.error) return <ErrorMessage error={details.error} />;
  if (!details.data) return null;
  const { node: record, fields, locations, relationships } = details.data;
  return <Card>
    <strong>{String(record.SymbolKey || record.IdentityKey || node)}</strong>
    <Tabs value={tab} onChange={setTab} tabs={[
      { id: "overview", label: "Overview" }, { id: "fields", label: "Fields", count: fields.length },
      { id: "relationships", label: "Relationships", count: relationships.length }, { id: "raw", label: "Raw" },
    ]} />
    {tab === "overview" && <>
      <DetailGrid>
        <Detail label="Kind">{String(record.NodeType || "")}</Detail>
        <Detail label="Language">{String(record.Language || "")}</Detail>
        <Detail label="Package">{String(record.Package || "")}</Detail>
        <Detail label="Signature">{String(record.Signature || "")}</Detail>
      </DetailGrid>
      <h3>Locations</h3>
      {locations.length ? <ul>{locations.map((location, index) => <li key={String(location.ID || index)}>
        <button type="button" className="cursor-pointer border-0 bg-transparent p-0 text-primary underline" onClick={() => onSource(String(location.SourceID))}>Source {String(location.SourceID).slice(0, 8)}</button>
        {location.StartLine ? `:${String(location.StartLine)}` : ""}{location.EndLine && location.EndLine !== location.StartLine ? `–${String(location.EndLine)}` : ""}
      </li>)}</ul> : <Muted>No source locations</Muted>}
    </>}
    {tab === "fields" && (fields.length ? <CodeBlock>{JSON.stringify(fields, null, 2)}</CodeBlock> : <Muted>No fields</Muted>)}
    {tab === "relationships" && (relationships.length ? <CodeBlock>{JSON.stringify(relationships, null, 2)}</CodeBlock> : <Muted>No relationships</Muted>)}
    {tab === "raw" && <CodeBlock>{JSON.stringify(details.data, null, 2)}</CodeBlock>}
  </Card>;
}

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

  const projects = useLoad<Project[]>(listProjects, String(refresh));
  const project = projects.data?.find((item) => item.key === route.project);
  useEffect(() => {
    if (!route.project && projects.data?.length) setRoute({ project: projects.data[0].key, snapshot: projects.data[0].snapshot_id ?? "" }, true);
    // The route is updated only when no project is selected.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projects.data, route.project]);
  useEffect(() => {
    if (route.project && !route.snapshot && project?.snapshot_id) setRoute({ snapshot: project.snapshot_id }, true);
    // A project head is resolved only when the URL does not name a snapshot.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [route.project, route.snapshot, project?.snapshot_id]);
  const snapshots = useLoad<Page<BrowseRow>>(route.project ? () => listRows("snapshot", { project: route.project, offset: String(route.offset) }) : null, `snapshots:${route.project}:${route.offset}:${refresh}`);
  const snapshot = route.snapshot || project?.snapshot_id || "";
  const roots = useLoad<Page<BrowseRow>>(snapshot ? () => listRows("root", { snapshot }) : null, `roots:${snapshot}:${refresh}`);
  const sources = useLoad<Page<BrowseRow>>(snapshot && route.view === "explorer" ? () => listRows("source", { snapshot, ...(route.root ? { root: route.root } : {}), ...(route.search ? { search: route.search } : {}), offset: String(route.offset) }) : null, `sources:${snapshot}:${route.root}:${route.search}:${route.offset}:${refresh}:${route.view}`);
  const nodes = useLoad<Page<BrowseRow>>(snapshot && route.view === "nodes" ? () => listRows("node", { snapshot, ...(route.root ? { root: route.root } : {}), ...(route.source ? { source: route.source } : {}), ...(route.search ? { search: route.search } : {}), offset: String(route.offset) }) : null, `nodes:${snapshot}:${route.root}:${route.source}:${route.search}:${route.offset}:${refresh}:${route.view}`);
  const query = useLoad<QueryRow[]>(snapshot && route.view === "query" && route.expression ? () => runQuery(route.project, route.expression, snapshot) : null, `query:${route.project}:${snapshot}:${route.expression}:${route.view}`);
  const [queryDraft, setQueryDraft] = useState(route.expression);
  useEffect(() => setQueryDraft(route.expression), [route.expression]);
  const selectedRoot = roots.data?.data.find((item) => item.id === route.root) ?? roots.data?.data[0];
  const nav = ["overview", "explorer", "nodes", "query"] as const;

  return <AppShell brand={<strong>UIR</strong>} contentWidth="full"
    navSections={[{ label: "Snapshot browser", items: nav.map((view) => ({ key: view, label: view[0].toUpperCase() + view.slice(1), to: routeURL({ ...route, view, offset: 0 }), active: route.view === view })) }]}
    sidebarHeader={<Field label="Project"><Select aria-label="Project" value={route.project} onChange={(event) => {
      const next = projects.data?.find((item) => item.key === event.target.value);
      setRoute({ project: event.target.value, snapshot: next?.snapshot_id ?? "", root: "", source: "", node: "", offset: 0 });
    }} options={(projects.data ?? []).map((item) => ({ value: item.key, label: item.name || item.key }))} /></Field>}
    bodyHeader={<span>{project?.name || route.project || "Projects"} {snapshot && <Muted>/ {snapshot.slice(0, 12)}</Muted>}</span>}
    bodyActions={<Button variant="outline" onClick={() => setRefresh((current) => current + 1)}>Refresh</Button>}>
    <PageLayout>
      {projects.loading && <Muted>Loading projects…</Muted>}
      <ErrorMessage error={projects.error} />
      {route.view === "overview" && <>
        <Section><Heading>Saved projects</Heading><Muted>Browse immutable UIR snapshots and their indexed code.</Muted>
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">{(projects.data ?? []).map((item) => <Card key={item.key}><strong>{item.name || item.key}</strong><div><Muted>{item.key}</Muted></div><div className="text-2xl font-bold">{item.nodes}</div><Muted>nodes · {item.sources} sources · {item.roots} roots</Muted></Card>)}</div>
          {projects.data?.length === 0 && <p>No projects are indexed yet. Use the form below to create the first snapshot.</p>}
        </Section>
        {route.project && <Section><h2>Snapshots</h2><DataList rows={snapshots.data?.data ?? []} loading={snapshots.loading} error={snapshots.error} columns={snapshotColumns}
          onClick={(row) => setRoute({ snapshot: row.id, root: "", source: "", node: "", offset: 0 })} page={snapshots.data?.page} onPage={(offset) => setRoute({ offset })} /></Section>}
        <ReindexForm project={route.project} defaultPath={selectedRoot?.local_path ?? ""} onSuccess={(nextSnapshot, nextProject) => { setRoute({ project: nextProject, snapshot: nextSnapshot, root: "", source: "", node: "", offset: 0 }); setRefresh((current) => current + 1); }} />
      </>}
      {route.view !== "overview" && !snapshot && <Card>Select or create a project snapshot on the Overview page.</Card>}
      {route.view === "explorer" && snapshot && <>
        <Heading>Source explorer</Heading>
        <Section><h2>Roots</h2><DataList rows={roots.data?.data ?? []} loading={roots.loading} error={roots.error} columns={rootColumns} onClick={(row) => setRoute({ root: row.id, source: "", offset: 0 })} /></Section>
        <Row><Field label="Root"><Select value={route.root} onChange={(event) => setRoute({ root: event.target.value, source: "", offset: 0 })} options={[{ value: "", label: "All roots" }, ...(roots.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))]} /></Field>
          <Field label="Path search"><TextInput value={route.search} onChange={(event) => setRoute({ search: event.target.value, offset: 0 })} /></Field></Row>
        <Section><h2>Sources</h2><DataList rows={sources.data?.data ?? []} loading={sources.loading} error={sources.error} columns={sourceColumns} onClick={(row) => setRoute({ source: row.id })} page={sources.data?.page} onPage={(offset) => setRoute({ offset })} /></Section>
        <SourceView source={route.source} />
      </>}
      {route.view === "nodes" && snapshot && <>
        <Heading>Nodes</Heading>
        <Row><Field label="Root"><Select value={route.root} onChange={(event) => setRoute({ root: event.target.value, node: "", offset: 0 })} options={[{ value: "", label: "All roots" }, ...(roots.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))]} /></Field>
          <Field label="Symbol search"><TextInput value={route.search} onChange={(event) => setRoute({ search: event.target.value, offset: 0 })} /></Field></Row>
        {route.source && <Button variant="outline" onClick={() => setRoute({ source: "" })}>Clear source filter</Button>}
        <DataList rows={nodes.data?.data ?? []} loading={nodes.loading} error={nodes.error} columns={nodeColumns} onClick={(row) => setRoute({ node: row.id })} page={nodes.data?.page} onPage={(offset) => setRoute({ offset })} />
        <NodeView node={route.node} onSource={(source) => setRoute({ view: "explorer", source, offset: 0 })} />
      </>}
      {route.view === "query" && snapshot && <>
        <Heading>PEG query</Heading><Muted>Run a symbol or call query against the selected snapshot.</Muted>
        <PanelForm layout="row" onSubmit={(event) => { event.preventDefault(); setRoute({ expression: queryDraft.trim() }); }}>
          <Field label="Expression"><TextInput required value={queryDraft} onChange={(event) => setQueryDraft(event.target.value)} /></Field>
          <Button type="submit">Run query</Button>
        </PanelForm>
        <h2>Results {query.data ? `(${query.data.length})` : ""}</h2>
        <ErrorMessage error={query.error} />
        <DataTable className="min-h-40 max-h-[28rem]" data={query.data ?? []} loading={query.loading} getRowId={(row) => row.id} emptyMessage={route.expression ? "No matches" : "Enter a PEG expression"}
          columns={[{ key: "operation", label: "Operation" }, { key: "symbol", label: "Symbol", grow: true }, { key: "node_type", label: "Kind" }, { key: "root", label: "Root" }, { key: "location", label: "Location", grow: true }]} />
      </>}
    </PageLayout>
  </AppShell>;
}
