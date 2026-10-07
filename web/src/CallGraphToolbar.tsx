import { Button, DropdownMenu, SegmentedControl, Switch } from "@flanksource/clicky-ui/components";
import { Badge, ServerTimingBadge, type ServerTimingMetric } from "@flanksource/clicky-ui/data";
import { UiFilter, UiHome, UiWarningTriangle } from "@flanksource/clicky-ui/icons";
import type { ModuleGraphDirection, ModuleGraphOmitted } from "./api";
import { EXCLUDE_EXTERNAL, setExternalHidden } from "./call-graph-exclude";
import { excludedTally, omittedParts } from "./call-graph-labels";
import { MAX_GRAPH_DEPTH } from "./call-graph-model";
import { PackageFacets, type ExclusionsProps } from "./CallGraphPackageTree";

const DIRECTIONS: { id: ModuleGraphDirection; label: string }[] = [
  { id: "callers", label: "Callers" },
  { id: "both", label: "Both" },
  { id: "callees", label: "Callees" },
];

function DepthStepper({ depth, onChange }: { depth: number; onChange: (depth: number) => void }) {
  return <div className="flex items-center gap-1" role="group" aria-label="Depth">
    <span className="text-xs text-muted-foreground">Depth</span>
    <Button type="button" variant="outline" size="sm" className="size-8 px-0" aria-label="Decrease depth" disabled={depth <= 1} onClick={() => onChange(depth - 1)}>−</Button>
    <output className="w-5 text-center text-sm font-medium tabular-nums" aria-live="polite" aria-label="Graph depth">{depth}</output>
    <Button type="button" variant="outline" size="sm" className="size-8 px-0" aria-label="Increase depth" disabled={depth >= MAX_GRAPH_DEPTH} onClick={() => onChange(depth + 1)}>+</Button>
  </div>;
}

function OmittedChip({ omitted }: { omitted: ModuleGraphOmitted }) {
  const parts = omittedParts(omitted);
  if (parts.length === 0) return null;
  const tally = excludedTally(omitted).map(({ group, count }) => `${group}: ${count}`);
  const title = [`Not drawn: ${parts.join(", ")}`, ...(tally.length ? ["Excluded per package:", ...tally] : [])].join("\n");
  return <span title={title} data-testid="graph-omitted"><Badge tone="warning" size="md" icon={UiWarningTriangle} clickToCopy={false}>{parts.join(" · ")}</Badge></span>;
}

export interface CallGraphToolbarProps {
  direction: ModuleGraphDirection;
  depth: number;
  pinned: boolean;
  /** A graph is drawn, so its root can be pinned. */
  canPin: boolean;
  exclusions?: ExclusionsProps;
  omitted?: ModuleGraphOmitted;
  timing?: ServerTimingMetric[];
  onDirection: (direction: ModuleGraphDirection) => void;
  onDepth: (depth: number) => void;
  onPin: (pinned: boolean) => void;
  onFit: () => void;
  onReset: () => void;
}

export function CallGraphToolbar(props: CallGraphToolbarProps) {
  const { exclusions } = props;
  const excludedCount = exclusions?.packages.filter((pkg) => pkg.excluded).length ?? 0;
  return <div className="flex flex-wrap items-center gap-3">
    <SegmentedControl value={props.direction} options={DIRECTIONS} onChange={props.onDirection} size="sm" aria-label="Direction" />
    <DepthStepper depth={props.depth} onChange={props.onDepth} />
    <Button type="button" variant={props.pinned ? "secondary" : "outline"} size="sm" disabled={!props.pinned && !props.canPin} aria-pressed={props.pinned}
      title={props.pinned ? "The graph keeps this root while the selection changes" : "Keep this root while the selection changes"} onClick={() => props.onPin(!props.pinned)}>
      {props.pinned ? "Unpin root" : "Pin root"}
    </Button>
    <Button type="button" variant="outline" size="sm" onClick={props.onFit}>Fit</Button>
    <Button type="button" variant="outline" size="sm" onClick={props.onReset}><UiHome />Reset</Button>
    <Switch checked={exclusions?.exclude.includes(EXCLUDE_EXTERNAL) ?? false} disabled={!exclusions || exclusions.busy} label="Hide all external"
      onChange={(hidden) => exclusions?.onExclude(setExternalHidden(exclusions.exclude, hidden))} />
    {exclusions && <DropdownMenu icon={UiFilter} label={`Packages${excludedCount ? ` (${excludedCount} excluded)` : ""}`} variant="outline" size="sm" menuLabel="Package exclusions" title="Choose the packages the graph leaves out">
      {() => <PackageFacets {...exclusions} />}
    </DropdownMenu>}
    <div className="ml-auto flex items-center gap-2">
      {props.omitted && <OmittedChip omitted={props.omitted} />}
      <ServerTimingBadge metrics={props.timing} />
    </div>
  </div>;
}
