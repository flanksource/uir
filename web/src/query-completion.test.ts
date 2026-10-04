import { describe, expect, it } from "vitest";
import { queryCompletionContext, querySyntaxCompletions, quoteQuerySymbol, unquoteQueryValue } from "./query-completion";

describe("query completion", () => {
  it.each([
    ["", "symbol", ""],
    ["store.St", "symbol", "store.St"],
    ["clicky.Exec*", "symbol", "clicky.Exec*"],
    ["clicky.Exe?", "symbol", "clicky.Exe?"],
    ["**/catalog.Item*", "symbol", "**/catalog.Item*"],
    ["store.Store.Save ", "relation", ""],
    ["store.Store.Save :i", "relation", ":i"],
    ["store.Store.Save < ", "filter", ""],
    ["store.Store.Save < -", "filter", "-"],
    ["store.Store.Save < +pkg ", "value", ""],
    ["store.Store.Save < -pkg example.org/sh", "value", "example.org/sh"],
    ["main.* >> ", "symbol", ""],
    ["main.* >> store.St", "symbol", "store.St"],
    ["main.* & ", "symbol", ""],
    ["main.* | ", "symbol", ""],
    ["(main.* | ", "symbol", ""],
    ["(main.*) ", "relation", ""],
    ["main.* < -pkg app ", "filter", ""],
    ["pkg:example.org/shop:app", "selector", "pkg:example.org/shop:app"],
    ["pkg:example.org/!team$#@/store", "selector", "pkg:example.org/!team$#@/store"],
    ["func:Save < pkg:example.org/shop:app", "selector", "pkg:example.org/shop:app"],
    ["func:Run +pkg:example.org/shop:app", "selector", "+pkg:example.org/shop:app"],
    ["type:Base :inherits ", "filter", ""],
    ["method:Save +path:example.org/shop/store/**", "selector", "+path:example.org/shop/store/**"],
    ["kind:oipa.rule:Rate*", "selector", "kind:oipa.rule:Rate*"],
    ["kind:foreign_key ", "relation", ""],
    ["func:Run -kind:oipa.rule", "selector", "-kind:oipa.rule"],
    ['mod:"Acme Regional/Gr', "selector", 'mod:"Acme Regional/Gr'],
    ['func:Run < mod:"Acme Regional/Group Life" ', "relation", ""],
    ['kind:oipa.rule:"CopyBook-Cyc', "selector", 'kind:oipa.rule:"CopyBook-Cyc'],
    ['"Add Ri', "symbol", '"Add Ri'],
    ['"Add Rider" ', "relation", ""],
    ['main.* >> "Premium Calc (Ann', "symbol", '"Premium Calc (Ann'],
    ["Plan:Pl", "selector", "Plan:Pl"],
    ['Plan:"Field With', "selector", 'Plan:"Field With'],
    ['"type":Co', "selector", '"type":Co'],
    ["pkgx:Fi", "selector", "pkgx:Fi"],
    ["Plan:PlanField1 ", "relation", ""],
    ["Plan:PlanField1 ~w ", "filter", ""],
    ["func:Save < Plan:Pl", "selector", "Plan:Pl"],
    ["Thing :i", "relation", ":i"],
  ] as const)("classifies %s at the caret", (draft, mode, prefix) => {
    expect(queryCompletionContext(draft, draft.length)).toMatchObject({ mode, prefix });
  });

  it.each([
    ['"Add Ri', "Add Ri"],
    ['"Add Rider"', "Add Rider"],
    ['"Say \\"Hi\\" \\\\ now"', 'Say "Hi" \\ now'],
    ["store.Save", "store.Save"],
  ])("reads the literal text of %s", (value, literal) => {
    expect(unquoteQueryValue(value)).toBe(literal);
  });

  it.each([
    ["example.org/shop/store.Store.Save", "example.org/shop/store.Store.Save"],
    ["Acme Regional/Group Life/GL.Add Rider", '"Acme Regional/Group Life/GL.Add Rider"'],
    ["Co/Prod.CopyBook-CycleA", "Co/Prod.CopyBook-CycleA"],
    ['Co/Prod.Say "Hi" *now*', '"Co/Prod.Say \\"Hi\\" \\*now\\*"'],
  ])("spells %s as a symbol that matches exactly it", (name, spelled) => {
    expect(quoteQuerySymbol(name)).toBe(spelled);
  });

  it("offers every valid next operator after a symbol", () => {
    expect(querySyntaxCompletions(queryCompletionContext("store.Store.Save ", 17)).map((option) => option.label))
      .toEqual(expect.arrayContaining(["+pkg:", "-pkg:", "+func:", "-struct:", "-path:", ":inherits", "<", ">", "=", "&", "|", ">>"]));
  });

  it("offers filters and composition after an incoming relation", () => {
    const options = querySyntaxCompletions(queryCompletionContext("store.Store.Save < ", 19));
    expect(options).toContainEqual({
      label: "-pkg", insert: "-pkg ", help: "Exclude a package; append /... for its subpackages",
    });
    expect(options.map((option) => option.label)).toEqual(expect.arrayContaining(["<", ">", "-f", "+pkg", "-pkg", "&", "|", ">>"]));
    expect(queryCompletionContext("store.Store.Save < -pkg ", 24)).toMatchObject({ mode: "value" });
  });

  it("replaces a whole token when the cursor is inside it", () => {
    expect(queryCompletionContext("store.Store.Save < -pkg example.org/shop", 28)).toMatchObject({
      mode: "value", start: 24, end: 40, prefix: "exam",
    });
  });

  it("suggests package scope values for the selected module", () => {
    expect(querySyntaxCompletions(queryCompletionContext("store.Do < -pkg ", 16), "example.org/shop").map((option) => option.insert))
      .toEqual(["example.org/shop ", "example.org/shop/... "]);
    expect(querySyntaxCompletions(queryCompletionContext("store.Do < -f ", 14)).map((option) => option.insert))
      .toEqual(["_test.go "]);
  });

  it("offers typed selectors as binary relation operands", () => {
    const options = querySyntaxCompletions(queryCompletionContext("func:Save < ", 12));
    expect(options.map((option) => option.label)).toEqual(expect.arrayContaining(["pkg:", "mod:", "func:", "field:", "struct:", "kind:"]));
  });
});
