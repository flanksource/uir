import type { PackageSymbolItem } from "./explorer-model";

export type SymbolFilterState = "neutral" | "include" | "exclude";

function parseFilter(value: string): Map<string, Exclude<SymbolFilterState, "neutral">> {
  const states = new Map<string, "include" | "exclude">();
  if (!value) return states;
  for (const token of value.split(",")) {
    const match = /^([+-])([a-z][a-z0-9_]*)$/.exec(token);
    if (!match || states.has(match[2])) throw new Error(`Invalid symbol filter token ${token}`);
    states.set(match[2], match[1] === "+" ? "include" : "exclude");
  }
  return states;
}

export function symbolFilterState(value: string, kind: string): SymbolFilterState {
  return parseFilter(value).get(kind) ?? "neutral";
}

export function symbolFilterKeys(value: string): string[] {
  return [...parseFilter(value).keys()];
}

export function cycleSymbolFilter(value: string, kind: string): string {
  if (!/^[a-z][a-z0-9_]*$/.test(kind)) throw new Error(`Invalid symbol filter kind ${kind}`);
  const states = parseFilter(value);
  const next = states.get(kind) === "include" ? "exclude" : states.get(kind) === "exclude" ? "neutral" : "include";
  if (next === "neutral") states.delete(kind);
  else states.set(kind, next);
  return [...states].sort(([left], [right]) => left.localeCompare(right))
    .map(([key, state]) => `${state === "include" ? "+" : "-"}${key}`).join(",");
}

function matches(value: string, states: Map<string, "include" | "exclude">): boolean {
  if (!value) throw new Error("Indexed symbol is missing its filter metadata");
  if (states.get(value) === "exclude") return false;
  return ![...states.values()].includes("include") || states.get(value) === "include";
}

export function filterPackageSymbols(items: PackageSymbolItem[], visibility: string, kinds: string): PackageSymbolItem[] {
  if (!visibility && !kinds) return items;
  const visibilityStates = parseFilter(visibility);
  for (const key of visibilityStates.keys()) {
    if (key !== "exported" && key !== "internal") throw new Error(`Unknown symbol visibility ${key}`);
  }
  const kindStates = parseFilter(kinds);
  function visible(item: PackageSymbolItem): PackageSymbolItem[] {
    const children = item.children.flatMap(visible);
    if (item.kind === "package") return children.length ? [{ ...item, children }] : [];
    if (!matches(item.node.visibility, visibilityStates) || !matches(item.node.kind, kindStates)) return children;
    return [{ ...item, children }];
  }
  return items.flatMap(visible);
}
