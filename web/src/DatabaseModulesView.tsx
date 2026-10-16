import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@flanksource/clicky-ui/components";
import { CONNECTION_DASHBOARD_QUERY_KEY } from "@flanksource/clicky-ui/connections";
import { captureDatabaseModule, compareDatabaseSnapshots, createDatabaseModule, deleteDatabaseModule, errorMessage, listConnectionDatabases, listDatabaseCaptures, listDatabaseModules, listDatabaseSnapshotSchemas, listSQLConnections, type DatabaseDiff, type DatabaseModule } from "./api";
import type { Route } from "./route";
import { Card, ErrorMessage, Field, Muted, PanelForm, Row, Section, TextInput } from "./ui";
import { useLoad } from "./use-load";

const selectClass = "h-9 min-w-48 rounded-md border border-input bg-background px-3 text-sm";

function CaptureChoice({ snapshots, value, onChange, label }: {
  snapshots: { id: string; started_at: string; ordinal: number }[];
  value: string;
  onChange: (value: string) => void;
  label: string;
}) {
  return <Field label={label}><select className={selectClass} value={value} onChange={(event) => onChange(event.target.value)}>
    {snapshots.length === 0 && <option value="">No captures yet</option>}
    {snapshots.map((snapshot) => <option key={snapshot.id} value={snapshot.id}>#{snapshot.ordinal} · {new Date(snapshot.started_at).toLocaleString()}</option>)}
  </select></Field>;
}

export function DatabaseModulesView({ route, onRoute, onRefresh, refresh }: {
  route: Route;
  onRoute: (patch: Partial<Route>) => void;
  onRefresh: () => void;
  refresh: number;
}) {
  // Keyed under the dashboard's query key so ConnectionsPage's invalidation after a save or delete refetches it.
  const connections = useQuery({ queryKey: [CONNECTION_DASHBOARD_QUERY_KEY, "sql-connections", refresh], queryFn: listSQLConnections });
  const sqlConnections = connections.data ?? [];
  const loadedModules = useLoad<DatabaseModule[]>(listDatabaseModules, `database-modules:${refresh}`);
  const modules = loadedModules.data ?? [];
  const [connectionID, setConnectionID] = useState("");
  const selectedConnection = sqlConnections.find((item) => item.id === connectionID) ?? sqlConnections[0];
  const databases = useLoad(selectedConnection ? () => listConnectionDatabases(selectedConnection.id) : null, `databases:${selectedConnection?.id}:${refresh}`);
  const [databaseName, setDatabaseName] = useState("");
  const selectedDatabase = databases.data?.includes(databaseName) ? databaseName : databases.data?.[0] ?? "";
  const [name, setName] = useState("");
  const [pending, setPending] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [diff, setDiff] = useState<DatabaseDiff>();
  const fromModule = modules.find((item) => item.id === route.fromModule) ?? modules[0];
  const toModule = modules.find((item) => item.id === route.toModule) ?? fromModule;
  const fromCaptures = useLoad(fromModule ? () => listDatabaseCaptures(fromModule.id) : null, `from:${fromModule?.id}:${refresh}`);
  const toCaptures = useLoad(toModule ? () => listDatabaseCaptures(toModule.id) : null, `to:${toModule?.id}:${refresh}`);
  const fromCapture = fromCaptures.data?.find((item) => item.id === route.fromCapture)?.id ?? fromCaptures.data?.[1]?.id ?? fromCaptures.data?.[0]?.id ?? "";
  const toCapture = toCaptures.data?.find((item) => item.id === route.toCapture)?.id ?? toCaptures.data?.[0]?.id ?? "";
  const crossModule = Boolean(fromModule && toModule && fromModule.id !== toModule.id);
  const fromSchemas = useLoad(crossModule && fromCapture ? () => listDatabaseSnapshotSchemas(fromCapture) : null, `from-schema:${fromCapture}:${crossModule}`);
  const toSchemas = useLoad(crossModule && toCapture ? () => listDatabaseSnapshotSchemas(toCapture) : null, `to-schema:${toCapture}:${crossModule}`);

  async function add(event: React.FormEvent) {
    event.preventDefault();
    if (!selectedConnection || !selectedDatabase) return;
    setPending("add"); setError(""); setMessage("");
    try {
      const result = await createDatabaseModule({ connection_id: selectedConnection.id, database_name: selectedDatabase, name: name.trim() || selectedDatabase });
      onRoute({ fromModule: result.module.id, toModule: result.module.id, fromCapture: "", toCapture: "" });
      setMessage(result.capture_error ?? `Captured ${selectedDatabase}. Use Capture schema to take another snapshot.`);
      onRefresh();
    } catch (reason) { setError(errorMessage(reason)); }
    finally { setPending(""); }
  }

  async function capture(module: DatabaseModule) {
    setPending(module.id); setError(""); setMessage("");
    try { await captureDatabaseModule(module.id); setMessage(`Captured ${module.name}.`); onRefresh(); }
    catch (reason) { setError(errorMessage(reason)); }
    finally { setPending(""); }
  }

  async function remove(module: DatabaseModule) {
    if (!window.confirm(`Remove ${module.name} and all its schema captures?`)) return;
    setPending(module.id); setError("");
    try { await deleteDatabaseModule(module.id); onRoute({ fromModule: "", toModule: "", fromCapture: "", toCapture: "" }); onRefresh(); }
    catch (reason) { setError(errorMessage(reason)); }
    finally { setPending(""); }
  }

  async function compare(event: React.FormEvent) {
    event.preventDefault();
    if (!fromCapture || !toCapture) return;
    setPending("diff"); setError(""); setDiff(undefined);
    try {
      const result = await compareDatabaseSnapshots({ from: fromCapture, to: toCapture,
        ...(crossModule ? { from_schema: route.fromSchema, to_schema: route.toSchema } : {}) });
      setDiff(result);
    } catch (reason) { setError(errorMessage(reason)); }
    finally { setPending(""); }
  }

  return <Section>
    <h2>Database modules</h2>
    <Muted>A connection can serve several databases. Adding a database module takes its first schema capture; later captures are manual. Captures store schema metadata only.</Muted>
    <PanelForm onSubmit={add}>
      <h3>Add a database</h3>
      <Row><Field label="SQL connection"><select className={selectClass} value={selectedConnection?.id ?? ""} onChange={(event) => { setConnectionID(event.target.value); setDatabaseName(""); }}>
        {connections.isPending && <option value="">Loading connections…</option>}
        {connections.isSuccess && sqlConnections.length === 0 && <option value="">Save a SQL connection first</option>}
        {sqlConnections.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.type}</option>)}
      </select></Field>
      <Field label="Database"><select className={selectClass} value={selectedDatabase} onChange={(event) => setDatabaseName(event.target.value)}>
        {databases.loading && <option value="">Loading databases…</option>}
        {(databases.data ?? []).map((item) => <option key={item} value={item}>{item}</option>)}
      </select></Field>
      <Field label="Module name"><TextInput value={name} onChange={(event) => setName(event.target.value)} placeholder={selectedDatabase || "Example"} /></Field></Row>
      <ErrorMessage error={connections.error ? errorMessage(connections.error) : databases.error} />
      <Button type="submit" disabled={!selectedDatabase || Boolean(pending)} loading={pending === "add"}>Add and capture schema</Button>
    </PanelForm>
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2">{modules.map((module) => <Card key={module.id}>
      <strong>{module.name}</strong><Muted>{module.database_name} · {sqlConnections.find((item) => item.id === module.connection_id)?.name ?? "Missing connection"}</Muted>
      <Row><Button type="button" variant="outline" disabled={Boolean(pending)} onClick={() => capture(module)}>Capture schema</Button>
        <Button type="button" variant="outline" disabled={Boolean(pending)} onClick={() => remove(module)}>Remove module</Button></Row>
    </Card>)}</div>
    <ErrorMessage error={loadedModules.error} />
    {loadedModules.data?.length === 0 && <Muted>No database modules are registered yet.</Muted>}
    {modules.length > 0 && <PanelForm onSubmit={compare}>
      <h3>Compare schema captures</h3>
      <Muted>Same-module comparison includes every schema. Across modules, select a schema on each side; objects align by exact name and SQL type.</Muted>
      <Row><Field label="From module"><select className={selectClass} value={fromModule?.id ?? ""} onChange={(event) => onRoute({ fromModule: event.target.value, fromCapture: "", fromSchema: "" })}>
        {modules.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
      </select></Field>
      <Field label="To module"><select className={selectClass} value={toModule?.id ?? ""} onChange={(event) => onRoute({ toModule: event.target.value, toCapture: "", toSchema: "" })}>
        {modules.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
      </select></Field></Row>
      <Row><CaptureChoice label="From capture" snapshots={fromCaptures.data ?? []} value={fromCapture} onChange={(next) => onRoute({ fromCapture: next, fromSchema: "" })} />
        <CaptureChoice label="To capture" snapshots={toCaptures.data ?? []} value={toCapture} onChange={(next) => onRoute({ toCapture: next, toSchema: "" })} /></Row>
      {crossModule && <Row><Field label="From schema"><select className={selectClass} value={route.fromSchema} onChange={(event) => onRoute({ fromSchema: event.target.value })}>
        <option value="">Select schema</option>{(fromSchemas.data ?? []).map((item) => <option key={item} value={item}>{item}</option>)}
      </select></Field><Field label="To schema"><select className={selectClass} value={route.toSchema} onChange={(event) => onRoute({ toSchema: event.target.value })}>
        <option value="">Select schema</option>{(toSchemas.data ?? []).map((item) => <option key={item} value={item}>{item}</option>)}
      </select></Field></Row>}
      <ErrorMessage error={fromCaptures.error ?? toCaptures.error ?? fromSchemas.error ?? toSchemas.error} />
      <Button type="submit" loading={pending === "diff"} disabled={!fromCapture || !toCapture || (crossModule && (!route.fromSchema || !route.toSchema))}>Compare</Button>
      {diff && <Section><strong>{diff.changes?.length ?? 0} changes</strong>
        {(diff.changes ?? []).map((change, index) => <Card key={`${change.kind}:${change.path}:${index}`}><strong>{change.kind} · {change.path}</strong>
          {change.detail && <Muted>{change.detail}</Muted>}{change.changes && <pre className="overflow-auto text-xs">{JSON.stringify(change.changes, null, 2)}</pre>}</Card>)}
        {(diff.changes ?? []).length === 0 && <Muted>No schema changes.</Muted>}
      </Section>}
    </PanelForm>}
    <ErrorMessage error={error} />{message && <Muted>{message}</Muted>}
  </Section>;
}
