import { expect, test, type Page, type Route } from "@playwright/test";
import { headSnapshot, location, mockApi, root, type TaskRunMeta } from "./module-fixture";

const initialCommit = "c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0";
const pendingCommit = "c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1";
const indexedCommit = "c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2";
const fromSnapshot = "44444444-dddd-4000-8000-000000000004";
const toSnapshot = "55555555-eeee-4000-8000-000000000005";

const history = {
  root_key: root, location, branches: [{ name: "main", commit: indexedCommit }], pull_requests: [],
  commits: [
    { commit: indexedCommit, parents: [pendingCommit], subject: "Push items onto the cart", authored_at: "2026-09-30T12:00:00Z",
      snapshot_id: toSnapshot, snapshot_reason: "historical", snapshot_completed_at: "2026-09-30T12:05:00Z" },
    { commit: pendingCommit, parents: [initialCommit], subject: "Add the cart", authored_at: "2026-09-29T12:00:00Z" },
  ],
};

const diff = {
  root_key: root, visibility: "all", stat: true,
  from: { commit: pendingCommit, snapshot_id: fromSnapshot, worktree_state: "clean" },
  to: { commit: indexedCommit, snapshot_id: toSnapshot, worktree_state: "clean" },
  packages: [{ path: root, files: [{ path: "cart.go", status: "modified", rows: [
    { class: "body", kind: "method", owner: "Cart", name: "Push", visibility: "exported", path_before: "cart.go", path_after: "cart.go", lines: { added: 3, removed: 1 } },
  ] }] }],
};

const autoIndexRun: TaskRunMeta = { id: "run-historical-0001", name: `Index ${root}@${initialCommit.slice(0, 12)}`, kind: "module-index",
  labels: { root, commit: initialCommit, reason: "historical" }, status: "running", total: 1, completed: 0, failed: 0, running: 1 };

function historyURL(search = "") {
  return `/history?module=${encodeURIComponent(root)}&location=${encodeURIComponent(location)}&snapshot=${headSnapshot}${search}`;
}

function isBrowseOf(snapshot: string) {
  return (request: { url(): string }) => {
    const url = new URL(request.url());
    return url.pathname === "/api/v1/modules/browse" && url.searchParams.get("snapshot") === snapshot;
  };
}

async function mockHistory(page: Page, handleDiff: (route: Route) => Promise<void>) {
  await mockApi(page, {
    responses: { "/api/v1/modules/history": history },
    runs: [autoIndexRun],
    handle: async (path, route) => {
      if (path !== "/api/v1/modules/diff") return false;
      await handleDiff(route);
      return true;
    },
  });
}

test("a commit row shows its indexed snapshot and opens it in the Explorer", async ({ page }) => {
  await mockHistory(page, (route) => route.fulfill({ json: diff }));
  await page.goto(historyURL());
  const indexed = page.getByTestId("commit-row").filter({ hasText: "Push items onto the cart" });
  await expect(indexed.getByRole("button", { name: `Open indexed snapshot of ${indexedCommit.slice(0, 7)} in Explorer` })).toContainText(/indexed .* · historical/);
  await expect(page.getByTestId("commit-row").filter({ hasText: "Add the cart" }).getByRole("button", { name: /Open indexed snapshot/ })).toHaveCount(0);

  const browsed = page.waitForRequest(isBrowseOf(toSnapshot));
  await indexed.getByRole("button", { name: /Open indexed snapshot/ }).click();
  await browsed;
  await expect(page).toHaveURL((url) => url.pathname === "/explorer" && url.searchParams.get("snapshot") === toSnapshot && url.searchParams.get("location") === location);
});

test("while a diff auto-indexes a commit it links to the module-index run, and the commit is badged once indexed", async ({ page }) => {
  const pendingSnapshot = "66666666-ffff-4000-8000-000000000006";
  let indexed = false;
  let releaseDiff!: () => void;
  const diffHeld = new Promise<void>((resolve) => { releaseDiff = resolve; });
  await mockApi(page, {
    runs: [autoIndexRun],
    handle: async (path, route) => {
      if (path === "/api/v1/modules/history") {
        await route.fulfill({ json: indexed ? { ...history, commits: history.commits.map((commit) => commit.commit !== pendingCommit ? commit
          : { ...commit, snapshot_id: pendingSnapshot, snapshot_reason: "historical", snapshot_completed_at: "2026-10-01T09:00:00Z" }) } : history });
        return true;
      }
      if (path !== "/api/v1/modules/diff") return false;
      await diffHeld;
      await route.fulfill({ json: { ...diff, from: { ...diff.from, commit: initialCommit }, to: { ...diff.to, commit: pendingCommit, snapshot_id: pendingSnapshot } } });
      return true;
    },
  });
  await page.goto(historyURL());
  const pending = page.getByTestId("commit-row").filter({ hasText: "Add the cart" });
  const requested = page.waitForRequest((request) => new URL(request.url()).pathname === "/api/v1/modules/diff");
  await pending.getByRole("button", { name: /Add the cart/ }).click();
  expect((await requested).postDataJSON()).toMatchObject({ args: [`${initialCommit}..${pendingCommit}`], root, location, "auto-index": true });

  const run = page.getByTestId("diff-pending").getByTestId("commit-index-run");
  await expect(run).toHaveCount(1);
  await expect(run).toContainText(`Indexing ${initialCommit.slice(0, 7)} · running · 0/1 tasks`);
  await run.getByRole("link").click();
  await expect(page).toHaveURL((url) => url.pathname === "/tasks" && url.searchParams.get("taskRun") === autoIndexRun.id);

  await page.goBack();
  await expect(run).toHaveCount(1);
  await expect(pending.getByRole("button", { name: /Open indexed snapshot/ })).toHaveCount(0);
  indexed = true;
  releaseDiff();
  await expect(page.getByTestId("diff-side-to")).toContainText(pendingSnapshot.slice(0, 12));
  await expect(pending.getByRole("button", { name: `Open indexed snapshot of ${pendingCommit.slice(0, 7)} in Explorer` })).toContainText(/indexed .* · historical/);
});

test("a diff opens both snapshots and a changed symbol in the Explorer", async ({ page }) => {
  await mockHistory(page, (route) => route.fulfill({ json: diff }));
  await page.goto(historyURL(`&compareFrom=${pendingCommit}&compareTo=${indexedCommit}&logCommit=${indexedCommit}`));
  await expect(page.getByTestId("diff-side-from")).toContainText(fromSnapshot.slice(0, 12));
  await expect(page.getByTestId("diff-side-to")).toContainText(toSnapshot.slice(0, 12));

  await page.getByRole("button", { name: "Open to snapshot in Explorer" }).click();
  await expect(page).toHaveURL((url) => url.pathname === "/explorer" && url.searchParams.get("snapshot") === toSnapshot);
  await page.goBack();

  const browsed = page.waitForRequest(isBrowseOf(toSnapshot));
  await page.getByRole("button", { name: "Open Cart.Push in Explorer" }).click();
  await browsed;
  await expect(page).toHaveURL((url) => url.pathname === "/explorer" && url.searchParams.get("snapshot") === toSnapshot
    && url.searchParams.get("node") === "source-cart:cart-push" && url.searchParams.get("line") === "14" && url.searchParams.get("location") === location);
});
