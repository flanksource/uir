// What the call graph says about a node, an edge and a package, as data: which glyph, which words,
// which pill text. Pure, with no React and no layout; call-graph-model.ts maps it onto the diagram.
import type { ModuleGraph, ModuleGraphEdge, ModuleGraphNode, ModuleGraphOmitted, ModuleGraphSite } from "./api";

/** What a node's icon says about it. */
export type NodeGlyph =
  | "func" | "method" | "interface_method" | "recursive" | "external_func" | "builtin" | "type" | "field"
  | "var" | "const" | "package" | "unresolved" | "symbol";

/** Every glyph, in legend order. */
export const NODE_GLYPHS: readonly NodeGlyph[] = [
  "func", "method", "interface_method", "recursive", "external_func", "builtin", "type", "field", "var",
  "const", "package", "unresolved", "symbol",
];

/** The legend's name for each glyph. */
export const NODE_GLYPH_NAME: Record<NodeGlyph, string> = {
  func: "function",
  method: "method",
  interface_method: "interface method",
  recursive: "calls itself",
  external_func: "external function",
  builtin: "builtin",
  type: "type",
  field: "field",
  var: "variable",
  const: "constant",
  package: "package scope",
  unresolved: "unresolved call",
  symbol: "other symbol",
};

/** The index's symbol kinds that have a glyph of their own, and the words for each. */
const KINDS = new Map<string, { glyph: NodeGlyph; words: string }>([
  ["func", { glyph: "func", words: "function" }],
  ["method", { glyph: "method", words: "method" }],
  ["builtin", { glyph: "builtin", words: "builtin" }],
  ["type", { glyph: "type", words: "type" }],
  ["field", { glyph: "field", words: "field" }],
  ["var", { glyph: "var", words: "variable" }],
  ["const", { glyph: "const", words: "constant" }],
  ["package", { glyph: "package", words: "package scope" }],
]);

export const DISPATCH_LABEL = "via interface";
/** The room the dispatch icon takes in a pill, in characters of guard text. */
const DISPATCH_ICON_CHARS = 3;

export function isBuiltin(node: ModuleGraphNode): boolean {
  return node.kind === "builtin";
}

/** The node standing for the calls a package makes outside any declaration. */
function isPackageScope(node: ModuleGraphNode): boolean {
  return node.kind === "package";
}

/** A symbol declared outside the indexed snapshots: it has no source, so it cannot be opened or expanded. */
export function isExternal(node: ModuleGraphNode): boolean {
  return !node.location && !node.unresolved && !isBuiltin(node) && !isPackageScope(node);
}

export function siteKey(site: ModuleGraphSite): string {
  return `${site.path}:${site.line}:${site.column}`;
}

/**
 * The index has no kind for an interface method: it is a `method`. It shows in the graph as the target
 * of a call whose site also carries a dispatch edge from the same caller, because the source adds that
 * dispatch edge to each implementation of the interface method called there.
 */
export function interfaceMethods(graph: ModuleGraph): Set<string> {
  const dispatched = new Set(graph.edges.filter((edge) => edge.type === "dispatch")
    .flatMap((edge) => edge.sites.map((site) => `${edge.from} ${siteKey(site)}`)));
  return new Set(graph.edges
    .filter((edge) => edge.type === "call" && edge.sites.some((site) => dispatched.has(`${edge.from} ${siteKey(site)}`)))
    .map((edge) => edge.to));
}

function callsItself(graph: ModuleGraph, node: ModuleGraphNode): boolean {
  return graph.edges.some((edge) => edge.from === node.id && edge.to === node.id);
}

/**
 * The one glyph a node shows. An unresolved call, a builtin and a package scope are what they are;
 * after those, a symbol that calls itself, an external function and an interface method outrank the
 * plain kind. An external method keeps the method glyph: its dashed box already says it has no source.
 */
export function nodeGlyph(graph: ModuleGraph, node: ModuleGraphNode): NodeGlyph {
  if (node.unresolved) return "unresolved";
  if (isBuiltin(node)) return "builtin";
  if (isPackageScope(node)) return "package";
  if (callsItself(graph, node)) return "recursive";
  if (isExternal(node) && node.kind === "func") return "external_func";
  if (interfaceMethods(graph).has(node.id)) return "interface_method";
  return KINDS.get(node.kind)?.glyph ?? "symbol";
}

/** What the node is, in words: `method`, `external function`, `recursive method`, `unresolved call`. */
export function nodeKind(graph: ModuleGraph, node: ModuleGraphNode): string {
  if (node.unresolved) return "unresolved call";
  const kind = interfaceMethods(graph).has(node.id) ? "interface method" : (KINDS.get(node.kind)?.words ?? node.kind.replaceAll("_", " "));
  return [isExternal(node) ? "external" : "", callsItself(graph, node) ? "recursive" : "", kind].filter(Boolean).join(" ");
}

/** The node's tooltip: its kind, its label and its full package path. */
export function nodeTitle(graph: ModuleGraph, node: ModuleGraphNode): string {
  const group = graph.groups?.find((candidate) => candidate.id === node.group)?.label ?? node.group;
  return `${nodeKind(graph, node)} ${node.label}${group ? ` in ${group}` : ""}`;
}

/** The glyphs the response's nodes use, once each, in legend order. */
export function usedGlyphs(graph: ModuleGraph): NodeGlyph[] {
  const used = new Set(graph.nodes.map((node) => nodeGlyph(graph, node)));
  return NODE_GLYPHS.filter((glyph) => used.has(glyph));
}

function counted(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`;
}

/** The excluded nodes per package, most first, then by path. */
export function excludedTally(omitted: ModuleGraphOmitted): { group: string; count: number }[] {
  return Object.entries(omitted.excluded ?? {}).map(([group, count]) => ({ group, count }))
    .sort((a, b) => b.count - a.count || a.group.localeCompare(b.group));
}

/** The omitted chip's parts: what the response left out, with the excluded nodes as one total. */
export function omittedParts(omitted: ModuleGraphOmitted): string[] {
  const unreadable = omitted.unreadable_source?.length ?? 0;
  const excluded = excludedTally(omitted).reduce((total, { count }) => total + count, 0);
  return [
    omitted.node_limit ? "node limit reached" : "",
    omitted.beyond_depth ? `${omitted.beyond_depth} beyond depth` : "",
    omitted.unresolved ? `${omitted.unresolved} unresolved` : "",
    unreadable > 0 ? counted(unreadable, "unreadable file", "unreadable files") : "",
    excluded > 0 ? `${excluded} excluded` : "",
  ].filter(Boolean);
}

/** A group box caption: the package path's last two segments. The full path is the caption's tooltip. */
export function groupCaption(path: string): string {
  return path.split("/").slice(-2).join("/");
}

/** Which of a site's nested guards the pill shows, or how many there are. */
export type GuardLabel = "innermost" | "outermost" | "count";

export interface PillOptions {
  guardLabel: GuardLabel;
  /** Longest guard text a pill shows before it is cut with an ellipsis. */
  truncateAt: number;
}

function guardsOf(site: ModuleGraphSite): string[] {
  return site.guards ?? [];
}

/** An edge is conditional when no call site behind it can be reached unguarded. */
export function isConditional(edge: ModuleGraphEdge): boolean {
  return edge.sites.length > 0 && edge.sites.every((site) => guardsOf(site).length > 0);
}

export function truncate(text: string, max: number): string {
  return text.length <= max ? text : `${text.slice(0, max - 1).trimEnd()}…`;
}

function guardCount(edge: ModuleGraphEdge): string {
  const counts = edge.sites.map((site) => guardsOf(site).length);
  const least = Math.min(...counts);
  const most = Math.max(...counts);
  if (least === most) return `${most} ${most === 1 ? "guard" : "guards"}`;
  return `${least}–${most} guards`;
}

/** The guard a conditional edge's sites share, or how many different ones they have. */
function guardText(edge: ModuleGraphEdge, { guardLabel, truncateAt }: PillOptions): string {
  if (!isConditional(edge)) return "";
  if (guardLabel === "count") return guardCount(edge);
  const chosen = new Set(edge.sites.map((site) => guardsOf(site).at(guardLabel === "innermost" ? -1 : 0)));
  const [only] = chosen;
  if (chosen.size > 1 || only === undefined) return `${chosen.size} conditions`;
  return truncate(only, truncateAt - (edge.type === "dispatch" ? DISPATCH_ICON_CHARS : 0));
}

/**
 * The pill's text: the guard the edge's call sites share, then `×N` when there are several sites.
 * Sites with different guards show how many conditions there are instead of one of them, which would
 * read as if it covered every site. A dispatch edge's guard is cut shorter by the room its icon takes.
 */
export function edgeLabel(edge: ModuleGraphEdge, options: PillOptions): string | undefined {
  const text = [guardText(edge, options), edge.sites.length > 1 ? `×${edge.sites.length}` : ""].filter(Boolean).join(" ");
  return text === "" ? undefined : text;
}

/** One line per distinct guard chain behind the edge: the guards, outermost first, joined with ∧. */
export function edgeTitle(edge: ModuleGraphEdge): string | undefined {
  const chains = new Set(edge.sites.map(guardsOf).filter((guards) => guards.length > 0).map((guards) => guards.join(" ∧ ")));
  return chains.size === 0 ? undefined : [...chains].join("\n");
}
