// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ConnectionsView } from "./ConnectionsView";
import { readRoute, type Route } from "./route";

const SQL_CONNECTIONS_URL = "/api/v1/connection?types=postgres,mysql,sql_server,sqlite";

const warehouse = { id: "warehouse-id", name: "warehouse", namespace: "analytics", type: "postgres" };
const cache = { id: "cache-id", name: "cache", namespace: "", type: "redis", url: "redis://cache:6379/0" };

const connectionSchema = {
  type: "object",
  required: ["name", "type"],
  properties: {
    name: { title: "Name", type: "string" },
    type: { title: "Type", type: "string", enum: ["postgres", "redis"] },
    url: { title: "URL", type: "string" },
  },
};

type Handler = (init: RequestInit | undefined) => Response;

function json(body: unknown, contentType = "application/json"): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": contentType } });
}

const api: Record<string, Handler> = {
  "GET /api/v1/connections/dashboard": () => json({
    connections: [warehouse, cache].map((item) => ({ ...item, secretCount: 0, inlineCredential: false, insecureTLS: false, profileCount: 0 })),
    generatedAt: "2026-10-16T08:00:00Z",
  }),
  "GET /api/v1/connection": (init) => {
    const accept = new Headers(init?.headers).get("Accept");
    if (accept !== "application/schema+json") throw new Error(`connection schema requested with Accept ${accept}`);
    return json(connectionSchema, "application/schema+json");
  },
  "GET /api/v1/connection/cache-id": () => json(cache),
  [`GET ${SQL_CONNECTIONS_URL}`]: () => json([warehouse]),
  "GET /api/v1/database-modules": () => json([]),
  "POST /api/v1/database-modules/connections/warehouse-id/databases": () => json(["orders"]),
};

let requests: string[] = [];

beforeEach(() => {
  requests = [];
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const key = `${(init?.method ?? "GET").toUpperCase()} ${String(input)}`;
    requests.push(key);
    const handler = api[key];
    if (!handler) throw new Error(`unexpected request ${key}`);
    return handler(init);
  }));
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderView(search: string) {
  const route: Route = readRoute({ pathname: "/connections", search });
  const onRoute = vi.fn();
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={queryClient}>
    <ConnectionsView route={route} onRoute={onRoute} refresh={0} onRefresh={() => undefined} />
  </QueryClientProvider>);
  return { onRoute };
}

describe("ConnectionsView", () => {
  it("opens the edit dialog for the connection the route names", async () => {
    renderView("?connection=cache-id");

    const dialog = await screen.findByRole("dialog", { name: "Edit cache" });
    expect(await within(dialog).findByRole("button", { name: "Delete" })).toBeTruthy();
    expect(requests).toContain("GET /api/v1/connection/cache-id");
  });

  it("clears the route's connection when the edit dialog closes", async () => {
    const { onRoute } = renderView("?connection=cache-id&fromModule=orders-module");

    const dialog = await screen.findByRole("dialog", { name: "Edit cache" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));

    expect(onRoute).toHaveBeenCalledExactlyOnceWith({ connection: "" });
  });

  it("routes a plain row click to its connection and links the row to that route", async () => {
    const { onRoute } = renderView("?fromModule=orders-module");

    const link = (await screen.findByText("warehouse", { selector: "a *, a" })).closest("a");
    expect(link?.getAttribute("href")).toBe("/connections?connection=warehouse-id&fromModule=orders-module");
    fireEvent.click(link!);

    expect(onRoute).toHaveBeenCalledExactlyOnceWith({ connection: "warehouse-id" });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("offers only SQL connections to database modules and lists their databases", async () => {
    renderView("");

    const connectionSelect = await screen.findByRole("combobox", { name: "SQL connection" });
    expect(await within(connectionSelect).findByRole("option", { name: "warehouse · postgres" })).toBeTruthy();
    const databaseSelect = screen.getByRole("combobox", { name: "Database" });
    expect(await within(databaseSelect).findByRole("option", { name: "orders" })).toBeTruthy();
    expect(requests).toEqual(expect.arrayContaining([
      `GET ${SQL_CONNECTIONS_URL}`,
      "POST /api/v1/database-modules/connections/warehouse-id/databases",
    ]));
  });
});
