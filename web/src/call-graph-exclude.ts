// Editing the call graph's package exclusions: the patterns `GET /api/v1/modules/graph?exclude=` takes.
// A pattern is an import path, a path ending in /..., or one of the keywords below; `none` alone
// excludes nothing, and no patterns at all take the server's defaults.
import type { ModuleGraphPackage } from "./api";

export const EXCLUDE_NONE = "none";
export const EXCLUDE_STD = "std";
export const EXCLUDE_BUILTIN = "builtin";
export const EXCLUDE_EXTERNAL = "external";
const KEYWORDS = new Set([EXCLUDE_STD, EXCLUDE_BUILTIN, EXCLUDE_EXTERNAL]);

/** The route's exclude value as patterns; empty is undefined, which takes the server's defaults. */
export function parseExclude(text: string): string[] | undefined {
  return text === "" ? undefined : text.split(",");
}

/** The route's exclude value for these patterns. No patterns is `none`, because an empty value means the defaults. */
export function formatExclude(patterns: readonly string[]): string {
  const active = withoutNone(patterns);
  return active.length === 0 ? EXCLUDE_NONE : active.join(",");
}

function withoutNone(patterns: readonly string[]): string[] {
  return patterns.filter((pattern) => pattern !== EXCLUDE_NONE);
}

/**
 * A standard library package, as Go's `std` pattern tells it without a module: a package outside the
 * scope whose first path element has no dot. The builtin pseudo-package has its own keyword.
 */
export function isStdPackage(pkg: ModuleGraphPackage): boolean {
  return pkg.external && pkg.path !== EXCLUDE_BUILTIN && !pkg.path.split("/")[0].includes(".");
}

/** Whether one pattern excludes the package. */
export function excludesPackage(pattern: string, pkg: ModuleGraphPackage): boolean {
  switch (pattern) {
    case EXCLUDE_NONE: return false;
    case EXCLUDE_STD: return isStdPackage(pkg);
    case EXCLUDE_BUILTIN: return pkg.path === EXCLUDE_BUILTIN;
    case EXCLUDE_EXTERNAL: return pkg.external;
  }
  if (pattern.endsWith("/...")) {
    const base = pattern.slice(0, -"/...".length);
    return pkg.path === base || pkg.path.startsWith(`${base}/`);
  }
  return pkg.path === pattern;
}

/** Excludes one package by its exact path. */
export function excludePackage(patterns: readonly string[], path: string): string[] {
  return [...withoutNone(patterns), path];
}

/**
 * Excludes a whole group: `std` or a `prefix/...` pattern. The exact and /... patterns it subsumes go,
 * so the patterns in force stay the ones that matter; keywords stay.
 */
export function excludeGroup(patterns: readonly string[], pattern: string, packages: readonly ModuleGraphPackage[]): string[] {
  if (pattern !== EXCLUDE_STD && !pattern.endsWith("/...")) throw new Error(`${pattern} is not std or a path ending in /...`);
  const subsumes = (existing: string): boolean => {
    if (KEYWORDS.has(existing)) return false;
    if (pattern === EXCLUDE_STD) return packages.some((pkg) => pkg.path === existing && isStdPackage(pkg));
    const base = pattern.slice(0, -"/...".length);
    const path = existing.endsWith("/...") ? existing.slice(0, -"/...".length) : existing;
    return path === base || path.startsWith(`${base}/`);
  };
  return [...withoutNone(patterns).filter((existing) => !subsumes(existing)), pattern];
}

/**
 * Brings the excluded ones of these packages back; the others are already shown. Every pattern that
 * excludes one of them goes; a keyword or a /... pattern that also covered other reached packages is
 * replaced by their exact paths, so they stay excluded.
 */
export function includePackages(patterns: readonly string[], targets: readonly ModuleGraphPackage[], packages: readonly ModuleGraphPackage[]): string[] {
  const excluded = targets.filter((target) => target.excluded);
  if (excluded.length === 0) throw new Error(`None of ${targets.map((target) => target.path).join(", ")} is excluded`);
  const active = withoutNone(patterns);
  const uncovered = excluded.find((target) => !active.some((pattern) => excludesPackage(pattern, target)));
  if (uncovered) throw new Error(`Package ${uncovered.path} is reported excluded, but none of ${active.join(", ")} matches it`);
  const covering = active.filter((pattern) => excluded.some((target) => excludesPackage(pattern, target)));
  const rest = active.filter((pattern) => !covering.includes(pattern));
  const targetPaths = new Set(excluded.map((target) => target.path));
  const replacements = packages.filter((pkg) => !targetPaths.has(pkg.path)
    && covering.some((pattern) => excludesPackage(pattern, pkg)) && !rest.some((pattern) => excludesPackage(pattern, pkg)));
  return [...rest, ...replacements.map((pkg) => pkg.path)];
}

/** Adds or removes the `external` keyword behind "Hide all external". */
export function setExternalHidden(patterns: readonly string[], hidden: boolean): string[] {
  const others = withoutNone(patterns).filter((pattern) => pattern !== EXCLUDE_EXTERNAL);
  return hidden ? [...others, EXCLUDE_EXTERNAL] : others;
}

/** Why a typed pattern cannot be added, or undefined when it can. The server checks the import path itself. */
export function excludePatternError(patterns: readonly string[], pattern: string): string | undefined {
  if (pattern === "") return "Enter a package path or a path ending in /...";
  if (/[\s,]/.test(pattern)) return `One pattern at a time: ${pattern} has a comma or a space`;
  if (pattern === EXCLUDE_NONE) return "Use Show all to exclude nothing";
  if (patterns.includes(pattern)) return `${pattern} is already excluded`;
  return undefined;
}

export function addExcludePattern(patterns: readonly string[], pattern: string): string[] {
  return [...withoutNone(patterns), pattern];
}

export function removeExcludePattern(patterns: readonly string[], pattern: string): string[] {
  return patterns.filter((candidate) => candidate !== pattern);
}
