import { ConnectionsPage } from "@flanksource/clicky-ui/connections";
import { DatabaseModulesView } from "./DatabaseModulesView";
import { routeURL, type Route } from "./route";
import { Heading, Muted, Section } from "./ui";

export function ConnectionsView({ route, onRoute, refresh, onRefresh }: {
  route: Route;
  onRoute: (patch: Partial<Route>) => void;
  refresh: number;
  onRefresh: () => void;
}) {
  return <Section>
    <Heading>Connections</Heading>
    <Muted>Save and test connections. Database modules use a saved SQL connection for manual schema captures.</Muted>
    <ConnectionsPage apiBase="/api/v1" selected={route.connection || undefined}
      onSelect={(id) => onRoute({ connection: id ?? "" })}
      connectionHref={(id) => routeURL({ ...route, view: "connections", connection: id })} />
    <DatabaseModulesView route={route} onRoute={onRoute} refresh={refresh} onRefresh={onRefresh} />
  </Section>;
}
