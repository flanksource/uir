import type { ReactNode } from "react";
import { Button } from "@flanksource/clicky-ui/components";
import { Badge, KeyValueList, type KeyValueListItem } from "@flanksource/clicky-ui/data";
import { UiWarningTriangle } from "@flanksource/clicky-ui/icons";
import type { ModuleGraph, ModuleGraphEdge, ModuleGraphNode, ModuleGraphSite } from "./api";
import { CallIcon, DispatchIcon, NodeGlyphIcon, PackageIcon } from "./call-graph-glyphs";
import { DISPATCH_LABEL, isBuiltin, isExternal, NODE_GLYPH_NAME, nodeGlyph, nodeKind, usedGlyphs } from "./call-graph-labels";
import { requireNode, walkedTotals } from "./call-graph-model";

function siteLocation(site: ModuleGraphSite): string {
  return `${site.path ?? "unknown file"}:${site.line ?? "?"}`;
}

function SiteBody({ site }: { site: ModuleGraphSite }) {
  const guards = site.guards ?? [];
  return <>
    <span className="block font-mono text-xs text-primary">{siteLocation(site)}</span>
    {site.text && <code className="block max-h-24 overflow-hidden whitespace-pre-wrap break-words rounded bg-muted px-1.5 py-1 font-mono text-xs" title={site.text}>{site.text}</code>}
    <span className="flex flex-wrap items-center gap-1">
      {guards.length === 0
        ? <span className="text-xs text-muted-foreground">Unconditional</span>
        : guards.map((guard, index) => <Badge key={`${index}-${guard}`} variant="outline" size="sm" wrap className="h-auto py-0.5 font-mono">{guard}</Badge>)}
    </span>
  </>;
}

/** The edge's type as its icon, with the word it replaced as the tooltip. */
function EdgeTypeIcon({ type }: { type: ModuleGraphEdge["type"] }) {
  const words = type === "dispatch" ? `dispatch ${DISPATCH_LABEL}` : "call";
  const Icon = type === "dispatch" ? DispatchIcon : CallIcon;
  return <span title={words} className="flex text-base leading-none"><Icon title={words} /></span>;
}

/**
 * The call sites behind the selected edge: where, the call as written, and every guard outermost first.
 * With `onReveal` each site is a button that opens its line in the Source tab.
 */
export function EdgeSites({ edge, graph, onReveal }: { edge: ModuleGraphEdge; graph: ModuleGraph; onReveal?: (site: ModuleGraphSite) => void }) {
  return <section className="space-y-2" aria-label="Call sites">
    <header className="space-y-1">
      <h3 className="break-words text-sm font-semibold">{requireNode(graph, edge.from).label} → {requireNode(graph, edge.to).label}</h3>
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <EdgeTypeIcon type={edge.type} />{edge.sites.length} call {edge.sites.length === 1 ? "site" : "sites"}
        {!onReveal && <span>· the caller has no indexed source</span>}
      </div>
    </header>
    <ol className="divide-y divide-border rounded-md border border-border">
      {edge.sites.map((site) => <li key={`${siteLocation(site)}:${site.column ?? 0}`}>
        {onReveal
          ? <button type="button" className="w-full space-y-1.5 px-3 py-2.5 text-left hover:bg-muted" aria-label={`Open ${siteLocation(site)} in Source`} onClick={() => onReveal(site)}><SiteBody site={site} /></button>
          : <div className="space-y-1.5 px-3 py-2.5"><SiteBody site={site} /></div>}
      </li>)}
    </ol>
  </section>;
}

function nodeStatus(node: ModuleGraphNode): string {
  if (node.unresolved) return "Unresolved: the index has no target for this call";
  if (isBuiltin(node)) return "Builtin: no source";
  if (isExternal(node)) return "External: declared outside the indexed snapshots";
  return node.location ? "Indexed" : "Package scope: calls made outside any declaration";
}

function Mono({ children }: { children: ReactNode }) {
  return <span className="break-all font-mono text-xs">{children}</span>;
}

/** What a node is and where it is declared, with the actions that open it or re-root the graph on it. */
export function NodeDetails({ node, graph, isRoot, onOpen, onFocus }: {
  node: ModuleGraphNode; graph: ModuleGraph; isRoot: boolean; onOpen: () => void; onFocus: () => void;
}) {
  const { module, package: pkg = node.group, type, method, signature } = node.identifier;
  const location = node.location;
  const kind = nodeKind(graph, node);
  const totals = walkedTotals(graph, node);
  const items: KeyValueListItem[] = [
    { key: "identifier", label: "Identifier", value: <span className="flex items-center gap-1.5">
      <span title={kind} className="flex shrink-0 text-base leading-none"><NodeGlyphIcon glyph={nodeGlyph(graph, node)} title={kind} /></span>
      <span className="min-w-0 break-words">{[type, method].filter(Boolean).join(".") || node.label}</span>
    </span> },
    { key: "signature", label: "Signature", value: <Mono>{signature}</Mono>, hidden: !signature },
    { key: "package", label: "Package", value: <Mono>{pkg}</Mono>, hidden: !pkg },
    { key: "module", label: "Module", value: module, hidden: !module },
    { key: "location", label: "Location", value: <Mono>{`${location?.path ?? "unknown file"}:${location?.line ?? "?"}`}</Mono>, hidden: !location },
    { key: "callers", label: "Callers", value: totals.callers, hidden: totals.callers === undefined },
    { key: "callees", label: "Callees", value: totals.callees, hidden: totals.callees === undefined },
    { key: "status", label: "Status", value: nodeStatus(node) },
  ];
  return <section className="space-y-2" aria-label="Node details">
    <h3 className="break-words text-sm font-semibold">{node.label}</h3>
    <KeyValueList items={items} rowClassName="grid-cols-[5.5rem_minmax(0,1fr)] gap-density-2" valueClassName="text-xs" />
    <div className="flex flex-wrap gap-2">
      <Button type="button" size="sm" variant="outline" disabled={!location} onClick={onOpen}>Open declaration</Button>
      <Button type="button" size="sm" variant="outline" disabled={!location || isRoot} onClick={onFocus}>Focus</Button>
    </div>
  </section>;
}

function LegendEntry({ mark, children }: { mark: ReactNode; children: ReactNode }) {
  return <li className="flex items-center gap-1.5"><span className="flex shrink-0 items-center gap-1 text-base leading-none">{mark}</span>{children}</li>;
}

/** The key to the icons: one entry per icon this response draws, then the box and line styles. */
export function Legend({ graph }: { graph: ModuleGraph }) {
  return <ul className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-muted-foreground" aria-label="Legend">
    {usedGlyphs(graph).map((glyph) => <LegendEntry key={glyph} mark={<NodeGlyphIcon glyph={glyph} />}>{NODE_GLYPH_NAME[glyph]}</LegendEntry>)}
    {(graph.groups ?? []).length > 0 && <LegendEntry mark={<PackageIcon />}>package</LegendEntry>}
    <LegendEntry mark={<span className="inline-block h-3 w-5 rounded border border-sky-500/60 bg-sky-500/10" />}>root</LegendEntry>
    <LegendEntry mark={<span className="inline-block h-3 w-5 rounded border border-dashed border-border bg-muted" />}>no source: not expandable</LegendEntry>
    <LegendEntry mark={<CallIcon />}>call</LegendEntry>
    <LegendEntry mark={<span className="inline-block w-6 border-t-2 border-dashed border-muted-foreground" />}>every call site guarded</LegendEntry>
    {graph.edges.some((edge) => edge.type === "dispatch") && <LegendEntry mark={<><span className="inline-block w-6 border-t-2 border-sky-500" /><DispatchIcon /></>}>{DISPATCH_LABEL}</LegendEntry>}
    <LegendEntry mark={<span className="text-xs font-semibold">×N</span>}>call sites on one edge</LegendEntry>
    <LegendEntry mark={<span className="text-xs font-semibold">+N</span>}>more to load</LegendEntry>
    <LegendEntry mark={<UiWarningTriangle />}>not drawn</LegendEntry>
  </ul>;
}
