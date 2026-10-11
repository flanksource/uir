import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { QueryView } from "./QueryView";
import { readRoute } from "./route";

describe("SQL result projections", () => {
  it("renders dotted column names as literal keys and preserves optional NULL values", () => {
    const route = readRoute({ pathname: "/query", search: "?language=sql" });
    const markup = renderToStaticMarkup(createElement(QueryView, { route, examples: [], onRoute: () => {}, query: { loading: false, data: {
      operation: "sql",
      total: 1, matches: [], symbols: [], declarations: [], coverage: [], warnings: [], stages: [],
      columns: [{ name: "r.method", field: "symbol.props.method", type: "string" }, { name: "r.path", field: "symbol.props.path", type: "string" }, { name: "r.description", field: "symbol.props.description", type: "string" }, { name: "__row", field: "symbol.id", type: "string" }],
      rows: [{ "r.method": "POST", "r.path": "/orders/new", "r.description": null, __row: "route identity" }],
    } } }));
    expect(markup).toContain(">POST<");
    expect(markup).toContain(">/orders/new<");
    expect(markup).toContain(">NULL<");
    expect(markup).toContain(">route identity<");
  });
});
