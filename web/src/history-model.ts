import type { GitHistory, ModuleNode, SymbolChange, SymbolDiff } from "./api";

function displayName(row: Pick<SymbolChange, "owner" | "name">): string {
  return row.owner ? `${row.owner}.${row.name}` : row.name;
}

/** A change opens on the snapshot that still has the symbol: the to side, or the from side for a removal. */
export function changeTarget(diff: Pick<SymbolDiff, "from" | "to">, row: SymbolChange): { snapshot: string; path: string } {
  const removed = row.class === "removed";
  const path = removed ? row.path_before : row.path_after;
  if (!path) throw new Error(`Change ${row.class} ${displayName(row)} has no ${removed ? "previous" : "current"} file path`);
  return { snapshot: removed ? diff.from.snapshot_id : diff.to.snapshot_id, path };
}

// ownerAndName mirrors symboldiff.ownerAndName, which names a change row from the same identifier.
function ownerAndName(identifier: ModuleNode["identifier"]): [string, string] {
  if (identifier.field) return [identifier.type ?? "", identifier.field];
  if (identifier.method) return [identifier.type ?? "", identifier.method];
  return ["", identifier.type ?? ""];
}

export function findChangedNode(nodes: ModuleNode[], row: SymbolChange, path: string): ModuleNode {
  const matches = nodes.filter((node) => {
    const [owner, name] = ownerAndName(node.identifier);
    return node.path === path && owner === (row.owner ?? "") && name === row.name;
  });
  const match = matches.find((node) => node.kind === row.kind) ?? matches[0];
  if (!match) throw new Error(`Symbol ${displayName(row)} is not declared in ${path} of this snapshot`);
  return match;
}

const FULL_COMMIT = /^[0-9a-f]{40}$/;

/** The commit a compare revision names, when the listed history or the revision itself says. */
export function resolveRevision(history: Pick<GitHistory, "branches" | "commits" | "pull_requests">, revision: string): string | undefined {
  const branch = history.branches.find((item) => item.name === revision);
  if (branch) return branch.commit;
  const pr = history.pull_requests.find((item) => `pr:${item.number}` === revision);
  if (pr) return pr.commit;
  if (FULL_COMMIT.test(revision)) return revision;
  return revision.length >= 7 ? history.commits.find((item) => item.commit.startsWith(revision))?.commit : undefined;
}
