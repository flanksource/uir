// The Explorer's Call graph tab: the graph around the pinned root or the selected function, fetched
// from GET /api/v1/modules/graph, with `+N` expansions merged in, the call sites of a selected edge
// and the declaration of a selected node opened in the Source tab.
import { useMemo, useState, type ReactNode } from "react";
import { Button } from "@flanksource/clicky-ui/components";
import { GraphDiagram } from "@flanksource/clicky-ui/data";
import { errorMessage, getModuleGraph, type ModuleGraphOptions, type ModuleGraphResult, type ModuleNode } from "./api";
import { formatExclude, parseExclude } from "./call-graph-exclude";
import { DispatchIcon, NodeGlyphIcon, PackageCaption } from "./call-graph-glyphs";
import { DISPATCH_LABEL } from "./call-graph-labels";
import {
  canRevealSites, columnGap, expansionDirection, graphRoot, mergeGraph, nodeRevealPatch, requireNode, siteRevealPatch, toDiagram, visibleGraph,
  type DiagramGlyphs, type DiagramOptions,
} from "./call-graph-model";
import { EdgeSites, Legend, NodeDetails } from "./CallGraphParts";
import { CallGraphToolbar } from "./CallGraphToolbar";
import { queryScope } from "./query-model";
import type { Route } from "./route";
import { ErrorMessage, Muted } from "./ui";
import { useTimedLoad } from "./use-load";

type OnRoute = (patch: Partial<Route>, replace?: boolean) => void;
type Held = { key: string; graph: ModuleGraphResult; revealed: string[] };
type Selection = { key: string; kind: "node" | "edge"; id: string };

const NODE_WIDTH = 188;
const NODE_HEIGHT = 32;
const ROW_GAP = 14;
/** Node labels are 12px: at this scale they are 7.2px, the smallest that still reads. */
const READABLE_SCALE = 0.6;
const OPTIONS: DiagramOptions = { guardLabel: "innermost", truncateAt: 32, grouped: true };
const GLYPHS: DiagramGlyphs = {
  node: (glyph, words) => <NodeGlyphIcon glyph={glyph} title={words} />,
  dispatch: () => <DispatchIcon title={DISPATCH_LABEL} />,
  group: (caption) => <PackageCaption caption={caption} />,
};

function Notice({ children }: { children: ReactNode }) {
  return <div className="flex h-full items-center justify-center p-6 text-center"><Muted>{children}</Muted></div>;
}

/** The symbols an ambiguous selector matched; choosing one pins it as the root. */
function Candidates({ graph, onRoute }: { graph: ModuleGraphResult; onRoute: OnRoute }) {
  return <section aria-label="Candidates" className="flex flex-col gap-1 p-3">
    <Muted>{graph.candidates.length} symbols match the selection. Choose the root:</Muted>
    {graph.candidates.map((candidate) => <Button key={candidate.id} type="button" size="sm" variant="outline" className="justify-start"
      onClick={() => onRoute({ graphRoot: candidate.id })}>{candidate.query_name} <Muted>{candidate.package_path}</Muted></Button>)}
  </section>;
}

function Stages({ graph }: { graph: ModuleGraphResult }) {
  return <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-label="Resolution stages">
    {graph.stages.map((stage) => <li key={stage.name}><span className="font-medium text-foreground">{stage.name}</span> {stage.value}</li>)}
    {graph.warnings.map((warning) => <li key={`${warning.root_key}:${warning.location}`} className="text-amber-600 dark:text-amber-400">{warning.root_key}: {warning.message}</li>)}
  </ul>;
}

export function CallGraphPane({ route, selectedNode, onRoute }: { route: Route; selectedNode?: ModuleNode; onRoute: OnRoute }) {
  const root = useMemo(() => {
    try { return graphRoot(route.graphRoot, selectedNode); } catch (error) { return { error: `Cannot draw the call graph of ${selectedNode?.symbol}: ${errorMessage(error)}` }; }
  }, [route.graphRoot, selectedNode]);
  const scope = queryScope(route);
  const exclude = parseExclude(route.graphExclude);
  const request: ModuleGraphOptions | null = scope && ("symbol" in root || "selector" in root)
    ? { ...root, direction: route.graphDir, depth: route.graphDepth, exclude, root: scope.root, snapshot: scope.snapshot } : null;
  const key = `graph:${JSON.stringify(request)}`;
  const load = useTimedLoad(request ? () => getModuleGraph(request) : null, key);

  const [expanded, setExpanded] = useState<Held>();
  const [last, setLast] = useState<Held>();
  const [selection, setSelection] = useState<Selection>();
  const [expanding, setExpanding] = useState<{ id: string; error?: string }>();
  const [opening, setOpening] = useState({ nonce: 0, fit: false });
  const fresh = useMemo<Held | undefined>(() => load.data && (expanded?.key === key ? expanded : { key, graph: load.data, revealed: [] }), [load.data, expanded, key]);
  if (fresh && fresh !== last) setLast(fresh);
  const shown = fresh ?? last;

  const visible = useMemo(() => shown && visibleGraph(shown.graph, { direction: route.graphDir, depth: route.graphDepth, revealed: shown.revealed }), [shown, route.graphDir, route.graphDepth]);
  const diagram = useMemo(() => shown && visible && toDiagram({ graph: shown.graph, visible, direction: route.graphDir, options: OPTIONS, glyphs: GLYPHS }), [shown, visible, route.graphDir]);
  const rootId = shown?.graph.roots[0];
  const current = selection?.key === shown?.key ? selection : undefined;
  const selectedGraphNode = current?.kind === "node" ? visible?.nodes.find((node) => node.id === current.id) : undefined;
  const selectedEdge = current?.kind === "edge" ? visible?.edges.find((edge) => edge.id === current.id) : undefined;
  const effectiveExclude = exclude ?? shown?.graph.exclude;

  // A `+N` loads one more hop on the node's outer side and merges it into the graph this request drew.
  const expand = async (drawn: Held, drawnRequest: ModuleGraphOptions, id: string) => {
    setExpanding({ id });
    try {
      const { data } = await getModuleGraph({ symbol: id, depth: 1, direction: expansionDirection(requireNode(drawn.graph, id)), exclude, root: drawnRequest.root, snapshot: drawnRequest.snapshot });
      setExpanded((held) => {
        const base = held?.key === drawn.key ? held : drawn;
        return { key: drawn.key, graph: mergeGraph(base.graph, data), revealed: [...base.revealed, ...data.nodes.map((node) => node.id)] };
      });
      setExpanding(undefined);
    } catch (error) {
      setExpanding({ id, error: `Cannot expand ${requireNode(drawn.graph, id).label}: ${errorMessage(error)}` });
    }
  };

  const toolbar = <CallGraphToolbar direction={route.graphDir} depth={route.graphDepth} pinned={Boolean(route.graphRoot)} canPin={Boolean(rootId)}
    omitted={shown?.graph.omitted} timing={load.timing}
    exclusions={shown && effectiveExclude ? { exclude: effectiveExclude, packages: shown.graph.packages, busy: load.loading,
      onExclude: (patterns) => onRoute({ graphExclude: formatExclude(patterns) }, true), onDefaults: () => onRoute({ graphExclude: "" }, true) } : undefined}
    onDirection={(graphDir) => onRoute({ graphDir }, true)} onDepth={(graphDepth) => onRoute({ graphDepth }, true)}
    onPin={(pinned) => onRoute({ graphRoot: pinned && rootId ? rootId : "" })}
    onFit={() => setOpening({ nonce: opening.nonce + 1, fit: true })} onReset={() => setOpening({ nonce: opening.nonce + 1, fit: false })} />;

  if ("empty" in root) return <div className="flex h-full min-h-0 flex-col gap-2 p-2">{toolbar}<Notice>{root.empty}</Notice></div>;
  return <div className="flex h-full min-h-0 flex-col gap-2 p-2" aria-busy={load.loading}>
    {toolbar}
    {"error" in root && <ErrorMessage error={root.error} />}
    <ErrorMessage error={load.error} />
    {shown && <Stages graph={shown.graph} />}
    {!shown && load.loading && <Notice>Loading call graph…</Notice>}
    {shown && shown.graph.candidates.length > 0 && <Candidates graph={shown.graph} onRoute={onRoute} />}
    {shown && !rootId && shown.graph.candidates.length === 0 && <Notice>The selection resolved to no indexed function or method in this scope.</Notice>}
    {shown && rootId && diagram && <div className="flex min-h-0 flex-1 flex-col gap-3 lg:flex-row">
      <div className={`relative min-h-64 min-w-0 flex-1 rounded-lg border border-border bg-background ${load.loading ? "opacity-60" : ""}`}>
        <GraphDiagram key={`${shown.key}:${opening.nonce}`} nodes={diagram.nodes} edges={diagram.edges} groups={diagram.groups} layout="columns" zoomable edgeFocus="auto"
          focusId={rootId} {...(opening.fit ? {} : { fitMinScale: READABLE_SCALE })} nodeWidth={NODE_WIDTH} nodeHeight={NODE_HEIGHT} columnGap={columnGap(diagram)} rowGap={ROW_GAP}
          className="h-full" ariaLabel={`Call graph of ${requireNode(shown.graph, rootId).label}`}
          onNodeSelect={(id: string) => setSelection({ key: shown.key, kind: "node", id })} onEdgeSelect={(id: string) => setSelection({ key: shown.key, kind: "edge", id })}
          {...(fresh && request ? { onNodeExpand: (id: string) => void expand(fresh, request, id) } : {})} {...(selectedGraphNode ? { selectedId: selectedGraphNode.id } : {})} {...(selectedEdge ? { selectedEdgeId: selectedEdge.id } : {})} />
      </div>
      <aside className="w-full shrink-0 overflow-y-auto rounded-lg border border-border bg-card p-3 lg:w-80" aria-label="Selection">
        {expanding && <p role="status" className="mb-2 text-xs text-muted-foreground">{expanding.error ?? `Loading more around ${requireNode(shown.graph, expanding.id).label}…`}</p>}
        {selectedEdge ? <EdgeSites edge={selectedEdge} graph={shown.graph}
          {...(canRevealSites(shown.graph, selectedEdge) ? { onReveal: (site) => onRoute(siteRevealPatch(shown.graph, selectedEdge, site, route.snapshot)) } : {})} />
          : selectedGraphNode ? <NodeDetails node={selectedGraphNode} graph={shown.graph} isRoot={selectedGraphNode.id === rootId}
            onOpen={() => onRoute(nodeRevealPatch(selectedGraphNode))} onFocus={() => onRoute({ graphRoot: selectedGraphNode.id })} />
          : <Muted>Select an edge to list its call sites, or a node to see where it is declared.</Muted>}
      </aside>
    </div>}
    {shown && rootId && <div className="shrink-0"><Legend graph={shown.graph} /></div>}
  </div>;
}
