import type { Page, Route } from "@playwright/test";

// A mocked UIR API for one module root with one checkout, shared by snapshots.spec.ts and
// history.spec.ts. Each spec overrides the responses it exercises.

export const root = "example.org/shop";
export const location = "/checkout/shop";
export const headSnapshot = "11111111-aaaa-4000-8000-000000000001";
export const sourceId = "source-cart";

export const cartSource = { id: sourceId, root_key: root, location, snapshot_id: headSnapshot, path: "cart.go", package_path: root, content_hash: "0123456789abcdef", size_bytes: 512 };

export function cartNode(snapshot: string) {
  return { id: `${sourceId}:cart-push`, source_id: sourceId, path: "cart.go", symbol: "shop.Cart.Push", node_type: "method", kind: "method", visibility: "exported",
    identifier: { package: root, type: "Cart", method: "Push", signature: "(item string)" }, child_slot: "decls", ordinal: 1, payload: {}, semantic_hash: `hash-${snapshot}`,
    line: 14, column: 6, calls: [] };
}

export const baseResponses: Record<string, unknown> = {
  "/api/v1/modules": [{ root_key: root, name: "shop", location, snapshot_id: headSnapshot, head_version: 3 }],
  "/api/v1/modules/locations": [{ id: "checkout-1", root_key: root, canonical_path: location, kind: "worktree", mount_path: location, primary: true, head_snapshot_id: headSnapshot, head_version: 3 }],
  "/api/v1/modules/snapshots": { data: [], page: { limit: 100, offset: 0, total: 0 } },
  "/api/v1/modules/heads": { items: [{ root_key: root, name: "shop", location, snapshot_id: headSnapshot, sources: [cartSource] }], warnings: [] },
  "/api/v1/modules/dependencies": { captured: true, items: [] },
  "/api/v1/modules/content": { path: "cart.go", origin: "snapshot", revision: "", snapshot_id: headSnapshot,
    content: Array.from({ length: 30 }, (_, index) => `// cart.go line ${index + 1}`).join("\n") },
};

/** The browse response of a snapshot: the one cart.go source and its Cart.Push method. */
export function browseResponse(snapshot: string) {
  return { sources: [{ ...cartSource, snapshot_id: snapshot }], nodes: [cartNode(snapshot)] };
}

export type TaskRunMeta = { id: string; name: string; kind: string; labels: Record<string, string>; status: string; total: number; completed: number; failed: number; running: number };

function runsEvent(runs: TaskRunMeta[]): string {
  return `event: runs\ndata: ${JSON.stringify(runs)}\n\n`;
}

/** Mocks /api/v1: the task-run listing stream answers runs whose labels match every label filter. */
export async function mockApi(page: Page, options: {
  responses?: Record<string, unknown>;
  runs?: TaskRunMeta[];
  handle?: (path: string, route: Route) => Promise<boolean>;
} = {}) {
  const responses = { ...baseResponses, ...options.responses };
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (options.handle && await options.handle(url.pathname, route)) return;
    if (url.pathname === "/api/v1/tasks/runs/stream") {
      const filters = url.searchParams.getAll("label").map((label) => label.split("=", 2) as [string, string]);
      const runs = (options.runs ?? []).filter((run) => filters.every(([key, value]) => run.labels[key] === value));
      await route.fulfill({ status: 200, headers: { "Content-Type": "text/event-stream" }, body: runsEvent(runs) });
      return;
    }
    if (url.pathname === "/api/v1/tasks/stream") {
      await route.fulfill({ status: 204 });
      return;
    }
    if (url.pathname === "/api/v1/modules/browse") {
      await route.fulfill({ json: browseResponse(url.searchParams.get("snapshot") ?? ""), headers: { "Server-Timing": "total;dur=1" } });
      return;
    }
    if (!(url.pathname in responses)) throw new Error(`Unexpected API request: ${url.pathname}`);
    await route.fulfill({ json: responses[url.pathname], headers: { "Server-Timing": "total;dur=1" } });
  });
}
