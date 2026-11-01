import { afterEach, expect, it, vi } from "vitest";
import { captureDatabaseModule, compareDatabaseSnapshots, createDatabaseModule, listConnectionDatabases, listSQLConnections } from "../../../web/src/api";

afterEach(() => vi.unstubAllGlobals());

it("lists SQL connections from the commons-db connection service and the databases of one", async () => {
  const saved = { id: "connection 1", name: "Example", type: "postgres" };
  const databases = ["orders", "billing"];
  const fetcher = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify([saved]), { status: 200 }))
    .mockResolvedValueOnce(new Response(JSON.stringify(databases), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(listSQLConnections()).resolves.toEqual([saved]);
  await expect(listConnectionDatabases(saved.id)).resolves.toEqual(databases);
  expect(fetcher.mock.calls).toEqual([
    ["/api/v1/connection?types=postgres,mysql,sql_server,sqlite", expect.objectContaining({ headers: { Accept: "application/json" } })],
    ["/api/v1/database-modules/connections/connection%201/databases", expect.objectContaining({ method: "POST" })],
  ]);
});

it("registers a database module, captures it again, and compares selected snapshots", async () => {
  const module = { id: "module-1", connection_id: "connection-1", database_name: "orders", name: "Orders", root_key: "database/module-1" };
  const result = { module, capture: { snapshot_id: "snapshot-1" } };
  const diff = { summary: { added: 1 }, changes: [{ kind: "added", path: "orders.amount" }] };
  const fetcher = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(result), { status: 201 }))
    .mockResolvedValueOnce(new Response(JSON.stringify({ snapshot_id: "snapshot-2" }), { status: 200 }))
    .mockResolvedValueOnce(new Response(JSON.stringify(diff), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(createDatabaseModule({ connection_id: module.connection_id, database_name: module.database_name, name: module.name })).resolves.toEqual(result);
  await expect(captureDatabaseModule(module.id)).resolves.toEqual({ snapshot_id: "snapshot-2" });
  await expect(compareDatabaseSnapshots({ from: "snapshot-1", to: "snapshot-2", from_schema: "public", to_schema: "dbo" })).resolves.toEqual(diff);
  expect(fetcher.mock.calls[2]?.[1]).toEqual(expect.objectContaining({ method: "POST", body: JSON.stringify({ from: "snapshot-1", to: "snapshot-2", from_schema: "public", to_schema: "dbo" }) }));
});
