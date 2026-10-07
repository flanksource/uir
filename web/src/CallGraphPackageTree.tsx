import { useMemo, useState, type FormEvent } from "react";
import { Button } from "@flanksource/clicky-ui/components";
import { Badge, TreeNode } from "@flanksource/clicky-ui/data";
import type { ModuleGraphPackage } from "./api";
import {
  addExcludePattern, EXCLUDE_NONE, excludeGroup, excludePackage, excludePatternError, includePackages, isStdPackage, removeExcludePattern,
} from "./call-graph-exclude";
import { buildPackageTree, type PackageTreeNode } from "./call-graph-package-tree";
import { filterItems } from "./tree-filter";
import { TextInput } from "./ui";

export interface ExclusionsProps {
  /** The effective patterns the server applied. */
  exclude: string[];
  packages: ModuleGraphPackage[];
  onExclude: (patterns: string[]) => void;
  onDefaults: () => void;
  /** A graph for the last edit is loading: the facets describe the one before it, so editing waits. */
  busy: boolean;
}

/** Unchecking excludes the node's pattern; checking a hidden or mixed node shows its whole subtree. */
function togglePackages(node: PackageTreeNode, exclude: string[], packages: ModuleGraphPackage[]): string[] {
  if (node.state !== "shown") return includePackages(exclude, node.packages, packages);
  return node.kind === "package" ? excludePackage(exclude, node.path) : excludeGroup(exclude, node.pattern, packages);
}

function PackageCheckbox({ node, onToggle }: { node: PackageTreeNode; onToggle: () => void }) {
  const mixed = node.state === "mixed";
  // indeterminate is a DOM property with no attribute, so it is set on every render; the clicks stay off the row, which expands and collapses.
  return <input type="checkbox" className="shrink-0" checked={node.state === "shown"} ref={(input) => { if (input) input.indeterminate = mixed; }}
    aria-checked={mixed ? "mixed" : undefined} aria-label={`Show ${node.kind === "std" ? "standard library" : node.pattern}`}
    onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()} onChange={onToggle} />;
}

/** The package facets: a checkbox tree of the packages the walk reached, the patterns in force, and free-text patterns. */
export function PackageFacets({ exclude, packages, onExclude, onDefaults, busy }: ExclusionsProps) {
  const [filter, setFilter] = useState("");
  const [pattern, setPattern] = useState("");
  const [error, setError] = useState<string>();
  const tree = useMemo(() => buildPackageTree(packages), [packages]);
  const roots = filterItems(tree, filter, (node) => node.path || node.label);
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
  // TreeNode rather than Tree: Tree adds a filter box of its own past 20 edges, beside this one.
  return <fieldset disabled={busy} aria-busy={busy} className="flex w-96 min-w-0 max-w-[90vw] flex-col gap-2 p-2 text-sm">
    <TextInput aria-label="Filter packages" placeholder="Filter packages" value={filter} onChange={(event) => setFilter(event.target.value)} />
    <div className="flex flex-wrap items-center gap-1" aria-label="Exclusion patterns">
      <span className="text-xs text-muted-foreground">Excluding</span>
      {patterns.length === 0 && <span className="text-xs text-muted-foreground">nothing</span>}
      {patterns.map((entry) => <button key={entry} type="button" className="rounded border border-border px-1.5 font-mono text-xs hover:bg-muted"
        aria-label={`Stop excluding ${entry}`} title={`Stop excluding ${entry}`} onClick={() => onExclude(removeExcludePattern(exclude, entry))}>{entry} ×</button>)}
    </div>
    <div role="tree" aria-label="Packages" className="max-h-72 overflow-y-auto pr-1">
      {roots.map((root) => <TreeNode<PackageTreeNode> key={`${filter ? "filtered" : "all"}:${root.id}`} node={root} getKey={(node) => node.id} getChildren={(node) => node.children}
        getAriaLabel={(node) => node.path || node.label} defaultOpen={(_, depth) => Boolean(filter) || depth < 1}
        renderRow={({ node }) => <>
          <PackageCheckbox node={node} onToggle={() => onExclude(togglePackages(node, exclude, packages))} />
          <span className="min-w-0 flex-1 truncate font-mono text-xs" title={node.path || node.label}>{node.label}</span>
          {node.external && !node.packages.every(isStdPackage) && <Badge variant="outline" size="sm" clickToCopy={false}>external</Badge>}
          <span className="w-8 shrink-0 text-right text-xs tabular-nums text-muted-foreground" title={`${node.nodes} nodes`}>{node.nodes}</span>
        </>} />)}
      {roots.length === 0 && <div className="py-1 text-xs text-muted-foreground">{packages.length ? "No package matches the filter." : "The graph reached no package."}</div>}
    </div>
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
