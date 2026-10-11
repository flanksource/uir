import { describe, expect, it } from "vitest";
import type { QueryCatalog } from "./api";
import { isSQL, sqlCompletions } from "./sql-completion";

const catalog: QueryCatalog = {
  functions: [{ name: "traverse", arguments: 1, options: ["via", "direction", "depth"], terminal: false }],
  kinds: [{ name: "http.route", category: "member" }],
  fields: { "symbol.name": "string", "symbol.props.method": "string", path: "string" },
  domains: { symbols: {}, relationships: { "http.handled_by": { name: "http.handled_by", type: "reference" } } },
};
const complete = (text: string) => sqlCompletions(text, text.length, catalog);

describe("SQL completion", () => {
  it("detects the SELECT token and explicit language without consuming compact names", () => {
    expect(["SELECT *", " select", "SELECT1", '"SELECT"', "func:SELECT"].map((s) => isSQL(s))).toEqual([true, true, false, false, false]);
    expect(isSQL("func:*", "sql")).toBe(true);
    expect(isSQL("SELECT *", "compact")).toBe(false);
  });
  it("offers registered kind selectors and functions inside a nested source", () => {
    expect(complete("SELECT * FROM traverse(").choices.map((c) => c.label)).toEqual(expect.arrayContaining(["http.route:", "traverse"]));
  });
  it("offers declared properties through a source alias", () => {
    const text = "SELECT * FROM http.route:* AS r WHERE r.me";
    const result = complete(text);
    expect(result.choices).toContainEqual(expect.objectContaining({ label: "r.method", insert: "r.method" }));
    expect(text.slice(result.start, result.end)).toBe("r.me");
  });
  it("offers selector kinds after IN in a predicate", () => {
    expect(complete("SELECT * FROM func:* WHERE symbol IN http.r").choices).toContainEqual(expect.objectContaining({ label: "http.route:" }));
  });
  it("completes projection aliases declared later in the source clause", () => {
    const text = "SELECT r.me FROM http.route:* AS r";
    expect(sqlCompletions(text, text.indexOf(" FROM"), catalog).choices).toContainEqual(expect.objectContaining({ label: "r.method" }));
  });
  it("offers registered relationship names inside the via string", () => {
    const text = "SELECT * FROM traverse(http.route:*, via => 'http.h";
    const result = complete(text);
    expect(result.choices).toContainEqual(expect.objectContaining({ label: "http.handled_by", insert: "http.handled_by" }));
    expect(text.slice(result.start, result.end)).toBe("http.h");
  });
  it("requests an indexed selector without treating an option value as a selector", () => {
    expect(complete("SELECT * FROM http.route:Ord").selector).toBe("http.route:Ord");
    expect(complete("SELECT * FROM traverse(http.route:*, depth => 2").selector).toBeUndefined();
  });
});
