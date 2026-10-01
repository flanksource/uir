import { expect, test, type Page, type Request } from "@playwright/test";
import {
  DEFAULT_EXCLUDE, explorerNodeId, graphResponse, location, MODULE_SCOPES, moduleScopesExpansion, responses, root, RUN_EXPR, SCOPE_SITE_LINE, snapshot,
} from "./call-graph-fixture";

const GRAPH_PATH = "/api/v1/modules/graph";
const RUN_EXPR_SELECTOR = "github.com/flanksource/uir/query.Pipeline.RunExpr";

async function mockApi(page: Page) {
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/api/v1/tasks/runs/stream") {
      await route.fulfill({ status: 204 });
      return;
    }
    const exclude = url.searchParams.get("exclude")?.split(",") ?? DEFAULT_EXCLUDE;
    const body = url.pathname === GRAPH_PATH
      ? (url.searchParams.get("symbol") === MODULE_SCOPES.id ? moduleScopesExpansion(exclude) : graphResponse(exclude))
      : responses[url.pathname];
    if (body === undefined) throw new Error(`Unexpected API request: ${url.pathname}`);
    await route.fulfill({ json: body, headers: { "Server-Timing": "total;dur=1" } });
  });
}

function isGraphRequest(request: Request): boolean {
  return new URL(request.url()).pathname === GRAPH_PATH;
}

function graphParams(request: Request): Record<string, string> {
  return Object.fromEntries(new URL(request.url()).searchParams);
}

test("the Explorer's Call graph tab expands, follows a call site into Source, edits exclusions and restores from the URL", async ({ page }) => {
  await mockApi(page);
  await page.goto(`/explorer?module=${encodeURIComponent(root)}&location=${encodeURIComponent(location)}&snapshot=${snapshot}&source=src-modules&node=${encodeURIComponent(explorerNodeId(RUN_EXPR))}`);

  const firstGraph = page.waitForRequest(isGraphRequest);
  await page.getByRole("tab", { name: "Call graph" }).click();
  expect(graphParams(await firstGraph)).toEqual({ selector: RUN_EXPR_SELECTOR, direction: "both", depth: "2", root, snapshot });
  const diagram = page.getByRole("group", { name: "Call graph of Pipeline.RunExpr" });
  await expect(diagram).toBeVisible();
  await expect(page.getByTestId("graph-omitted")).toContainText("8 excluded");

  const expansion = page.waitForRequest((request) => isGraphRequest(request) && graphParams(request).symbol === MODULE_SCOPES.id);
  await page.getByRole("button", { name: "Expand 2 more from Pipeline.moduleScopes" }).click();
  expect(graphParams(await expansion)).toEqual({ symbol: MODULE_SCOPES.id, direction: "callees", depth: "1", root, snapshot });
  await expect(page.getByRole("button", { name: "method Pipeline.broadModuleScopes", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: /^Expand \d+ more from Pipeline\.moduleScopes/ })).toHaveCount(0);

  await page.locator(`[data-graph-edge-label="${RUN_EXPR.id}|${MODULE_SCOPES.id}|call"]`).click();
  const sites = page.getByRole("region", { name: "Call sites" });
  await expect(sites).toContainText("options.Scope != nil");
  await sites.getByRole("button", { name: `Open query/modules.go:${SCOPE_SITE_LINE} in Source` }).click();
  await expect(page.getByRole("tab", { name: "Source" })).toHaveAttribute("aria-selected", "true");
  await expect(page).toHaveURL(new RegExp(`[?&]line=${SCOPE_SITE_LINE}(&|$)`));
  await expect(page).not.toHaveURL(/explorerTab=/);

  await page.getByRole("tab", { name: "Call graph" }).click();
  await page.getByRole("button", { name: /^Packages/ }).click();
  const facets = page.getByRole("menu", { name: "Package exclusions" });
  const showFmt = page.waitForRequest((request) => isGraphRequest(request) && graphParams(request).exclude !== undefined);
  await facets.getByRole("switch", { name: "Show fmt" }).click();
  expect(graphParams(await showFmt).exclude).toBe("builtin,gorm.io/...,strings");
  await page.keyboard.press("Escape");

  await page.getByRole("radio", { name: "Callees" }).click();
  await page.getByRole("button", { name: "Increase depth" }).click();
  await expect(page).toHaveURL(/graphDepth=3/);

  const restored = page.waitForRequest(isGraphRequest);
  await page.reload();
  expect(graphParams(await restored)).toEqual({ selector: RUN_EXPR_SELECTOR, direction: "callees", depth: "3", exclude: "builtin,gorm.io/...,strings", root, snapshot });
  await expect(page.getByRole("tab", { name: "Call graph" })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("radio", { name: "Callees" })).toHaveAttribute("aria-checked", "true");
  await expect(page.getByLabel("Graph depth")).toHaveText("3");
});
