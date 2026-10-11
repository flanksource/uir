import { useMemo, useState } from "react";
import { Button } from "@flanksource/clicky-ui/components";
import { GraphDiagram } from "@flanksource/clicky-ui/data";
import type { ModuleGraphResult } from "./api";
import { DispatchIcon, NodeGlyphIcon, PackageCaption } from "./call-graph-glyphs";
import { canRevealSites, columnGap, nodeRevealPatch, siteRevealPatch, toDiagram } from "./call-graph-model";
import { EdgeSites, Legend, NodeDetails } from "./CallGraphParts";
import type { Route } from "./route";
import { Muted } from "./ui";

export function QueryGraph({ graph, route, onRoute }: { graph: ModuleGraphResult; route: Route; onRoute: (patch: Partial<Route>) => void }) {
  const [selection, setSelection] = useState<{ kind: "node" | "edge"; id: string }>();
  const diagram = useMemo(() => toDiagram({ graph, visible: graph, direction: "both", options: { grouped: true, guardLabel: "innermost", truncateAt: 48 },
    glyphs: { node: (glyph, words) => <NodeGlyphIcon glyph={glyph} title={words} />, dispatch: () => <DispatchIcon />, group: (caption) => <PackageCaption caption={caption} /> } }), [graph]);
  const node = selection?.kind === "node" ? graph.nodes.find((node) => node.id === selection.id) : undefined;
  const edge = selection?.kind === "edge" ? graph.edges.find((edge) => edge.id === selection.id) : undefined;
  const reveal = (patch: Partial<Route>) => onRoute({ ...patch, view: "explorer" });
  return <section aria-label="Query graph" className="space-y-2">
    {graph.candidates.length > 0 && <Muted>Select a more specific root: {graph.candidates.map((candidate) => candidate.query_name).join(", ")}</Muted>}
    <div className="flex min-h-[28rem] gap-3">
      <GraphDiagram className="min-w-0 flex-1 rounded-lg border border-border" nodes={diagram.nodes.map(({ expandCount: _expandCount, ...node }) => node)}
        edges={diagram.edges} groups={diagram.groups} layout="columns" zoomable focusId={graph.roots[0]} columnGap={columnGap(diagram)} ariaLabel="SQL query graph"
        onNodeSelect={(id: string) => setSelection({ kind: "node", id })} onEdgeSelect={(id: string) => setSelection({ kind: "edge", id })}
        selectedId={node?.id} selectedEdgeId={edge?.id} />
      {(node || edge) && <aside className="w-80 max-w-[45%] space-y-2 rounded-lg border border-border p-3">
        <Button variant="ghost" size="sm" onClick={() => setSelection(undefined)}>Close selection</Button>
        {node && <NodeDetails node={node} graph={graph} isRoot={graph.roots.includes(node.id)} onOpen={() => reveal(nodeRevealPatch(node))} />}
        {edge && <EdgeSites edge={edge} graph={graph} {...(canRevealSites(graph, edge) ? { onReveal: (site) => reveal(siteRevealPatch(graph, edge, site, route.snapshot)) } : {})} />}
      </aside>}
    </div>
    <Legend graph={graph} expandable={false} />
  </section>;
}
