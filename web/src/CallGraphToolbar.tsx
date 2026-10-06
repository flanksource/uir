import { useState, type FormEvent } from "react";
import { Button, DropdownMenu, SegmentedControl, Switch } from "@flanksource/clicky-ui/components";
import { Badge, ServerTimingBadge, type ServerTimingMetric } from "@flanksource/clicky-ui/data";
import { UiFilter, UiHome, UiWarningTriangle } from "@flanksource/clicky-ui/icons";
import type { ModuleGraphDirection, ModuleGraphOmitted, ModuleGraphPackage } from "./api";
import {
  addExcludePattern, EXCLUDE_EXTERNAL, EXCLUDE_NONE, excludePackage, excludePatternError, includePackage, removeExcludePattern, setExternalHidden,
} from "./call-graph-exclude";
import { excludedTally, omittedParts } from "./call-graph-labels";
import { MAX_GRAPH_DEPTH } from "./call-graph-model";
import { TextInput } from "./ui";

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

export interface ExclusionsProps {
  /** The effective patterns the server applied. */
  exclude: string[];
  packages: ModuleGraphPackage[];
  onExclude: (patterns: string[]) => void;
  onDefaults: () => void;
  /** A graph for the last edit is loading: the facets describe the one before it, so editing waits. */
  busy: boolean;
}

function PackageRow({ pkg, onToggle }: { pkg: ModuleGraphPackage; onToggle: () => void }) {
  return <li className="flex items-center gap-2 py-1">
    <Switch checked={!pkg.excluded} onChange={onToggle} aria-label={`Show ${pkg.path}`} />
    <span className="min-w-0 flex-1 truncate font-mono text-xs" title={pkg.path}>{pkg.path}</span>
    {pkg.external && <Badge variant="outline" size="sm" clickToCopy={false}>external</Badge>}
    <span className="w-8 shrink-0 text-right text-xs tabular-nums text-muted-foreground" title={`${pkg.nodes} nodes`}>{pkg.nodes}</span>
  </li>;
}

/** The package facets: one switch per package the walk reached, the patterns in force, and free-text patterns. */
function PackageFacets({ exclude, packages, onExclude, onDefaults, busy }: ExclusionsProps) {
  const [filter, setFilter] = useState("");
  const [pattern, setPattern] = useState("");
  const [error, setError] = useState<string>();
  const shown = packages.filter((pkg) => pkg.path.toLowerCase().includes(filter.trim().toLowerCase()));
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const typed = pattern.trim();
    const problem = excludePatternError(exclude, typed);
    setError(problem);
    if (problem) return;
    setPattern("");
    onExclude(addExcludePattern(exclude, typed));
  };
  const patterns = exclude.filter((entry) => entry !== EXCLUDE_NONE);
  // The filter box comes first: the menu focuses its first control on opening, which must not be one that drops an exclusion.
  return <fieldset disabled={busy} aria-busy={busy} className="flex w-96 min-w-0 max-w-[90vw] flex-col gap-2 p-2 text-sm">
    <TextInput aria-label="Filter packages" placeholder="Filter packages" value={filter} onChange={(event) => setFilter(event.target.value)} />
    <div className="flex flex-wrap items-center gap-1" aria-label="Exclusion patterns">
      <span className="text-xs text-muted-foreground">Excluding</span>
      {patterns.length === 0 && <span className="text-xs text-muted-foreground">nothing</span>}
      {patterns.map((entry) => <button key={entry} type="button" className="rounded border border-border px-1.5 font-mono text-xs hover:bg-muted"
        aria-label={`Stop excluding ${entry}`} title={`Stop excluding ${entry}`} onClick={() => onExclude(removeExcludePattern(exclude, entry))}>{entry} ×</button>)}
    </div>
    <ul className="max-h-72 overflow-y-auto pr-1" aria-label="Packages">
      {shown.map((pkg) => <PackageRow key={pkg.path} pkg={pkg}
        onToggle={() => onExclude(pkg.excluded ? includePackage(exclude, pkg, packages) : excludePackage(exclude, pkg.path))} />)}
      {shown.length === 0 && <li className="py-1 text-xs text-muted-foreground">{packages.length ? "No package matches the filter." : "The graph reached no package."}</li>}
    </ul>
    <form className="flex items-start gap-2" onSubmit={submit}>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <TextInput aria-label="Exclude pattern" placeholder="example.com/module/..." value={pattern} onChange={(event) => setPattern(event.target.value)} />
        {error && <span role="alert" className="text-xs text-destructive">{error}</span>}
      </div>
      <Button type="submit" size="sm" variant="outline">Exclude</Button>
    </form>
    <div className="flex justify-between gap-2 border-t border-border pt-2">
      <Button type="button" size="sm" variant="ghost" onClick={onDefaults}>Reset to defaults</Button>
      <Button type="button" size="sm" variant="ghost" onClick={() => onExclude([])}>Show all</Button>
    </div>
  </fieldset>;
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
