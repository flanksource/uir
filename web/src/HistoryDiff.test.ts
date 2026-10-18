import { describe, expect, it } from "vitest";
import type { SymbolChange, SymbolDiff } from "./api";
import { countChanges } from "./HistoryDiff";

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
  it("counts a moved symbol once although both of its files report it", () => {
    const diff = diffOf([
      { path: "book.go", status: "modified", rows: [{ ...moved, moved_away: true }, body] },
      { path: "count.go", status: "added", rows: [moved] },
    ]);
    expect(countChanges(diff)).toBe(2);
  });
});
