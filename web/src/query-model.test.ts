import { describe, expect, it } from "vitest";
import type { ModuleNode, ModuleQueryRow } from "./api";
import { groupRowsByFile, identityLabel, nodeIdentityKey, paletteExpression, parseIdentityKey, queryExamples, symbolSelector } from "./query-model";

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
    ["a method", key("method", "Store", "Save"), `package = "${pkg}" and type = "Store" and method = "Save"`],
    ["a function extracted as a method without an owner", key("method", "", "Open"), `package = "${pkg}" and method = "Open" and kind = "func"`],
    ["a function node", key("function", "", "Open"), `package = "${pkg}" and method = "Open" and kind = "func"`],
    ["a method on a nested owner", key("method", "Store.cache", "Get"), `package = "${pkg}" and owner = "cache" and method = "Get"`],
    ["a struct field", key("record_field", "Store", "", "db"), `package = "${pkg}" and type = "Store" and field = "db"`],
    ["a field of a literal struct field", key("record_field", "Store", "", "options.Timeout"), `package = "${pkg}" and owner = "options" and field = "Timeout"`],
    ["a type", key("class", "Store"), `package = "${pkg}" and type = "Store"`],
    ["a nested type", key("class", "Store.cache"), `package = "${pkg}" and name = "cache" and kind = "type"`],
    ["an interface", key("interface", "Saver"), `package = "${pkg}" and type = "Saver"`],
    ["a package variable", key("package_variable", "", "", "DefaultTimeout"), `package = "${pkg}" and name = "DefaultTimeout"`],
    ["a value with quotes", key("package_variable", "", "", 'say"hi'), `package = "${pkg}" and name = "say\\"hi"`],
  ])("selects %s", (_, identity, expected) => {
    expect(symbolSelector(parseIdentityKey(identity))).toBe(expected);
  });

  it.each([
    ["a package", key("package", "", "", "", "", pkg), /package identity names no symbol/],
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
  const method = `package = "${pkg}" and type = "Store" and method = "Save"`;

  it("builds one example per operation from an exported method and an interface in the snapshot", () => {
    expect(queryExamples(root, [helper, unexported, save, saver]).map((example) => [example.id, example.expression])).toEqual([
      ["nodes", `nodes where root = "${root}" and node_type = "method"`],
      ["unresolved", `unresolved calls where root = "${root}"`],
      ["references", `references of node where ${method}`],
      ["definitions", `definitions of node where ${method}`],
      ["implementations", `implementations of node where package = "${pkg}" and type = "Saver"`],
      ["callers", `callers of node where ${method}`],
      ["dispatch", `callers of node where ${method} including dispatch`],
      ["callees", `callees of node where ${method}`],
      ["search", `search "Sav" where root = "${root}"`],
      ["member-search", `search "Store.Sa" where root = "${root}"`],
    ]);
  });

  it("uses the method's owner type for implementations when the snapshot declares no interface", () => {
    expect(queryExamples(root, [save]).find((example) => example.id === "implementations")?.expression)
      .toBe(`implementations of node where package = "${pkg}" and type = "Store"`);
  });

  it("falls back to an unexported method only when no exported method exists", () => {
    expect(queryExamples(root, [unexported]).find((example) => example.id === "references")?.expression)
      .toBe(`references of node where package = "${pkg}" and type = "Store" and method = "flush"`);
  });

  it("keeps only the root-scoped examples when the snapshot has no method", () => {
    expect(queryExamples(root, [helper, saver]).map((example) => example.id)).toEqual(["nodes", "unresolved"]);
  });
});

describe("paletteExpression", () => {
  it.each([
    [">nodes where method = \"Run\"", "nodes where method = \"Run\""],
    [">  unresolved calls ", "unresolved calls"],
    ["references of node where type = \"Store\"", "references of node where type = \"Store\""],
    ["callers of node where method = \"Run\" including dispatch", "callers of node where method = \"Run\" including dispatch"],
    ["search \"Store.Sa\"", "search \"Store.Sa\""],
    ["  search Sav", "search Sav"],
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
