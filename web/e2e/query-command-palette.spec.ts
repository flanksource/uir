import { expect, test } from "@playwright/test";

const root = "example.org/service";
const location = "/checkout/service";
const snapshot = "11111111-1111-1111-1111-111111111111";
const expression = "func:Save <";
const invalidExpression = "func:Save &";

test("Cmd+K and Query editors show runner failures in Monaco and clear them on edit", async ({ page }) => {
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/v1/tasks/runs/stream") {
      await route.fulfill({ status: 204 });
      return;
    }
    if (path === "/api/v1/modules/query" && route.request().postDataJSON().args[0] === invalidExpression) {
      await route.fulfill({
        status: 400,
        json: {
          code: "invalid_query",
          message: "parse error near IntersectionOperator",
          hint: "Enter a Go symbol or typed selector after an operator.",
          context: { line: 1, column: 11 },
          trace: "trace-1",
        },
      });
      return;
    }
    const responses: Record<string, unknown> = {
      "/api/v1/modules": [
        {
          root_key: root,
          name: "service",
          location,
          snapshot_id: snapshot,
          head_version: 1,
        },
      ],
      "/api/v1/modules/locations": [
        {
          id: "checkout-1",
          root_key: root,
          canonical_path: location,
          kind: "worktree",
          primary: true,
          head_snapshot_id: snapshot,
          head_version: 1,
        },
      ],
      "/api/v1/modules/snapshots": {
        data: [],
        page: { limit: 100, offset: 0, total: 0 },
      },
      "/api/v1/modules/browse": {
        sources: [],
        nodes: [],
      },
      "/api/v1/modules/suggest": [],
      "/api/v1/modules/suggest-selectors": [],
      "/api/v1/modules/query": {
        operation: "incoming",
        total: 1,
        declarations: [],
        symbols: [],
        coverage: [],
        stages: [],
        matches: [
          {
            kind: "reference",
            symbol: "function:example.org/service:Save#()",
            root,
            location,
            snapshot_id: snapshot,
            source: "service.go:12:3",
            path: "service.go",
            line: 12,
          },
        ],
      },
    };
    if (!(path in responses)) throw new Error(`Unexpected API request: ${path}`);
    await route.fulfill({
      json: responses[path],
      headers: { "Server-Timing": "total;dur=1" },
    });
  });

  await page.goto(
    `/?module=${encodeURIComponent(root)}&location=${encodeURIComponent(location)}&snapshot=${snapshot}`,
  );
  await expect(page.getByRole("button", { name: /Search modules, files, symbols/ })).toBeVisible();
  await page.keyboard.press("ControlOrMeta+k");
  const palette = page.getByRole("dialog", { name: "Command palette" });
  await expect(palette).toBeVisible();
  await palette.getByRole("combobox", { name: "Command palette" }).fill(">");

  await expect(palette.locator('[data-slot="monaco-editor"]')).toBeVisible();
  const editor = palette.getByRole("textbox", { name: "Expression" });
  await expect(editor).toBeFocused();
  await page.keyboard.insertText(invalidExpression);
  await page.keyboard.press("ControlOrMeta+Enter");
  await expect(palette.locator(".squiggly-error")).toBeVisible();
  await expect(palette).toContainText("parse error near IntersectionOperator");
  await expect(palette).toContainText("Hint: Enter a Go symbol");

  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.insertText(expression);
  await expect(palette.locator(".squiggly-error")).toHaveCount(0);
  const submitted = page.waitForRequest(
    (request) => request.method() === "POST" && new URL(request.url()).pathname === "/api/v1/modules/query",
  );
  await page.keyboard.press("ControlOrMeta+Enter");

  expect((await submitted).postDataJSON()).toEqual({ args: [expression], root, snapshot });
  await expect(palette.getByRole("option", { name: /service\.Save/ })).toBeVisible();
  await expect(palette).toContainText("1 matches");

  await palette.getByRole("option", { name: /Open full results on Query page/ }).click();
  const queryEditor = page.locator('[data-slot="monaco-editor"]');
  await expect(queryEditor).toBeVisible();
  await queryEditor.click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.insertText(invalidExpression);
  await page.getByRole("button", { name: "Run query" }).click();
  await expect(page.locator(".squiggly-error")).toBeVisible();
  await expect(page.getByRole("main")).toContainText("parse error near IntersectionOperator");
  await expect(page.getByRole("main")).toContainText("Hint: Enter a Go symbol");

  await queryEditor.click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.insertText(expression);
  await expect(page.locator(".squiggly-error")).toHaveCount(0);
});
