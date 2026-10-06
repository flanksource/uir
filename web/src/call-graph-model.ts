// Pure mapping from the server's call graph to what GraphDiagram draws, how a `+N` expansion merges
// into the held graph, and where a node or call site opens in the Explorer. It decides what is shown
// and leaves drawing a glyph to the DiagramGlyphs it is handed, so it renders nothing itself.
import type { ReactNode } from "react";

import type { ModuleGraph, ModuleGraphDirection, ModuleGraphEdge, ModuleGraphGroup, ModuleGraphNode, ModuleGraphSite, ModuleNode } from "./api";
import {
  DISPATCH_LABEL, edgeLabel, edgeTitle, groupCaption, isBuiltin, isConditional, isExternal, nodeGlyph, nodeKind, nodeTitle,
  siteKey, type NodeGlyph, type PillOptions,
} from "./call-graph-labels";
import { nodeIdentityKey, parseIdentityKey, symbolSelector } from "./query-model";
import type { Route } from "./route";

/** The deepest graph the server builds (graph.MaxDepth). */
export const MAX_GRAPH_DEPTH = 8;

export interface GraphView {
  direction: ModuleGraphDirection;
  /** Nodes further than this many hops from the root are hidden. */
  depth: number;
  /** Node ids shown regardless of `depth`, because a `+N` expansion revealed them. */
  revealed: readonly string[];
}

export interface VisibleGraph {
  nodes: ModuleGraphNode[];
  edges: ModuleGraphEdge[];
}

export interface DiagramOptions extends PillOptions {
  /** Box nodes by their `group` (the package). */
  grouped: boolean;
}

// The shapes GraphDiagram's "columns" layout takes, declared here so the pure model does not depend on
// the clicky-ui release that introduced them.
export interface DiagramNode {
  id: string;
  label: string;
  icon: ReactNode;
  title: string;
  level: number;
  group?: string;
  tone?: "info" | "warning";
  muted?: boolean;
  expandCount?: number;
}

export interface DiagramEdge {
  id: string;
  from: string;
  to: string;
  dashed: boolean;
  tone?: "info";
  icon?: ReactNode;
  iconLabel?: string;
  label?: string;
  title?: string;
}

export interface DiagramGroup {
  id: string;
  label: ReactNode;
  title: string;
}

export interface Diagram {
  nodes: DiagramNode[];
  edges: DiagramEdge[];
  groups: DiagramGroup[];
}

/** Draws the glyphs the model chooses. The pane supplies icons; the tests supply strings. */
export interface DiagramGlyphs {
  /** `words` is the icon's name for a screen reader, such as `external function`. */
  node(glyph: NodeGlyph, words: string): ReactNode;
  dispatch(): ReactNode;
  group(caption: string): ReactNode;
}

export interface DiagramInput {
  graph: ModuleGraph;
  visible: VisibleGraph;
  direction: ModuleGraphDirection;
  options: DiagramOptions;
  glyphs: DiagramGlyphs;
}

export function requireNode(graph: ModuleGraph, id: string): ModuleGraphNode {
  const node = graph.nodes.find((candidate) => candidate.id === id);
  if (!node) throw new Error(`call graph: no node "${id}"`);
  return node;
}

function inDirection(depth: number, direction: ModuleGraphDirection): boolean {
  if (direction === "both" || depth === 0) return true;
  return direction === "callees" ? depth > 0 : depth < 0;
}

/** The nodes on the requested side within `depth`, plus revealed ones, and the edges between them. */
export function visibleGraph(graph: ModuleGraph, view: GraphView): VisibleGraph {
  const revealed = new Set(view.revealed);
  const nodes = graph.nodes.filter((node) => inDirection(node.depth, view.direction) && (Math.abs(node.depth) <= view.depth || revealed.has(node.id)));
  const ids = new Set(nodes.map((node) => node.id));
  return { nodes, edges: graph.edges.filter((edge) => ids.has(edge.from) && ids.has(edge.to)) };
}

/** A caller grows toward its own callers; the root and callees grow toward their callees. */
function outerSide(node: ModuleGraphNode): "in" | "out" {
  return node.depth < 0 ? "in" : "out";
}

/** The direction a `+N` expansion of the node walks: its outer side, the side `+N` counts. */
export function expansionDirection(node: ModuleGraphNode): Exclude<ModuleGraphDirection, "both"> {
  return outerSide(node) === "in" ? "callers" : "callees";
}

/** The ids at the far end of a node's edges on its outer side, as the response holds them. */
function outerNeighbours(graph: ModuleGraph, node: ModuleGraphNode): string[] {
  const side = outerSide(node);
  return graph.edges.filter((edge) => (side === "out" ? edge.from : edge.to) === node.id).map((edge) => (side === "out" ? edge.to : edge.from));
}

/** The neighbours on a node's outer side that the response holds and are not drawn. */
export function hiddenNeighbours(graph: ModuleGraph, visible: VisibleGraph, nodeId: string): string[] {
  const drawn = new Set(visible.nodes.map((node) => node.id));
  return outerNeighbours(graph, requireNode(graph, nodeId)).filter((id, index, all) => !drawn.has(id) && all.indexOf(id) === index);
}

/**
 * The `+N` on a node: how many more edges there are to load on its outer side. Only that side's count
 * is a total (`out` for the root and callees, `in` for callers); the other equals the edges in the
 * response. Excluded neighbours are in neither. From it come off the edges drawn. A node with no source
 * cannot be expanded, nor can the side the direction hides.
 */
export function expandCount(graph: ModuleGraph, visible: VisibleGraph, nodeId: string, direction: ModuleGraphDirection): number {
  const node = requireNode(graph, nodeId);
  const side = outerSide(node);
  if (!node.location || direction === (side === "out" ? "callers" : "callees")) return 0;
  const drawn = new Set(visible.nodes.map((entry) => entry.id));
  const accounted = outerNeighbours(graph, node).filter((id) => drawn.has(id)).length;
  return Math.max(0, (side === "out" ? node.out : node.in) - accounted);
}

/** A node's call totals on the sides the response walked: callees at depth >= 0, callers at depth <= 0. */
export function walkedTotals(graph: ModuleGraph, node: ModuleGraphNode): { callers?: number; callees?: number } {
  const walked = (sign: number) => graph.nodes.some((other) => Math.sign(other.depth) === sign);
  return {
    ...(node.depth <= 0 && walked(-1) ? { callers: node.in } : {}),
    ...(node.depth >= 0 && walked(1) ? { callees: node.out } : {}),
  };
}

function distinctSites(sites: ModuleGraphSite[]): ModuleGraphSite[] {
  return sites.filter((site, index) => sites.findIndex((other) => siteKey(other) === siteKey(site)) === index);
}

/**
 * Merges a `symbol=<id>&depth=1` expansion into the held graph. The expansion is rooted at a held node,
 * so its depths are re-signed from that node's depth. Held nodes keep their place; edges are keyed by
 * `from|to|type` and gain the call sites they did not hold; the held response's other fields stay.
 */
export function mergeGraph<T extends ModuleGraph>(held: T, expansion: ModuleGraph): T {
  const [rootId] = expansion.roots;
  const base = held.nodes.find((node) => node.id === rootId);
  if (rootId === undefined || !base) throw new Error(`call graph: expansion root "${rootId}" is not in the held graph`);
  const heldIds = new Set(held.nodes.map((node) => node.id));
  const added = expansion.nodes.filter((node) => !heldIds.has(node.id)).map((node) => ({ ...node, depth: base.depth + node.depth }));
  const incoming = new Map(expansion.edges.map((edge) => [`${edge.from}|${edge.to}|${edge.type}`, edge]));
  const edges = held.edges.map((edge) => {
    const more = incoming.get(`${edge.from}|${edge.to}|${edge.type}`);
    incoming.delete(`${edge.from}|${edge.to}|${edge.type}`);
    return more ? { ...edge, sites: distinctSites([...edge.sites, ...more.sites]) } : edge;
  });
  const groupIds = new Set((held.groups ?? []).map((group) => group.id));
  const groups: ModuleGraphGroup[] = [...(held.groups ?? []), ...(expansion.groups ?? []).filter((group) => !groupIds.has(group.id))];
  return { ...held, nodes: [...held.nodes, ...added], edges: [...edges, ...incoming.values()], groups };
}

type Located = Required<Pick<NonNullable<ModuleGraphNode["location"]>, "source_id" | "snapshot_id" | "checkout_path" | "path" | "identity_key">>
  & NonNullable<ModuleGraphNode["location"]>;

function requireLocation(node: ModuleGraphNode): Located {
  const location = node.location;
  if (!location) throw new Error(`${node.label} has no indexed source to reveal`);
  const { source_id, snapshot_id, checkout_path, path, identity_key } = location;
  if (!source_id || !snapshot_id || !checkout_path || !path || !identity_key) {
    throw new Error(`${node.label} has a location without a source, snapshot, checkout, path or identity`);
  }
  return { ...location, source_id, snapshot_id, checkout_path, path, identity_key };
}

/** The Explorer's node id of a graph node (`<source_id>:<identity_key>`), or undefined for a node without a source. */
export function explorerNodeId(node: ModuleGraphNode): string | undefined {
  if (!node.location) return undefined;
  const { source_id, identity_key } = node.location;
  if (!source_id || !identity_key) throw new Error(`${node.label} has a location without a source_id or identity_key`);
  return `${source_id}:${identity_key}`;
}

export type GraphRevealPatch = Pick<Route, "location" | "snapshot" | "source" | "line" | "column" | "explorerTab"> & Partial<Pick<Route, "node" | "fileSearch" | "symbolSearch">>;

/** Selects a node and reveals its declaration in the Source tab, in the checkout and snapshot it was indexed from. */
export function nodeRevealPatch(node: ModuleGraphNode): GraphRevealPatch {
  const location = requireLocation(node);
  return { location: location.checkout_path, snapshot: location.snapshot_id, source: location.source_id, node: `${location.source_id}:${location.identity_key}`,
    line: location.line ?? 0, column: Math.max(1, location.column ?? 1), explorerTab: "source", fileSearch: "", symbolSearch: "" };
}

export type GraphRoot = { symbol: string } | { selector: string } | { empty: string };

/**
 * What the graph is rooted at: the pinned symbol, else the selected function or method by its exact
 * selector, else why there is nothing to draw. A selected symbol that cannot be spelled as a selector fails.
 */
export function graphRoot(pinned: string, selected: Pick<ModuleNode, "id" | "source_id" | "kind" | "symbol"> | undefined): GraphRoot {
  if (pinned) return { symbol: pinned };
  if (!selected) return { empty: "Select a function or method to draw its call graph." };
  if (selected.kind !== "func" && selected.kind !== "method") return { empty: `${selected.symbol} is a ${selected.kind}: only a function or method has a call graph.` };
  return { selector: symbolSelector(parseIdentityKey(nodeIdentityKey(selected))) };
}

/** Whether a site of the edge can be opened: its caller, whose body holds it, has a source. */
export function canRevealSites(graph: ModuleGraph, edge: ModuleGraphEdge): boolean {
  return Boolean(requireNode(graph, edge.from).location);
}

/**
 * Reveals a call site in the Source tab. A call site lies in its caller's body, so in the caller's file.
 * The selection is kept unless the caller is in another snapshot, where it selects the caller instead.
 */
export function siteRevealPatch(graph: ModuleGraph, edge: ModuleGraphEdge, site: ModuleGraphSite, snapshot: string): GraphRevealPatch {
  const caller = requireNode(graph, edge.from);
  const location = requireLocation(caller);
  if (site.path !== location.path) throw new Error(`call site ${site.path}:${site.line} is not in ${location.path}, the file of its caller ${caller.label}`);
  return { location: location.checkout_path, snapshot: location.snapshot_id, source: location.source_id, line: site.line ?? 0, column: Math.max(1, site.column ?? 1), explorerTab: "source",
    ...(location.snapshot_id !== snapshot ? { node: `${location.source_id}:${location.identity_key}` } : {}) };
}

/**
 * The order nodes are handed to the diagram, which stacks groups in the order they first appear: the
 * root's package, then the other indexed packages, then the others, then nodes in no package.
 */
function groupRank(graph: ModuleGraph): (node: ModuleGraphNode) => number {
  const rootGroups = new Set(graph.roots.map((id) => requireNode(graph, id).group));
  const indexed = new Set(graph.nodes.filter((node) => node.location).map((node) => node.group));
  return (node) => {
    if (node.group === undefined) return 3;
    if (rootGroups.has(node.group)) return 0;
    return indexed.has(node.group) ? 1 : 2;
  };
}

function toDiagramNode(node: ModuleGraphNode, { graph, visible, direction, options, glyphs }: DiagramInput): DiagramNode {
  const count = expandCount(graph, visible, node.id, direction);
  const tone = graph.roots.includes(node.id) ? "info" : node.unresolved ? "warning" : undefined;
  return {
    id: node.id,
    label: node.label,
    icon: glyphs.node(nodeGlyph(graph, node), nodeKind(graph, node)),
    title: nodeTitle(graph, node),
    level: node.depth,
    ...(options.grouped && node.group !== undefined ? { group: node.group } : {}),
    ...(tone !== undefined ? { tone } : {}),
    ...(node.unresolved || isBuiltin(node) || isExternal(node) ? { muted: true } : {}),
    ...(count > 0 ? { expandCount: count } : {}),
  };
}

function toDiagramEdge(edge: ModuleGraphEdge, { options, glyphs }: DiagramInput): DiagramEdge {
  const label = edgeLabel(edge, options);
  const title = edgeTitle(edge);
  return {
    id: edge.id,
    from: edge.from,
    to: edge.to,
    dashed: isConditional(edge),
    ...(edge.type === "dispatch" ? { tone: "info" as const, icon: glyphs.dispatch(), iconLabel: DISPATCH_LABEL } : {}),
    ...(label !== undefined ? { label } : {}),
    ...(title !== undefined ? { title } : {}),
  };
}

/** Mean glyph advance of the 10px pill text, and the pill's padding plus a little air. */
const PILL_CHAR_WIDTH = 5.2;
const PILL_CHROME = 22;
/** The room a pill's icon and its gap take, in characters. */
const PILL_ICON_CHARS = 3;
const MIN_COLUMN_GAP = 96;

/**
 * The gap between columns: as wide as the longest pill on an edge between two columns, so no pill
 * covers a node. Wider gaps than the pills need make the diagram wider, and so smaller once fitted.
 */
export function columnGap({ nodes, edges }: {
  nodes: readonly Pick<DiagramNode, "id" | "label" | "level">[];
  edges: readonly Pick<DiagramEdge, "id" | "from" | "to" | "label" | "icon">[];
}): number {
  const level = new Map(nodes.map((node) => [node.id, node.level]));
  const chars = edges.filter((edge) => level.get(edge.from) !== level.get(edge.to))
    .map((edge) => (edge.label?.length ?? 0) + (edge.icon != null ? PILL_ICON_CHARS : 0));
  return Math.max(MIN_COLUMN_GAP, Math.round(Math.max(0, ...chars) * PILL_CHAR_WIDTH) + PILL_CHROME);
}

export function toDiagram(input: DiagramInput): Diagram {
  const { graph, visible, options, glyphs } = input;
  const rank = groupRank(graph);
  return {
    nodes: [...visible.nodes].sort((a, b) => rank(a) - rank(b)).map((node) => toDiagramNode(node, input)),
    edges: visible.edges.map((edge) => toDiagramEdge(edge, input)),
    groups: options.grouped ? (graph.groups ?? []).map(({ id, label }) => ({ id, label: glyphs.group(groupCaption(label)), title: label })) : [],
  };
}
