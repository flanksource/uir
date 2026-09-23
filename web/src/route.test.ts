import { expect, it } from "vitest";
import { readRoute, routeURL } from "./route";

it("round trips the selected snapshot, source, node, and query through the URL", () => {
  const original = readRoute({ pathname: "/query", search: "?project=acme%2Fservice&snapshot=s1&root=r1&source=f1&node=n1&search=Handler&expression=type%3AFunction&offset=100" });
  const url = new URL(routeURL(original), "http://localhost");
  expect(readRoute(url)).toEqual(original);
});
