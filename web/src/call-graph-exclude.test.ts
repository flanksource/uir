import { describe, expect, it } from "vitest";

import type { ModuleGraphPackage } from "./api";
import {
  addExcludePattern,
  excludePackage,
  excludePatternError,
  excludesPackage,
  formatExclude,
  includePackage,
  parseExclude,
  removeExcludePattern,
  setExternalHidden,
} from "./call-graph-exclude";

const DEFAULTS = ["std", "builtin", "gorm.io/..."];
const pkg = (path: string, external: boolean, excluded: boolean, nodes = 1): ModuleGraphPackage => ({ path, external, excluded, nodes });
const QUERY = pkg("github.com/flanksource/uir/query", false, false, 20);
const FMT = pkg("fmt", true, true, 5);
const STRINGS = pkg("strings", true, true, 3);
const BUILTIN = pkg("builtin", true, true, 4);
const GORM = pkg("gorm.io/gorm", true, true, 4);
const GORM_CLAUSE = pkg("gorm.io/gorm/clause", true, true, 1);
const UUID = pkg("github.com/google/uuid", true, false, 2);
const PACKAGES = [QUERY, FMT, BUILTIN, GORM, STRINGS, UUID, GORM_CLAUSE];

describe("exclude text", () => {
  it.each([
    ["", undefined],
    ["none", ["none"]],
    ["std,builtin,gorm.io/...", DEFAULTS],
  ])("reads %j from the route as %j, empty meaning the server defaults", (text, patterns) => {
    expect(parseExclude(text)).toEqual(patterns);
  });

  it("writes an empty list as none, since an empty route value means the server defaults", () => {
    expect([formatExclude([]), formatExclude(["none"]), formatExclude(DEFAULTS)]).toEqual(["none", "none", "std,builtin,gorm.io/..."]);
  });
});

describe("excludesPackage", () => {
  it("matches each pattern kind the way the server's grammar does", () => {
    const matches = (pattern: string) => PACKAGES.filter((entry) => excludesPackage(pattern, entry)).map((entry) => entry.path);
    expect({
      std: matches("std"), builtin: matches("builtin"), external: matches("external"), tree: matches("gorm.io/..."),
      exact: matches("gorm.io/gorm"), none: matches("none"),
    }).toEqual({
      std: ["fmt", "strings"], builtin: ["builtin"],
      external: ["fmt", "builtin", "gorm.io/gorm", "strings", "github.com/google/uuid", "gorm.io/gorm/clause"],
      tree: ["gorm.io/gorm", "gorm.io/gorm/clause"], exact: ["gorm.io/gorm"], none: [],
    });
  });

  it("never takes an in-scope package without a dot in its path for the standard library", () => {
    expect(excludesPackage("std", pkg("localmod/internal", false, false))).toBe(false);
  });
});

describe("excludePackage", () => {
  it("adds the exact path, replacing none", () => {
    expect([excludePackage(DEFAULTS, UUID.path), excludePackage(["none"], UUID.path)]).toEqual([[...DEFAULTS, UUID.path], [UUID.path]]);
  });
});

describe("includePackage", () => {
  it("drops an exact path that excludes the package", () => {
    expect(includePackage([...DEFAULTS, UUID.path], { ...UUID, excluded: true }, PACKAGES)).toEqual(DEFAULTS);
  });

  it("replaces a keyword with the exact paths of the other packages it covered", () => {
    expect(includePackage(DEFAULTS, FMT, PACKAGES)).toEqual(["builtin", "gorm.io/...", "strings"]);
  });

  it("replaces a /... pattern with the exact paths of the other packages below it", () => {
    expect(includePackage(DEFAULTS, GORM, PACKAGES)).toEqual(["std", "builtin", "gorm.io/gorm/clause"]);
  });

  it("drops every pattern that covers the package, leaving out paths another remaining pattern still covers", () => {
    expect(includePackage(["external", "std", "builtin"], FMT, PACKAGES)).toEqual(["builtin", "gorm.io/gorm", "strings", "github.com/google/uuid", "gorm.io/gorm/clause"]);
  });

  it("leaves none when the last pattern goes", () => {
    expect(formatExclude(includePackage(["builtin"], BUILTIN, PACKAGES))).toBe("none");
  });

  it("fails when no pattern covers a package the server reported excluded", () => {
    expect(() => includePackage(["builtin"], FMT, PACKAGES)).toThrow('Package fmt is reported excluded, but none of builtin matches it');
  });
});

describe("setExternalHidden", () => {
  it("adds and removes the external keyword, replacing none and never doubling it", () => {
    expect([
      setExternalHidden(DEFAULTS, true),
      setExternalHidden(["none"], true),
      setExternalHidden([...DEFAULTS, "external"], true),
      setExternalHidden([...DEFAULTS, "external"], false),
    ]).toEqual([[...DEFAULTS, "external"], ["external"], [...DEFAULTS, "external"], DEFAULTS]);
  });
});

describe("free-text patterns", () => {
  it.each([
    ["", "Enter a package path or a path ending in /..."],
    ["a,b", "One pattern at a time: a,b has a comma or a space"],
    ["github.com/x y", "One pattern at a time: github.com/x y has a comma or a space"],
    ["gorm.io/...", "gorm.io/... is already excluded"],
    ["none", "Use Show all to exclude nothing"],
    ["github.com/google/...", undefined],
  ])("checks %j before it is added: %j", (pattern, error) => {
    expect(excludePatternError(DEFAULTS, pattern)).toBe(error);
  });

  it("adds a pattern after the others, replacing none, and removes one", () => {
    expect([addExcludePattern(["none"], "github.com/google/..."), removeExcludePattern(DEFAULTS, "builtin")])
      .toEqual([["github.com/google/..."], ["std", "gorm.io/..."]]);
  });
});
