import { afterEach, expect, it, vi } from "vitest";
import { getSourceContent, listRows, runQuery, runReindex } from "../../../web/src/api";

afterEach(() => vi.unstubAllGlobals());

it("sends the selected snapshot to the query action", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response("[]", { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await runQuery("acme/service", "type:Function", "11111111-1111-1111-1111-111111111111");
  expect(fetcher).toHaveBeenCalledWith("/api/v1/project/acme%2Fservice/query", expect.objectContaining({
    method: "POST",
    body: JSON.stringify({ expression: "type:Function", snapshot: "11111111-1111-1111-1111-111111111111" }),
  }));
});

it("reads a scoped source list and source content", async () => {
  const fetcher = vi.fn().mockImplementation(() => Promise.resolve(new Response('{"data":[],"page":{"limit":100,"offset":0,"total":0}}', { status: 200 })));
  vi.stubGlobal("fetch", fetcher);
  await listRows("source", { snapshot: "snapshot-id", root: "root-id" });
  const list = new URL(fetcher.mock.calls[0][0], "http://localhost");
  expect([list.pathname, Object.fromEntries(list.searchParams)]).toEqual(["/api/v1/source", { snapshot: "snapshot-id", root: "root-id", limit: "100" }]);
  await getSourceContent("source-id");
  expect(fetcher.mock.calls[1][0]).toBe("/api/v1/source/source-id/content");
});

it("sends explicit reindex inputs and reports server failures", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response("index failed", { status: 500, statusText: "Internal Server Error" }));
  vi.stubGlobal("fetch", fetcher);
  await expect(runReindex("acme", { path: "/repo", "include-tests": true, force: false })).rejects.toThrow("500 Internal Server Error: index failed");
  expect(fetcher.mock.calls[0][1].body).toBe(JSON.stringify({ path: "/repo", "include-tests": true, force: false }));
});
