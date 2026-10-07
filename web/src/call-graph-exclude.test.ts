import { describe, expect, it } from "vitest";

import type { ModuleGraphPackage } from "./api";
import {
  addExcludePattern,
  excludeGroup,
  excludePackage,
  excludePatternError,
  excludesPackage,
  formatExclude,
  includePackages,
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

describe("includePackages", () => {
  it("drops an exact path that excludes the package", () => {
    expect(includePackages([...DEFAULTS, UUID.path], [{ ...UUID, excluded: true }], PACKAGES)).toEqual(DEFAULTS);
  });

  it("replaces a keyword with the exact paths of the other packages it covered", () => {
    expect(includePackages(DEFAULTS, [FMT], PACKAGES)).toEqual(["builtin", "gorm.io/...", "strings"]);
  });

  it("replaces a /... pattern with the exact paths of the other packages below it", () => {
    expect(includePackages(DEFAULTS, [GORM], PACKAGES)).toEqual(["std", "builtin", "gorm.io/gorm/clause"]);
  });

  it("drops every pattern that covers the package, leaving out paths another remaining pattern still covers", () => {
    expect(includePackages(["external", "std", "builtin"], [FMT], PACKAGES)).toEqual(["builtin", "gorm.io/gorm", "strings", "github.com/google/uuid", "gorm.io/gorm/clause"]);
  });

  it("brings back a folder's packages covered by a keyword and an exact path, re-adding none of them and skipping the shown ones", () => {
    expect(includePackages(["std", "builtin", "gorm.io/gorm/clause"], [FMT, GORM_CLAUSE, QUERY], PACKAGES)).toEqual(["builtin", "strings"]);
  });

  it("leaves none when the last pattern goes", () => {
    expect(formatExclude(includePackages(["builtin"], [BUILTIN], PACKAGES))).toBe("none");
  });

  it("fails when no pattern covers a package the server reported excluded", () => {
    expect(() => includePackages(["builtin"], [FMT], PACKAGES)).toThrow("Package fmt is reported excluded, but none of builtin matches it");
  });

  it("fails when none of the packages is excluded", () => {
    expect(() => includePackages(DEFAULTS, [QUERY, UUID], PACKAGES)).toThrow(`None of ${QUERY.path}, ${UUID.path} is excluded`);
  });
});

describe("excludeGroup", () => {
  it("adds a /... pattern, dropping the exact and /... patterns it subsumes and keeping keywords", () => {
    expect(excludeGroup(["std", "gorm.io", "gorm.io/gorm/clause", "gorm.io/gorm/...", "gorm.iox/driver", "external"], "gorm.io/...", PACKAGES))
      .toEqual(["std", "gorm.iox/driver", "external", "gorm.io/..."]);
  });

  it("adds std, dropping the exact paths of reached standard library packages", () => {
    expect(excludeGroup(["fmt", "builtin", "gorm.io/gorm"], "std", PACKAGES)).toEqual(["builtin", "gorm.io/gorm", "std"]);
  });

  it("replaces none", () => {
    expect(excludeGroup(["none"], "github.com/google/...", PACKAGES)).toEqual(["github.com/google/..."]);
  });

  it("fails on a pattern that is not a group", () => {
    expect(() => excludeGroup(DEFAULTS, "gorm.io/gorm", PACKAGES)).toThrow("gorm.io/gorm is not std or a path ending in /...");
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
