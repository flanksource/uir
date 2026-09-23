import { afterEach, expect, it, vi } from "vitest";
import { addModules, browseModule, listModuleLocations, listModuleSnapshots, readModuleSource, reindexModules, runModuleQuery } from "../../../web/src/api";

afterEach(() => vi.unstubAllGlobals());

it("sends the selected module snapshot to the PEG query action", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response("[]", { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await runModuleQuery('nodes where method = "Run"', "example.org/service", "11111111-1111-1111-1111-111111111111");
  expect(fetcher).toHaveBeenCalledWith("/api/v1/modules/query", expect.objectContaining({
    method: "POST",
    body: JSON.stringify({ args: ['nodes where method = "Run"'], root: "example.org/service", snapshot: "11111111-1111-1111-1111-111111111111" }),
  }));
});

it("scopes checkout history, source projections, and content to a module snapshot", async () => {
  const fetcher = vi.fn().mockImplementation(async () => new Response("{}", { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  await listModuleLocations("example.org/service");
  await listModuleSnapshots("example.org/service", "/checkout/service", 100);
  await browseModule("snapshot-id");
  await readModuleSource("snapshot-id", "pkg/main.go");
  expect(fetcher.mock.calls.map(([path]) => {
    const url = new URL(path, "http://localhost");
    return [url.pathname, Object.fromEntries(url.searchParams)];
  })).toEqual([
    ["/api/v1/modules/locations", { root: "example.org/service" }],
    ["/api/v1/modules/snapshots", { root: "example.org/service", location: "/checkout/service", offset: "100", limit: "100" }],
    ["/api/v1/modules/browse", { snapshot: "snapshot-id" }],
    ["/api/v1/modules/content", { snapshot: "snapshot-id", path: "pkg/main.go" }],
  ]);
});

it("sends explicit add and reindex inputs and reports server failures", async () => {
  const fetcher = vi.fn().mockResolvedValueOnce(new Response("[]", { status: 200 }))
    .mockResolvedValueOnce(new Response("index failed", { status: 500, statusText: "Internal Server Error" }));
  vi.stubGlobal("fetch", fetcher);
  await addModules("/repo", true);
  expect(fetcher.mock.calls[0]).toEqual(["/api/v1/modules/add", expect.objectContaining({
    method: "POST", body: JSON.stringify({ args: ["/repo"], "include-tests": true, "no-workspace-uses": true }),
  })]);
  await expect(reindexModules("/repo", true, false)).rejects.toThrow("500 Internal Server Error: index failed");
  expect(fetcher.mock.calls[1]).toEqual(["/api/v1/modules/reindex", expect.objectContaining({
    method: "POST", body: JSON.stringify({ args: ["/repo"], "include-tests": true, force: false }),
  })]);
});
