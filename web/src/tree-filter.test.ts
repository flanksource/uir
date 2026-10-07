import { describe, expect, it } from "vitest";

import { filterItems } from "./tree-filter";

type Item = { label: string; state: string; children: Item[] };
const item = (label: string, state: string, children: Item[] = []): Item => ({ label, state, children });
const HTTP = item("http", "shown");
const URL = item("url", "hidden");
const NET = item("net", "mixed", [HTTP, URL]);
const FMT = item("fmt", "hidden");
const ROOTS = [item("std", "mixed", [FMT, NET])];

describe("filterItems", () => {
  it("keeps every item when the query is blank", () => {
    expect(filterItems(ROOTS, "  ", (entry) => entry.label)).toBe(ROOTS);
  });

  it("keeps a matching item with its whole subtree", () => {
    expect(filterItems(ROOTS, "NET", (entry) => entry.label)).toEqual([item("std", "mixed", [NET])]);
  });

  it("keeps a copy of each ancestor with only the matching children, carrying the ancestor's own fields", () => {
    expect(filterItems(ROOTS, "url", (entry) => entry.label)).toEqual([item("std", "mixed", [item("net", "mixed", [URL])])]);
  });
});
