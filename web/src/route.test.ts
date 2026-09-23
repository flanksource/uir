import { expect, it } from "vitest";
import { readRoute, routeURL } from "./route";

it("round trips the selected module, checkout, snapshot, source, node, and query through the URL", () => {
  const original = readRoute({ pathname: "/query", search: "?module=example.org%2Fservice&location=%2Fcheckout%2Fservice&snapshot=s1&source=f1&node=n1&search=Handler&expression=nodes&offset=100" });
  const url = new URL(routeURL(original), "http://localhost");
  expect(readRoute(url)).toEqual(original);
});
