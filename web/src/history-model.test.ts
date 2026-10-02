import { describe, expect, it } from "vitest";
import type { GitHistory, ModuleNode, SymbolChange, SymbolDiff } from "./api";
import { changeTarget, findChangedNode, resolveRevision } from "./history-model";

const fromSnapshot = "from-snapshot";
const toSnapshot = "to-snapshot";
const diff: Pick<SymbolDiff, "from" | "to"> = {
  from: { commit: "a".repeat(40), snapshot_id: fromSnapshot, worktree_state: "clean" },
  to: { commit: "b".repeat(40), snapshot_id: toSnapshot, worktree_state: "clean" },
};

function change(patch: Partial<SymbolChange>): SymbolChange {
  return { class: "body", kind: "method", name: "Push", owner: "List", visibility: "exported", path_before: "list.go", path_after: "list.go", ...patch };
}

function node(id: string, path: string, identifier: ModuleNode["identifier"], kind = "method"): ModuleNode {
  return { id, source_id: `source:${path}`, path, symbol: id, node_type: kind, kind, visibility: "exported", identifier,
    child_slot: "decls", ordinal: 0, payload: {}, semantic_hash: "hash", line: 10, column: 6, calls: [] };
}

describe("changeTarget", () => {
  it.each([
    ["the to snapshot at the new path for a moved symbol", change({ class: "moved", path_before: "old.go", path_after: "new.go" }), { snapshot: toSnapshot, path: "new.go" }],
    ["the from snapshot at the old path for a removed symbol", change({ class: "removed", path_after: undefined }), { snapshot: fromSnapshot, path: "list.go" }],
  ])("opens %s", (_, row, expected) => {
    expect(changeTarget(diff, row)).toEqual(expected);
  });

  it("rejects a change with no path on the side it opens", () => {
    expect(() => changeTarget(diff, change({ class: "added", path_after: undefined }))).toThrow(/List.Push/);
  });
});

describe("findChangedNode", () => {
  const nodes = [
    node("list-push", "list.go", { type: "List", method: "Push" }),
    node("stack-push", "list.go", { type: "Stack", method: "Push" }),
    node("other-file-push", "other.go", { type: "List", method: "Push" }),
    node("list-type", "list.go", { type: "List" }, "type"),
    node("list-meta", "list.go", { type: "List", field: "Meta" }, "field"),
    node("scale-func", "list.go", { method: "Scale" }, "func"),
  ];

  it.each([
    ["a method by owner and name in its file", change({}), "list-push"],
    ["a type by its name with no owner", change({ kind: "type", owner: undefined, name: "List" }), "list-type"],
    ["a field by its owning type", change({ kind: "field", name: "Meta" }), "list-meta"],
    ["a package function with no owner", change({ kind: "func", owner: undefined, name: "Scale" }), "scale-func"],
  ])("finds %s", (_, row, expected) => {
    expect(findChangedNode(nodes, row, "list.go").id).toBe(expected);
  });

  it("names the symbol and file when the snapshot has no matching declaration", () => {
    expect(() => findChangedNode(nodes, change({ name: "Pop" }), "list.go")).toThrow(/List\.Pop.*list\.go/);
  });
});

describe("resolveRevision", () => {
  const history: Pick<GitHistory, "branches" | "commits" | "pull_requests"> = {
    branches: [{ name: "main", commit: "c".repeat(40) }],
    pull_requests: [{ number: 12, commit: "d".repeat(40) }],
    commits: [{ commit: "e".repeat(40), parents: [], subject: "init", authored_at: "2026-09-30T10:00:00Z" }],
  };

  it.each([
    ["a branch name", "main", "c".repeat(40)],
    ["a pull request", "pr:12", "d".repeat(40)],
    ["a listed commit prefix", "eeeeeee", "e".repeat(40)],
    ["a full commit id that is not listed", "f".repeat(40), "f".repeat(40)],
    ["an unknown ref", "release", undefined],
  ])("resolves %s", (_, revision, expected) => {
    expect(resolveRevision(history, revision)).toBe(expected);
  });
});
