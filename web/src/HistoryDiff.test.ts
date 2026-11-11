import { describe, expect, it } from "vitest";
import type { SymbolChange, SymbolDiff } from "./api";
import { countChanges } from "./HistoryDiff";
import { DiffResult } from "./HistoryDiff";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

const moved: SymbolChange = { class: "moved", kind: "method", name: "Count", visibility: "exported", path_before: "book.go", path_after: "count.go" };
const body: SymbolChange = { class: "body", kind: "method", name: "Total", visibility: "exported", path_before: "book.go", path_after: "book.go" };

function diffOf(files: SymbolDiff["packages"][number]["files"]): SymbolDiff {
  return {
    root_key: "example.org/ledger", visibility: "all", stat: false,
    from: { commit: "a".repeat(40), snapshot_id: "from", worktree_state: "clean" },
    to: { commit: "b".repeat(40), snapshot_id: "to", worktree_state: "clean" },
    packages: [{ path: "example.org/ledger/book", files }],
  } as SymbolDiff;
}

describe("countChanges", () => {
  it("renders renamed symbols in change grouping with their old names", () => {
    const renamed: SymbolChange = { ...moved, class: "renamed", name_before: "Before", name: "After" };
    const diff = diffOf([{ path: "count.go", status: "modified", rows: [renamed] }]);
    const html = renderToStaticMarkup(createElement(DiffResult, { diff, group: "change", location: "/workspace", onRoute: () => undefined }));
    expect(html).toContain("Before → After");
    expect(html).toContain("renamed");
  });
  it("counts a moved symbol once although both of its files report it", () => {
    const diff = diffOf([
      { path: "book.go", status: "modified", rows: [{ ...moved, moved_away: true }, body] },
      { path: "count.go", status: "added", rows: [moved] },
    ]);
    expect(countChanges(diff)).toBe(2);
  });
});
