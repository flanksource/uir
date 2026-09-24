import { expect, it } from "vitest";
import { readRoute, routeURL } from "./route";

it("opens the task manager at its own route", () => {
  const route = readRoute({ pathname: "/tasks", search: "?module=example.org%2Fservice" });
  expect(route.view).toBe("tasks");
  expect(routeURL(route)).toBe("/tasks?module=example.org%2Fservice");
});

it("round trips the selected module, checkout, snapshot, source, node, filters, and query through the URL", () => {
  const original = readRoute({ pathname: "/query", search: "?module=example.org%2Fservice&location=%2Fcheckout%2Fservice&snapshot=s1&source=f1&node=n1&fileSearch=internal&symbolSearch=Handler&expression=nodes&offset=100" });
  const url = new URL(routeURL(original), "http://localhost");
  expect(readRoute(url)).toEqual(original);
});

it("moves legacy Symbols links into the explorer with their symbol search", () => {
  const route = readRoute({ pathname: "/nodes", search: "?snapshot=s1&source=f1&node=n1&search=Handler" });
  expect(route).toMatchObject({ view: "explorer", snapshot: "s1", source: "f1", node: "n1", symbolSearch: "Handler", fileSearch: "" });
  expect(routeURL(route)).toBe("/explorer?snapshot=s1&source=f1&node=n1&symbolSearch=Handler");
});

it("keeps a legacy explorer path search as a file search", () => {
  const route = readRoute({ pathname: "/explorer", search: "?snapshot=s1&search=internal" });
  expect(route).toMatchObject({ fileSearch: "internal", symbolSearch: "" });
});
