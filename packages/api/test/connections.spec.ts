import { afterEach, expect, it, vi } from "vitest";
import { captureDatabaseModule, compareDatabaseSnapshots, createConnection, createDatabaseModule, listConnectionSchemas, listConnections } from "../../../web/src/api";

afterEach(() => vi.unstubAllGlobals());

it("loads nine shared connection schemas and sends literal credentials without client-side rejection", async () => {
  const schemas = { git: { type: "object", properties: { url: { type: "string" } } } };
  const saved = { id: "connection-1", name: "Example", type: "git", has_password: true, literal_credentials: true, warnings: ["Literal credentials are stored in the UIR database."] };
  const fetcher = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(schemas), { status: 200 }))
    .mockResolvedValueOnce(new Response(JSON.stringify(saved), { status: 201 }))
    .mockResolvedValueOnce(new Response(JSON.stringify([saved]), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(listConnectionSchemas()).resolves.toEqual(schemas);
  await expect(createConnection({ name: "Example", type: "git", url: "https://example.org/repo.git", password: "literal-example" })).resolves.toEqual(saved);
  await expect(listConnections()).resolves.toEqual([saved]);
  expect(fetcher.mock.calls).toEqual([
    ["/api/v1/connections/schema", expect.objectContaining({ headers: { Accept: "application/json" } })],
    ["/api/v1/connections", expect.objectContaining({ method: "POST", body: JSON.stringify({ name: "Example", type: "git", url: "https://example.org/repo.git", password: "literal-example" }) })],
    ["/api/v1/connections", expect.objectContaining({ headers: { Accept: "application/json" } })],
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
