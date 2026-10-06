import { expect, test, type Locator, type Page } from "@playwright/test";
import { headSnapshot, location, mockApi, root } from "./module-fixture";

const historicalSnapshot = "22222222-bbbb-4000-8000-000000000002";
const versionedSnapshot = "33333333-cccc-4000-8000-000000000003";
const reindexRun = "run-reindex-0001";

const snapshot = { root_key: root, canonical_path: location, worktree_state: "clean", coverage: "indexed", occurrence_count: 0, started_at: "2026-09-30T10:00:00Z" };
const snapshots = [
  { ...snapshot, id: headSnapshot, base_snapshot_id: historicalSnapshot, revision: "a1b2c3d4e5f6a7b8c9d0", git_commit: "a1b2c3d4e5f6a7b8c9d0", kind: "head", head: true, head_version: 3,
    reason: "reindex", index_started_at: "2026-09-30T10:00:00Z", completed_at: "2026-09-30T10:01:30Z", task_run_id: reindexRun,
    file_count: 12, symbol_count: 340, source_bytes: 1536, files_added: 2, files_changed: 3, files_deleted: 1, symbols_changed: 14 },
  { ...snapshot, id: historicalSnapshot, revision: "f0e1d2c3b4a5f6e7d8c9", git_commit: "f0e1d2c3b4a5f6e7d8c9", kind: "historical", head: false,
    reason: "historical", index_started_at: "2026-09-29T09:00:00Z", completed_at: "2026-09-29T09:00:00.850Z",
    file_count: 10, symbol_count: 320, source_bytes: 1200, files_added: 10, files_changed: 0, files_deleted: 0, symbols_changed: 320 },
  { ...snapshot, id: versionedSnapshot, revision: "v1.2.0", module_version: "v1.2.0", kind: "versioned", head: false, coverage: "partial",
    reason: "versioned-dependency", completed_at: "2026-09-28T08:00:00Z",
    file_count: 1, symbol_count: 1, source_bytes: 900, files_added: 1, files_changed: 0, files_deleted: 0, symbols_changed: 1 },
];

async function openOverview(page: Page) {
  await mockApi(page, { responses: { "/api/v1/modules/snapshots": { data: snapshots, page: { limit: 100, offset: 0, total: snapshots.length } } } });
  await page.goto(`/?module=${encodeURIComponent(root)}&location=${encodeURIComponent(location)}&snapshot=${headSnapshot}`);
  const section = page.locator("section").filter({ has: page.getByRole("heading", { name: `Snapshots for ${location}` }) }).last();
  await expect(section.getByRole("row")).toHaveCount(snapshots.length + 1);
  return section;
}

function rowFor(section: Locator, id: string) {
  return section.getByRole("row").filter({ has: section.page().locator("code", { hasText: id.slice(0, 12) }) });
}

async function chooseFilter(page: Page, section: Locator, column: string, value: string) {
  await section.getByRole("combobox", { name: column }).click();
  await page.getByRole("option", { name: value, exact: true }).click();
  await page.keyboard.press("Escape");
}

test("the Overview snapshot table shows when, why and how each snapshot was indexed", async ({ page }) => {
  const section = await openOverview(page);
  const headers = await section.getByRole("columnheader").allInnerTexts();
  expect(headers.map((header) => header.split("\n")[0].trim()).filter(Boolean)).toEqual(["Snapshot", "Date", "Reason", "Kind", "Duration", "Size", "Delta", "Revision", "Coverage", "Run"]);

  const head = rowFor(section, headSnapshot);
  await expect(head).toContainText("reindex");
  await expect(head).toContainText("head · current v3");
  await expect(head).toContainText("1.5 min");
  await expect(head).toContainText("12 files · 340 symbols · 1.5 KB");
  await expect(head).toContainText(`+2 ~3 −1 files · 14 symbols vs ${historicalSnapshot.slice(0, 12)}`);
  await expect(head).toContainText("a1b2c3d4e5f6");

  const historical = rowFor(section, historicalSnapshot);
  await expect(historical).toContainText("850 ms");
  await expect(historical).toContainText("(no base)");

  const versioned = rowFor(section, versionedSnapshot);
  await expect(versioned).toContainText("v1.2.0");
  await expect(versioned).toContainText("partial");
  const durationColumn = headers.findIndex((header) => header.startsWith("Duration"));
  await expect(versioned.getByRole("cell").nth(durationColumn)).toHaveText("—");
});

test("the snapshot table filters the loaded page by reason and by kind", async ({ page }) => {
  const section = await openOverview(page);
  await chooseFilter(page, section, "Reason", "historical");
  await expect(section.getByRole("row")).toHaveCount(2);
  await expect(rowFor(section, historicalSnapshot)).toBeVisible();

  await page.reload();
  await expect(section.getByRole("row")).toHaveCount(snapshots.length + 1);
  await chooseFilter(page, section, "Kind", "versioned");
  await expect(section.getByRole("row")).toHaveCount(2);
  await expect(rowFor(section, versionedSnapshot)).toBeVisible();
});

test("a snapshot opens in the Explorer and its index run opens in Tasks", async ({ page }) => {
  const section = await openOverview(page);
  const browsed = page.waitForRequest((request) => new URL(request.url()).pathname === "/api/v1/modules/browse"
    && new URL(request.url()).searchParams.get("snapshot") === historicalSnapshot);
  await rowFor(section, historicalSnapshot).getByRole("button", { name: "Open in Explorer" }).click();
  await browsed;
  await expect(page).toHaveURL((url) => url.pathname === "/explorer" && url.searchParams.get("snapshot") === historicalSnapshot
    && url.searchParams.get("location") === location);

  await page.goBack();
  await rowFor(section, headSnapshot).getByRole("link", { name: reindexRun.slice(0, 12) }).click();
  await expect(page).toHaveURL((url) => url.pathname === "/tasks" && url.searchParams.get("taskRun") === reindexRun);
  await expect(page.getByRole("heading", { name: "Indexing tasks" })).toBeVisible();
});
