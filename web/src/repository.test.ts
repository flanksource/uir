import { expect, it } from "vitest";
import { repositoryURL } from "./repository";

it.each([
  ["https://github.com/acme/service.git", "https://github.com/acme/service.git"],
  ["git@github.com:acme/service.git", "https://github.com/acme/service"],
  ["ssh://git@gitlab.example/acme/service.git", "https://gitlab.example/acme/service"],
  ["/local/path", null],
])("returns an openable repository URL for %s", (input, expected) => {
  expect(repositoryURL(input)).toBe(expected);
});
