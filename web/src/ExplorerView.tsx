import { useEffect, useMemo, useRef, useState } from "react";
import Editor, { type OnMount } from "@monaco-editor/react";
import { Button, DropdownMenu, Select, Tabs, Workspace, type WorkspacePaneSpec } from "@flanksource/clicky-ui/components";
import { ServerTimingBadge, Tree } from "@flanksource/clicky-ui/data";
import { UiDotsVertical, UiFolder, UiListTree } from "@flanksource/clicky-ui/icons";
import { MonacoProvider } from "@flanksource/clicky-ui/monaco";
import { browseModule, readModuleSource, type ItemsWithWarnings, type ModuleBrowse, type ModuleHead, type ModuleLocation, type ModuleNode, type ModuleRoot, type ModuleSource, type ModuleSourceContent } from "./api";
import { CallGraphPane } from "./CallGraphPane";
import { MissingHeadWarnings } from "./CoverageWarning";
import { ExplorerDependencies } from "./ExplorerDependencies";
import { ExplorerRefactorDialog } from "./ExplorerRefactorDialog";
import { moduleHeadTree, packageSymbolTree, scopedHeads, symbolModuleHeads, symbolTree, type HeadFileItem, type PackageSymbolItem, type SymbolItem } from "./explorer-model";
import { fileSelectionPatch, symbolSelectionPatch } from "./explorer-navigation";
import { FileTypeIcon, FolderTypeIcon } from "./file-icons";
import { getMonacoWorker } from "./monaco-workers";
import type { Route } from "./route";
import { SymbolDetails } from "./SymbolDetails";
import { cycleSymbolFilter, filterPackageSymbols, symbolFilterKeys, symbolFilterState } from "./symbol-filters";
import { SymbolIcon } from "./symbol-icons";
import { filterItems } from "./tree-filter";
import { ErrorMessage, Field, Muted, TextInput } from "./ui";
import { useLoad, type Load, type TimedLoad } from "./use-load";

type OutlineItem = SymbolItem | { id: string; label: string; children: SymbolItem[]; source: ModuleSource };
type ModuleSymbolItem = { id: string; label: string; kind: "module"; rootKey: string; location: string; snapshot: string; children: PackageSymbolItem[] };
type NavigationSymbolItem = ModuleSymbolItem | PackageSymbolItem;
type EditorInstance = Parameters<OnMount>[0];
const EMPTY_SOURCES: ModuleSource[] = [];
const EMPTY_NODES: ModuleNode[] = [];
const EMPTY_HEADS: ModuleHead[] = [];
function findItem<T extends { id: string; children: T[] }>(items: T[], id: string): T | undefined {
  for (const item of items) {
    if (item.id === id) return item;
    const child = findItem(item.children, id);
    if (child) return child;
  }
}

function revealPosition(editor: EditorInstance, line: number, column: number) {
  if (line < 1) return;
  editor.setPosition({ lineNumber: line, column: Math.max(1, column) });
  editor.revealLineInCenter(line);
}

// SourcePane stays mounted while another center tab shows, hidden with display:none. A hidden editor
// has no size, so the reveal waits until the pane is active and lays the editor out first.
function SourcePane({ snapshot, source, line, column, active }: { snapshot: string; source?: ModuleSource; line: number; column: number; active: boolean }) {
  const content = useLoad<ModuleSourceContent>(source ? () => readModuleSource(snapshot, source.path) : null, `${snapshot}:${source?.id}`);
  const editor = useRef<EditorInstance | null>(null);
  useEffect(() => {
    if (!active || !editor.current || !content.data) return;
    editor.current.layout();
    revealPosition(editor.current, line, column);
  }, [active, content.data, line, column]);

  if (!source) return <div className="p-3"><Muted>Select a file to view its verified content.</Muted></div>;
  return <div className="flex h-full min-h-0 flex-col">
    <div className="shrink-0 border-b border-border px-3 py-2 text-xs">
      <div className="truncate font-medium" title={source.path}>{source.path}</div>
      <Muted>{source.package_path} · SHA-256 {source.content_hash.slice(0, 12)}</Muted>
      {content.data && <div><Muted>{content.data.origin === "git" ? `Pinned Git revision ${content.data.revision}` : content.data.origin === "snapshot" ? "Stored snapshot source" : "Local file matches indexed hash"}</Muted></div>}
    </div>
    {content.loading && <div className="p-3"><Muted>Loading source…</Muted></div>}
    {content.error && <div className="p-3"><ErrorMessage error={content.error} /></div>}
    {content.data && <div className="min-h-0 flex-1">
      <MonacoProvider getWorker={getMonacoWorker}>
        <Editor value={content.data.content} language="go" path={`file:///uir/${snapshot}/${source.path}`} height="100%" options={{ readOnly: true, automaticLayout: true, minimap: { enabled: false } }}
          onMount={(instance) => { editor.current = instance; if (active) revealPosition(instance, line, column); }} />
      </MonacoProvider>
    </div>}
  </div>;
}

const CENTER_TABS = [{ id: "source", label: "Source" }, { id: "call-graph", label: "Call graph" }];

// CenterTabs holds the Workspace's one center pane: Source and Call graph share it, and the source
// editor stays mounted behind the graph so returning to it keeps its state.
function CenterTabs({ route, source, onRoute, children }: { route: Route; source: React.ReactNode; onRoute: (patch: Partial<Route>, replace?: boolean) => void; children: React.ReactNode }) {
  return <div className="flex h-full min-h-0 flex-col">
    <Tabs className="shrink-0 px-2" tabs={CENTER_TABS} value={route.explorerTab} onChange={(tab) => onRoute({ explorerTab: tab === "call-graph" ? "call-graph" : "source" })} />
    <div className={route.explorerTab === "source" ? "min-h-0 flex-1" : "hidden"}>{source}</div>
    {route.explorerTab === "call-graph" && <div className="min-h-0 flex-1">{children}</div>}
  </div>;
}

function FilePane({ files, selected, heads, route, onRoute }: { files: HeadFileItem[]; selected?: HeadFileItem; heads: Load<ItemsWithWarnings<ModuleHead>>; route: Route; onRoute: (patch: Partial<Route>, replace?: boolean) => void }) {
  const locallySelectedSource = useRef("");
  const [revealVersion, setRevealVersion] = useState(0);
  useEffect(() => {
    if (!route.source) return;
    if (route.source === locallySelectedSource.current) {
      locallySelectedSource.current = "";
      return;
    }
    setRevealVersion((version) => version + 1);
  }, [route.source]);

  return <div className="flex h-full flex-col"><div className="shrink-0 p-2"><Field label="Search files"><TextInput value={route.fileSearch} onChange={(event) => onRoute({ fileSearch: event.target.value }, true)} /></Field></div>
    {heads.loading && <div className="px-3 text-sm text-muted-foreground">Loading module heads…</div>}
    {heads.error && <div className="px-3"><ErrorMessage error={heads.error} /></div>}
    <Tree<HeadFileItem> key={revealVersion} className="min-h-0 flex-1" ariaLabel="Module heads and source files" roots={filterItems(files, route.fileSearch, (item) => item.path)} getChildren={(item) => item.children} getKey={(item) => item.id}
      selected={selected} defaultOpen={(item, depth) => Boolean(route.fileSearch) || depth < 2 ||
        Boolean(selected && item.kind === "folder" && item.head === selected.head && selected.path.startsWith(`${item.path}/`))}
      onSelect={(item) => {
        const patch = fileSelectionPatch(item);
        if (patch) {
          locallySelectedSource.current = patch.source;
          onRoute(patch);
        }
      }}
      renderRow={({ node: item, open }) => <>{item.kind === "file" ? <FileTypeIcon filename={item.label} /> : <FolderTypeIcon open={open} root={item.kind === "module"} />}
        <span className="truncate" title={item.path}>{item.label}</span></>}
      empty={<div className="p-3 text-sm text-muted-foreground">{route.fileSearch ? "No matching files." : "No indexed module heads."}</div>} />
  </div>;
}

function OutlinePane({ items, selected, route, onRoute }: { items: OutlineItem[]; selected?: OutlineItem; route: Route; onRoute: (patch: Partial<Route>, replace?: boolean) => void }) {
  return <div className="flex h-full flex-col"><div className="shrink-0 p-2"><Field label="Search symbols"><TextInput value={route.symbolSearch} onChange={(event) => onRoute({ symbolSearch: event.target.value }, true)} /></Field></div><Tree<OutlineItem> className="min-h-0 flex-1" ariaLabel="Symbols" roots={filterItems(items, route.symbolSearch, (item) => "node" in item ? `${item.label} ${item.node.symbol}` : "")} getChildren={(item) => item.children} getKey={(item) => item.id}
    selected={selected} revealSelected defaultOpen={() => Boolean(route.symbolSearch)}
    onSelect={(item) => { if ("node" in item) onRoute({ source: item.node.source_id, node: item.node.id, fileSearch: "", symbolSearch: "" }); }}
    renderRow={({ node: item }) => <>{"node" in item ? <SymbolIcon nodeType={item.node.node_type} /> : <FileTypeIcon filename={item.source.path} />}<span className="truncate" title={"node" in item ? item.node.symbol : item.source.path}>{item.label}</span>{"node" in item && item.node.line && <span className="ml-auto text-xs text-muted-foreground">{item.node.line}</span>}</>}
    empty={<div className="p-3 text-sm text-muted-foreground">No indexed symbols for this file.</div>} /></div>;
}

function SymbolFilterMenu({ route, kinds, onRoute }: { route: Route; kinds: string[]; onRoute: (patch: Partial<Route>, replace?: boolean) => void }) {
  const active = Boolean(route.symbolVisibility || route.symbolKinds);
  const groups = [
    { title: "Visibility", key: "symbolVisibility" as const, choices: [["exported", "Public"], ["internal", "Private"]] },
    { title: "Kind", key: "symbolKinds" as const, choices: kinds.map((kind) => [kind, ({ func: "Function", type: "Type", method: "Method", field: "Field" } as Record<string, string>)[kind] ?? kind]) },
  ];
  return <DropdownMenu icon={UiDotsVertical} label={<span className="sr-only">Filter symbols</span>} hideChevron variant={active ? "outline" : "ghost"} size="icon"
    title="Filter symbols" menuLabel="Filter symbols" align="right"
    footer={<button type="button" disabled={!active} onClick={() => onRoute({ symbolVisibility: "", symbolKinds: "" }, true)}
      className="w-full rounded px-2 py-1 text-left text-xs text-foreground hover:bg-accent disabled:opacity-50">Reset filters</button>}>
    {() => <div className="min-w-48 p-1">{groups.map((group) => <div key={group.key} className="py-1">
      <div className="px-2 py-1 text-xs font-medium text-muted-foreground">{group.title}</div>
      {group.choices.map(([value, label]) => {
        const state = symbolFilterState(route[group.key], value);
        return <button key={value} type="button" role="menuitem" aria-label={`${label}: ${state}; set ${state === "neutral" ? "include" : state === "include" ? "exclude" : "neutral"}`}
          onClick={() => onRoute({ [group.key]: cycleSymbolFilter(route[group.key], value) }, true)}
          className="flex w-full items-center justify-between gap-4 rounded px-2 py-1 text-left text-xs text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring">
          <span>{label}</span><span className={state === "neutral" ? "text-muted-foreground" : state === "include" ? "text-primary" : "text-destructive"}>{state}</span>
        </button>;
      })}
    </div>)}</div>}
  </DropdownMenu>;
}

function SymbolModulePane({ roots, route, browse, onRoute }: { roots: Load<ModuleRoot[]>; route: Route; browse: TimedLoad<ModuleBrowse>; onRoute: (patch: Partial<Route>, replace?: boolean) => void }) {
  const [loaded, setLoaded] = useState<Map<string, PackageSymbolItem[]>>(new Map());
  const modules = useMemo<ModuleSymbolItem[]>(() => symbolModuleHeads(roots.data ?? [], route).map((head) => {
    const children = head.snapshot === route.snapshot && browse.data ? packageSymbolTree(head.rootKey, browse.data) : loaded.get(head.snapshot) ?? [];
    return { id: JSON.stringify(["symbol-module", head.rootKey, head.location, head.snapshot]), label: head.name,
      kind: "module", rootKey: head.rootKey, location: head.location, snapshot: head.snapshot, children };
  }), [roots.data, route.module, route.location, route.snapshot, browse.data, loaded]);
  const kinds = useMemo(() => {
    const found = new Set(["type", "func", "method", "field", ...symbolFilterKeys(route.symbolKinds)]);
    function visit(items: PackageSymbolItem[]) {
      for (const item of items) {
        if (item.kind === "symbol") found.add(item.node.kind);
        visit(item.children);
      }
    }
    modules.forEach((module) => visit(module.children));
    return [...found].sort();
  }, [modules, route.symbolKinds]);
  const filteredModules = useMemo(() => modules.flatMap((module) => {
    if (!route.symbolVisibility && !route.symbolKinds) return [module];
    const children = filterPackageSymbols(module.children, route.symbolVisibility, route.symbolKinds);
    const loadedModule = loaded.has(module.snapshot) || (module.snapshot === route.snapshot && Boolean(browse.data));
    return children.length || !loadedModule ? [{ ...module, children }] : [];
  }), [modules, loaded, browse.data, route.snapshot, route.symbolVisibility, route.symbolKinds]);
  const selected = findItem<NavigationSymbolItem>(filteredModules, route.node);
  return <div className="flex h-full flex-col">
    {roots.loading && <div className="px-3 text-sm text-muted-foreground">Loading modules…</div>}
    {roots.error && <div className="px-3"><ErrorMessage error={roots.error} /></div>}
    <Tree<NavigationSymbolItem> key={route.snapshot || "all"} className="min-h-0 flex-1" ariaLabel="Modules, packages, and symbols" roots={filteredModules}
      showSearch searchQuery={route.symbolSearch} onSearchQueryChange={(value) => onRoute({ symbolSearch: value }, true)}
      getSearchText={(item) => item.kind === "module" ? `${item.label} ${item.rootKey}` : item.kind === "package" ? `${item.label} ${item.packagePath}` : item.label}
      actions={<SymbolFilterMenu route={route} kinds={kinds} onRoute={onRoute} />}
      getChildren={(item) => item.children} getKey={(item) => item.kind === "module" ? `${item.id}:${route.symbolVisibility}:${route.symbolKinds}` : item.id} selected={selected} revealSelected
      defaultOpen={(item) => item.kind === "module" && (item.rootKey === route.module || item.snapshot === route.snapshot || Boolean((route.symbolVisibility || route.symbolKinds) && item.children.length))}
      hasMoreChildren={(item) => item.kind === "module" && Boolean(item.snapshot) && !loaded.has(item.snapshot) && !(item.snapshot === route.snapshot && browse.data)}
      loadChildren={async (item) => {
        if (item.kind !== "module") throw new Error(`Cannot load children for ${item.kind}`);
        const existing = loaded.get(item.snapshot);
        if (existing) return existing;
        const result = await browseModule(item.snapshot);
        const children = packageSymbolTree(item.rootKey, result.data);
        setLoaded((current) => new Map(current).set(item.snapshot, children));
        return filterPackageSymbols(children, route.symbolVisibility, route.symbolKinds);
      }}
      onSelect={(item) => {
        if (item.kind !== "symbol") return;
        const source = filteredModules.find((module) => module.children.some((pkg) => findItem([pkg], item.id)));
        if (!source) throw new Error(`Symbol ${item.id} has no module in the navigation tree`);
        onRoute({ ...symbolSelectionPatch(source, item.node), symbolSearch: route.symbolSearch });
      }}
      renderRow={({ node: item, open, loading, error }) => <>
        {item.kind === "module" ? <FolderTypeIcon open={open} root /> : <SymbolIcon nodeType={item.kind === "package" ? "package" : item.node.node_type} />}
        <span className="truncate" title={item.kind === "module" ? item.rootKey : item.kind === "package" ? item.packagePath : `${item.node.path}:${item.node.line}`}>{item.label}</span>
        {item.kind === "symbol" && <span className="ml-auto text-xs text-muted-foreground">{item.node.line}</span>}
        {item.kind === "module" && !item.snapshot && <span className="ml-auto text-xs text-muted-foreground">No head</span>}
        {loading && <span className="ml-auto text-xs text-muted-foreground">Loading…</span>}
        {error != null && <span className="ml-auto text-xs text-destructive" title={String(error)}>Load failed; reopen to retry</span>}
      </>}
      empty={<div className="p-3 text-sm text-muted-foreground">{route.symbolVisibility || route.symbolKinds ? "No matching symbols." : "No indexed modules."}</div>} />
  </div>;
}

export function ExplorerView({ route, roots, heads, browse, locations, onRoute, onRefresh }: { route: Route; roots: Load<ModuleRoot[]>; heads: Load<ItemsWithWarnings<ModuleHead>>; browse: TimedLoad<ModuleBrowse>; locations: Load<ModuleLocation[]>; onRoute: (patch: Partial<Route>, replace?: boolean) => void; onRefresh: () => void }) {
  const [refactorAction, setRefactorAction] = useState<"rename" | "move" | null>(null);
  const sources = browse.data?.sources ?? EMPTY_SOURCES;
  const nodes = browse.data?.nodes ?? EMPTY_NODES;
  const selectedNode = nodes.find((node) => node.id === route.node);
  const selectedSource = sources.find((source) => source.id === route.source);
  const currentHead = heads.data?.items.find((head) => head.root_key === selectedSource?.root_key && head.location === selectedSource.location);
  const canRefactor = Boolean(selectedSource && (!route.node || selectedNode?.source_id === selectedSource.id) &&
    (!selectedNode || selectedNode.identifier.field || selectedNode.identifier.method || selectedNode.identifier.type) &&
    currentHead?.snapshot_id === route.snapshot);
  const canMove = !selectedNode?.identifier.field;
  useEffect(() => setRefactorAction(null), [route.snapshot, route.source, route.node]);
  const files = useMemo(() => {
    const selected = [...scopedHeads(heads.data?.items ?? EMPTY_HEADS, route.module)];
    if (browse.data && route.module && route.location && route.snapshot && !selected.some((head) => head.snapshot_id === route.snapshot)) {
      const currentIndex = selected.findIndex((head) => head.root_key === route.module && head.location === route.location);
      const historical = { root_key: route.module, name: currentIndex >= 0 ? selected[currentIndex].name : route.module.split("/").at(-1) ?? route.module,
        location: route.location, snapshot_id: route.snapshot, sources: browse.data.sources };
      if (currentIndex >= 0) selected[currentIndex] = historical;
      else selected.push(historical);
    }
    return moduleHeadTree(selected);
  }, [browse.data, heads.data, route.location, route.module, route.snapshot]);
  const selectedFile = selectedSource && findItem(files, JSON.stringify(["file", selectedSource.root_key, route.location, route.snapshot, selectedSource.path]));
  const items = useMemo<OutlineItem[]>(() => {
    if (!route.symbolSearch) return symbolTree(nodes.filter((node) => node.source_id === route.source), nodes);
    const bySource = new Map<string, ModuleNode[]>();
    nodes.forEach((node) => bySource.set(node.source_id, [...(bySource.get(node.source_id) ?? []), node]));
    return sources.flatMap((source) => {
      const children = symbolTree(bySource.get(source.id) ?? EMPTY_NODES, nodes);
      return children.length ? [{ id: `source:${source.id}`, label: source.path, source, children }] : [];
    });
  }, [nodes, route.source, route.symbolSearch, sources]);
  const selectedSymbol = findItem(items, route.node);
  const revealed = route.line ? route : selectedNode && selectedNode.source_id === route.source ? { line: selectedNode.line ?? 0, column: selectedNode.column ?? 1 } : { line: 0, column: 0 };

  useEffect(() => {
    if (!browse.data) return;
    if (route.node && selectedNode && !route.source) onRoute({ source: selectedNode.source_id, fileSearch: "" }, true);
    else if (!route.source && !route.node && !route.fileSearch && sources.length) onRoute({ source: sources[0].id }, true);
  }, [browse.data, onRoute, route.fileSearch, route.node, route.source, selectedNode, sources]);

  const navigationPanes: WorkspacePaneSpec[] = route.explorerMode === "files" ? [
    { id: "files", label: "Files", icon: <UiFolder />, location: "left", width: 300, height: 360,
      content: <FilePane files={files} selected={selectedFile} heads={heads} route={route} onRoute={onRoute} />, contentClassName: "overflow-hidden" },
    { id: "outline", label: "Symbols", icon: <UiListTree />, location: "left", width: 300, height: 240,
      content: <OutlinePane items={items} selected={selectedSymbol} route={route} onRoute={onRoute} />, contentClassName: "overflow-hidden" },
  ] : [
    { id: "module-symbols", label: "Symbols", icon: <UiListTree />, location: "left", width: 300,
      content: <SymbolModulePane roots={roots} route={route} browse={browse} onRoute={onRoute} />, contentClassName: "overflow-hidden" },
  ];
  const panes: WorkspacePaneSpec[] = [
    ...navigationPanes,
    { id: "source", label: selectedSource?.path ?? "Source", icon: <FileTypeIcon filename={selectedSource?.path ?? ""} />, location: "center", collapsible: false,
      content: <CenterTabs route={route} onRoute={onRoute} source={<SourcePane snapshot={route.snapshot} source={selectedSource} line={revealed.line} column={revealed.column} active={route.explorerTab === "source"} />}>
        <CallGraphPane route={route} selectedNode={selectedNode} onRoute={onRoute} />
      </CenterTabs>, contentClassName: "overflow-hidden" },
    { id: "details", label: "Details", icon: <UiListTree />, location: "right", width: 320,
      content: <SymbolDetails node={selectedNode} route={route} sources={sources} onRoute={onRoute} /> },
    { id: "dependencies", label: "Dependencies", icon: <UiListTree />, location: "right", width: 320,
      content: <ExplorerDependencies snapshot={route.snapshot} onRoute={onRoute} /> },
  ];

  return <div className="flex h-full min-h-0 flex-col">
    <div className="flex shrink-0 items-end gap-3 border-b border-border px-3 py-2">
      {route.module && <Field label="Checkout"><Select value={route.location} onChange={(event) => {
        const next = locations.data?.find((item) => item.canonical_path === event.target.value);
        onRoute({ location: event.target.value, snapshot: next?.head_snapshot_id ?? "", source: "", node: "", fileSearch: "", symbolSearch: "", offset: 0 });
      }} options={(locations.data ?? []).map((item) => ({ value: item.canonical_path, label: item.canonical_path }))} /></Field>}
      <ServerTimingBadge metrics={browse.timing} />
      {canRefactor && <div className="ml-auto flex gap-2"><Button type="button" size="sm" variant="outline" onClick={() => setRefactorAction("rename")}>Rename {selectedNode ? "symbol" : "file"}</Button>
        {canMove && <Button type="button" size="sm" variant="outline" onClick={() => setRefactorAction("move")}>Move {selectedNode ? "symbol" : "file"}</Button>}</div>}
    </div>
    {heads.data && <div className="shrink-0 px-3 py-2"><MissingHeadWarnings warnings={heads.data.warnings} /></div>}
    {browse.loading && <div className="p-3"><Muted>Loading snapshot…</Muted></div>}
    {browse.error && <div className="p-3"><ErrorMessage error={browse.error} /></div>}
    {browse.data && route.source && !selectedSource && <div className="p-3"><ErrorMessage error={`Source ${route.source} is not in this snapshot`} /></div>}
    {browse.data && route.node && !selectedNode && <div className="p-3"><ErrorMessage error={`Symbol ${route.node} is not in this snapshot`} /></div>}
    <div className="min-h-0 flex-1"><Workspace panes={panes} storageKey="uir-explorer-workspace-v1" /></div>
    {refactorAction && selectedSource && <ExplorerRefactorDialog key={`${route.snapshot}:${route.source}:${route.node}:${refactorAction}`} action={refactorAction} snapshot={route.snapshot} source={selectedSource} node={selectedNode} onClose={() => setRefactorAction(null)}
      onApplied={(snapshot) => { setRefactorAction(null); onRoute({ snapshot, source: "", node: "", line: 0, column: 0 }); onRefresh(); }} />}
  </div>;
}
