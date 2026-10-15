import { Button } from "@flanksource/clicky-ui/components";
import { listConnectionSchemas, listConnections, listDatabaseModules, type ConnectionView, type DatabaseModule } from "./api";
import { ConnectionEditor } from "./ConnectionEditor";
import { DatabaseModulesView } from "./DatabaseModulesView";
import type { Route } from "./route";
import { Card, ErrorMessage, Heading, Muted, Row, Section } from "./ui";
import { useLoad } from "./use-load";

export function ConnectionsView({ route, onRoute, refresh, onRefresh }: {
  route: Route;
  onRoute: (patch: Partial<Route>) => void;
  refresh: number;
  onRefresh: () => void;
}) {
  const schemas = useLoad(listConnectionSchemas, "connection-schemas");
  const connections = useLoad<ConnectionView[]>(listConnections, `connections:${refresh}`);
  const modules = useLoad<DatabaseModule[]>(listDatabaseModules, `database-modules:${refresh}`);
  const selected = connections.data?.find((item) => item.id === route.connection);
  return <Section>
    <Heading>Connections</Heading>
    <Muted>Save and test Git, hosted source control, SQL, and S3 connections. Database modules use a saved SQL connection for manual schema captures.</Muted>
    <ErrorMessage error={schemas.error ?? connections.error ?? modules.error} />
    <Row><Button type="button" variant="outline" onClick={() => onRoute({ connection: "" })}>New connection</Button></Row>
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">{(connections.data ?? []).map((item) => <Card key={item.id}>
      <strong>{item.name}</strong><Muted>{item.type.replaceAll("_", " ")}{item.namespace ? ` · ${item.namespace}` : ""}</Muted>
      {item.literal_credentials && <span className="text-sm text-amber-700 dark:text-amber-300">Literal credentials stored</span>}
      <Button type="button" variant="outline" onClick={() => onRoute({ connection: item.id })}>Edit and test</Button>
    </Card>)}</div>
    {connections.data?.length === 0 && <Muted>No connections saved yet.</Muted>}
    {schemas.data && <ConnectionEditor key={selected?.id ?? "new"} schemas={schemas.data}
      connection={selected} onSaved={(saved) => { onRoute({ connection: saved.id }); onRefresh(); }} onDeleted={() => { onRoute({ connection: "" }); onRefresh(); }} />}
    <DatabaseModulesView connections={connections.data ?? []} modules={modules.data ?? []} route={route} onRoute={onRoute} refresh={refresh} onRefresh={onRefresh} />
  </Section>;
}
