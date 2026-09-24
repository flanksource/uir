import { describe, expect, it } from "vitest";
import type { ModuleNode, ModuleQueryRow } from "./api";
import { groupRowsByFile, identityLabel, nodeIdentityKey, paletteExpression, parseIdentityKey, queryExamples, queryScope, symbolSelector } from "./query-model";

const pkg = "example.org/service/store";
const root = "example.org/service";

function key(nodeType: string, type = "", method = "", field = "", signature = "", packagePath = pkg): string {
  return `v1:${JSON.stringify([nodeType, "", packagePath, type, method, field, signature])}`;
}

function node(nodeType: string, identity: string, sourceId = "source-1"): ModuleNode {
  const parsed = parseIdentityKey(identity);
  return { id: `${sourceId}:${identity}`, source_id: sourceId, path: "store/store.go", symbol: parsed.method || parsed.type, node_type: nodeType,
    identifier: { package: parsed.package, type: parsed.type, method: parsed.method, field: parsed.field, signature: parsed.signature },
    child_slot: "", ordinal: 0, payload: {}, semantic_hash: "hash", calls: [] };
}

function row(kind: string, path: string | undefined, line: number): ModuleQueryRow {
  return { kind, symbol: "Store.Save", root, location: "/work/service", source: `${path}:${line}`, snapshot_id: "snapshot-1", path, line, column: 4, role: "reference" };
}

describe("identity keys", () => {
  it("reads the seven identity components of a source-scoped node id", () => {
    const identity = key("method", "Store", "Save", "", "(ctx context.Context) error");
    expect(parseIdentityKey(nodeIdentityKey(node("method", identity)))).toEqual({
      nodeType: "method", module: "", package: pkg, type: "Store", method: "Save", field: "", signature: "(ctx context.Context) error",
    });
  });

  it.each([
    ["an unversioned key", '["method","","p","T","M","",""]', /not a v1 identity key/],
    ["a short component list", 'v1:["method","p"]', /seven string components/],
    ["a non-string component", 'v1:["method","","p","T","M","",7]', /seven string components/],
  ])("rejects %s", (_, identity, error) => {
    expect(() => parseIdentityKey(identity)).toThrow(error);
  });

  it("rejects a node id that is not scoped to its source", () => {
    expect(() => nodeIdentityKey({ id: "other:v1:[]", source_id: "source-1" })).toThrow(/not scoped to source source-1/);
  });

  it.each([
    [key("method", "Store", "Save"), "Store.Save"],
    [key("record_field", "Store", "", "db"), "Store.db"],
    [key("package", "", "", "", "", pkg), pkg],
  ])("labels %s as %s", (identity, label) => {
    expect(identityLabel(identity)).toBe(label);
  });
});

describe("symbolSelector", () => {
  it.each([
    ["a method", key("method", "Store", "Save"), `${pkg}.Store.Save`],
    ["a function extracted as a method without an owner", key("method", "", "Open"), `${pkg}.Open`],
    ["a function node", key("function", "", "Open"), `${pkg}.Open`],
    ["a method on a nested owner", key("method", "Store.cache", "Get"), `${pkg}.Store.cache.Get`],
    ["a struct field", key("record_field", "Store", "", "db"), `${pkg}.Store.db`],
    ["a field of a literal struct field", key("record_field", "Store", "", "options.Timeout"), `${pkg}.Store.options.Timeout`],
    ["a type", key("class", "Store"), `${pkg}.Store`],
    ["a nested type", key("class", "Store.cache"), `${pkg}.Store.cache`],
    ["an interface", key("interface", "Saver"), `${pkg}.Saver`],
    ["a package variable", key("package_variable", "", "", "DefaultTimeout"), `${pkg}.DefaultTimeout`],
    ["a package", key("package", "", "", "", "", pkg), pkg],
  ])("selects %s", (_, identity, expected) => {
    expect(symbolSelector(parseIdentityKey(identity))).toBe(expected);
  });

  it.each([
    ["a member with invalid punctuation", key("package_variable", "", "", 'say"hi'), /cannot be spelled/],
    ["an identity without a package", key("method", "Store", "Save", "", "", ""), /has no package/],
  ])("rejects %s", (_, identity, error) => {
    expect(() => symbolSelector(parseIdentityKey(identity))).toThrow(error);
  });
});

describe("queryExamples", () => {
  const helper = node("method", key("method", "", "open"));
  const unexported = node("method", key("method", "Store", "flush"));
  const save = node("method", key("method", "Store", "Save"));
  const saver = node("interface", key("interface", "Saver"));
  const method = `${pkg}.Store.Save`;

  it("builds compact examples from an exported method and an interface in the snapshot", () => {
    expect(queryExamples(root, [helper, unexported, save, saver]).map((example) => [example.id, example.expression])).toEqual([
      ["resolve", method],
      ["incoming", `${method} <`],
      ["definitions", `${method} =`],
      ["outgoing", `${method} >`],
      ["transitive", `${method} <<3`],
      ["methods", `${pkg}.Store :methods`],
      ["implementations", `${pkg}.Saver :impl`],
    ]);
  });

  it("omits implementers when the snapshot declares no interface", () => {
    expect(queryExamples(root, [save]).find((example) => example.id === "implementations")).toBeUndefined();
  });

  it("falls back to an unexported method only when no exported method exists", () => {
    expect(queryExamples(root, [unexported]).find((example) => example.id === "incoming")?.expression)
      .toBe(`${pkg}.Store.flush <`);
  });

  it("omits examples when no owner-qualified method can be selected", () => {
    expect(queryExamples(root, [helper, saver])).toEqual([]);
  });

  it("has no fabricated examples when the scope has no indexed methods", () => {
    expect(queryExamples("", [])).toEqual([]);
  });

  it("uses the request scope rather than embedding a root predicate", () => {
    const expressions = Object.fromEntries(queryExamples("", [save, saver]).map((example) => [example.id, example.expression]));
    expect(expressions).toMatchObject({
      incoming: `${method} <`,
      implementations: `${pkg}.Saver :impl`,
    });
  });
});

describe("queryScope", () => {
  it.each([
    ["every module for an empty module", { module: "", snapshot: "" }, { root: "", snapshot: "" }],
    ["nothing while a chosen module has no snapshot", { module: root, snapshot: "" }, null],
    ["the chosen module snapshot", { module: root, snapshot: "snapshot-1" }, { root, snapshot: "snapshot-1" }],
  ])("queries %s", (_, route, expected) => {
    expect(queryScope(route)).toEqual(expected);
  });
});

describe("paletteExpression", () => {
  it.each([
    [">sub.Thing.Do <", "sub.Thing.Do <"],
    [">  main.* >> sub.Thing.Do ", "main.* >> sub.Thing.Do"],
    ["sub.Thing.Do <", "sub.Thing.Do <"],
    ["example.org/service/store.Store.Save =", "example.org/service/store.Store.Save ="],
    ["main.* >> sub.Thing.Do", "main.* >> sub.Thing.Do"],
  ])("runs %s as %s", (input, expression) => {
    expect(paletteExpression(input)).toBe(expression);
  });

  it.each([[">"], ["> "], ["Store"], ["references"], ["search"], ["researcher of"]])("treats %s as a palette search", (input) => {
    expect(paletteExpression(input)).toBeUndefined();
  });
});

describe("groupRowsByFile", () => {
  it("groups occurrences by file in first-seen order and keeps candidates apart", () => {
    const a1 = row("reference", "a.go", 3);
    const b1 = row("reference", "b.go", 9);
    const a2 = row("reference", "a.go", 12);
    const candidate = row("candidate", undefined, 0);
    expect(groupRowsByFile([a1, candidate, b1, a2])).toEqual({
      candidates: [candidate],
      files: [{ path: "a.go", rows: [a1, a2] }, { path: "b.go", rows: [b1] }],
    });
  });

  it("rejects an occurrence without a path", () => {
    expect(() => groupRowsByFile([row("reference", undefined, 3)])).toThrow(/reference row Store.Save has no path/);
  });
});
