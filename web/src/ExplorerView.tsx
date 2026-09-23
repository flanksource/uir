import { useEffect, useMemo, useRef, useState } from "react";
import { Select, Tabs, Workspace, type WorkspacePaneSpec } from "@flanksource/clicky-ui/components";
import { Tree } from "@flanksource/clicky-ui/data";
import { UiAnnotation, UiClass3, UiClass6, UiColumn, UiConstant1, UiField3, UiFileCode, UiFolder, UiFunction1, UiGroupByPackage, UiIndex, UiListTree, UiMethod2, UiMethod6, UiModule1, UiRecord1, UiSymbol, UiVariable6 } from "@flanksource/clicky-ui/icons";
import { MonacoEditor, MonacoProvider, type MonacoEditorProps } from "@flanksource/clicky-ui/monaco";
import { readModuleSource, type ModuleBrowse, type ModuleLocation, type ModuleNode, type ModuleSource, type ModuleSourceContent } from "./api";
import { fileTree, symbolTree, type FileItem, type SymbolItem } from "./explorer-model";
import { getMonacoWorker } from "./monaco-workers";
import type { Route } from "./route";
import { CodeBlock, Detail, DetailGrid, ErrorMessage, Field, Muted } from "./ui";
import { useLoad, type Load } from "./use-load";

type OutlineItem = SymbolItem | { id: string; label: string; children: SymbolItem[]; source: ModuleSource };
type EditorInstance = Parameters<NonNullable<MonacoEditorProps["onMount"]>>[0];
const EMPTY_SOURCES: ModuleSource[] = [];
const EMPTY_NODES: ModuleNode[] = [];
const symbolIcons = {
  package: UiGroupByPackage,
  module: UiModule1,
  class: UiClass3,
  interface: UiClass6,
  record: UiRecord1,
  method: UiMethod6,
  function: UiFunction1,
  constructor: UiMethod2,
  record_field: UiField3,
  column: UiColumn,
  index: UiIndex,
  function_variable: UiVariable6,
  package_variable: UiVariable6,
  annotation: UiAnnotation,
  constant: UiConstant1,
} as const;

function SymbolIcon({ nodeType }: { nodeType: string }) {
  const Icon = symbolIcons[nodeType as keyof typeof symbolIcons] ?? UiSymbol;
  return <Icon className="size-4 shrink-0" aria-hidden="true" />;
}

function findItem<T extends { id: string; children: T[] }>(items: T[], id: string): T | undefined {
  for (const item of items) {
    if (item.id === id) return item;
    const child = findItem(item.children, id);
    if (child) return child;
  }
}

function revealSymbol(editor: EditorInstance, node?: ModuleNode) {
  if (!node?.line || node.line < 1) return;
  editor.setPosition({ lineNumber: node.line, column: Math.max(1, node.column ?? 1) });
  editor.revealLineInCenter(node.line);
}

function SourcePane({ snapshot, source, node }: { snapshot: string; source?: ModuleSource; node?: ModuleNode }) {
  const content = useLoad<ModuleSourceContent>(source ? () => readModuleSource(snapshot, source.path) : null, `${snapshot}:${source?.id}`);
  const editor = useRef<EditorInstance | null>(null);
  useEffect(() => { if (editor.current && content.data) revealSymbol(editor.current, node); }, [content.data, node]);

  if (!source) return <div className="p-3"><Muted>Select a file to view its verified content.</Muted></div>;
  return <div className="flex h-full min-h-0 flex-col">
    <div className="shrink-0 border-b border-border px-3 py-2 text-xs">
      <div className="truncate font-medium" title={source.path}>{source.path}</div>
      <Muted>{source.package_path} · SHA-256 {source.content_hash.slice(0, 12)}</Muted>
      {content.data && <div><Muted>{content.data.origin === "git" ? `Pinned Git revision ${content.data.revision}` : "Local file matches indexed hash"}</Muted></div>}
    </div>
    {content.loading && <div className="p-3"><Muted>Loading source…</Muted></div>}
    {content.error && <div className="p-3"><ErrorMessage error={content.error} /></div>}
    {content.data && <div className="min-h-0 flex-1 [&>[data-slot=monaco-editor]]:h-full [&>[data-slot=monaco-editor]]:rounded-none [&>[data-slot=monaco-editor]]:border-0">
      <MonacoProvider getWorker={getMonacoWorker}>
        <MonacoEditor value={content.data.content} language="go" path={`file:///uir/${snapshot}/${source.path}`} height="100%" readOnly
          onMount={(instance) => { editor.current = instance; revealSymbol(instance, node); }} />
      </MonacoProvider>
    </div>}
  </div>;
}

function SymbolDetails({ node }: { node?: ModuleNode }) {
  const [tab, setTab] = useState("overview");
  if (!node) return <div className="p-3"><Muted>Select a symbol to inspect its payload, field, and outgoing calls.</Muted></div>;
  return <div className="flex min-w-0 flex-col gap-3 p-3">
    <strong className="break-all text-sm">{node.symbol || node.id}</strong>
    <Tabs value={tab} onChange={setTab} tabs={[
      { id: "overview", label: "Overview" }, { id: "field", label: "Field", count: node.field ? 1 : 0 },
      { id: "calls", label: "Calls", count: node.calls.length }, { id: "raw", label: "Raw" },
    ]} />
    {tab === "overview" && <>
      <DetailGrid>
        <Detail label="Kind">{node.node_type}</Detail>
        <Detail label="Source">{node.path}{node.line ? `:${node.line}` : ""}</Detail>
        <Detail label="Child slot">{node.child_slot}</Detail>
        <Detail label="Semantic hash">{node.semantic_hash.slice(0, 12)}</Detail>
      </DetailGrid>
      <h3>UIR payload</h3><CodeBlock>{JSON.stringify(node.payload, null, 2)}</CodeBlock>
    </>}
    {tab === "field" && (node.field ? <CodeBlock>{JSON.stringify(node.field, null, 2)}</CodeBlock> : <Muted>No field projection</Muted>)}
    {tab === "calls" && (node.calls.length ? <CodeBlock>{JSON.stringify(node.calls, null, 2)}</CodeBlock> : <Muted>No outgoing calls</Muted>)}
    {tab === "raw" && <CodeBlock>{JSON.stringify(node, null, 2)}</CodeBlock>}
  </div>;
}

function FilePane({ files, selected, route, onRoute }: { files: FileItem[]; selected?: FileItem; route: Route; onRoute: (patch: Partial<Route>, replace?: boolean) => void }) {
  return <Tree<FileItem> className="h-full" ariaLabel="Source files" roots={files} getChildren={(item) => item.children} getKey={(item) => item.id}
    selected={selected} revealSelected defaultOpen={(_, depth) => depth < 2} showSearch searchQuery={route.fileSearch}
    onSearchQueryChange={(fileSearch) => onRoute({ fileSearch }, true)} getSearchText={(item) => item.path}
    onSelect={(item) => { if (item.source) onRoute({ source: item.source.id, node: "", fileSearch: "", symbolSearch: "" }); }}
    renderRow={({ node: item }) => <><span className="shrink-0 text-muted-foreground">{item.source ? <UiFileCode className="size-4" /> : <UiFolder className="size-4" />}</span><span className="truncate" title={item.path}>{item.label}</span></>}
    empty={<div className="p-3 text-sm text-muted-foreground">No indexed source files.</div>} />;
}

function OutlinePane({ items, selected, route, onRoute }: { items: OutlineItem[]; selected?: OutlineItem; route: Route; onRoute: (patch: Partial<Route>, replace?: boolean) => void }) {
  return <Tree<OutlineItem> className="h-full" ariaLabel="Symbols" roots={items} getChildren={(item) => item.children} getKey={(item) => item.id}
    selected={selected} revealSelected showSearch searchQuery={route.symbolSearch}
    onSearchQueryChange={(symbolSearch) => onRoute({ symbolSearch }, true)} getSearchText={(item) => "node" in item ? `${item.label} ${item.node.symbol}` : ""}
    onSelect={(item) => { if ("node" in item) onRoute({ source: item.node.source_id, node: item.node.id, fileSearch: "", symbolSearch: "" }); }}
    renderRow={({ node: item }) => <>{"node" in item ? <SymbolIcon nodeType={item.node.node_type} /> : <UiFileCode className="size-4 shrink-0" aria-hidden="true" />}<span className="truncate" title={"node" in item ? item.node.symbol : item.source.path}>{item.label}</span>{"node" in item && item.node.line && <span className="ml-auto text-xs text-muted-foreground">{item.node.line}</span>}</>}
    empty={<div className="p-3 text-sm text-muted-foreground">No indexed symbols for this file.</div>} />;
}

export function ExplorerView({ route, browse, locations, onRoute }: { route: Route; browse: Load<ModuleBrowse>; locations: Load<ModuleLocation[]>; onRoute: (patch: Partial<Route>, replace?: boolean) => void }) {
  const sources = browse.data?.sources ?? EMPTY_SOURCES;
  const nodes = browse.data?.nodes ?? EMPTY_NODES;
  const selectedNode = nodes.find((node) => node.id === route.node);
  const selectedSource = sources.find((source) => source.id === route.source);
  const files = useMemo(() => fileTree(sources), [sources]);
  const selectedFile = findItem(files, `file:${route.source}`);
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

  useEffect(() => {
    if (!browse.data) return;
    if (route.node && selectedNode && route.source !== selectedNode.source_id) onRoute({ source: selectedNode.source_id, fileSearch: "" }, true);
    else if (!route.source && !route.node && !route.fileSearch && sources.length) onRoute({ source: sources[0].id }, true);
  }, [browse.data, onRoute, route.fileSearch, route.node, route.source, selectedNode, sources]);

  const panes: WorkspacePaneSpec[] = [
    { id: "files", label: "Files", icon: <UiFolder />, location: "left", width: 300, height: 360,
      content: <FilePane files={files} selected={selectedFile} route={route} onRoute={onRoute} />, contentClassName: "overflow-hidden" },
    { id: "outline", label: "Symbols", icon: <UiListTree />, location: "left", width: 300, height: 240,
      content: <OutlinePane items={items} selected={selectedSymbol} route={route} onRoute={onRoute} />, contentClassName: "overflow-hidden" },
    { id: "source", label: selectedSource?.path ?? "Source", icon: <UiFileCode />, location: "center", collapsible: false,
      content: <SourcePane snapshot={route.snapshot} source={selectedSource} node={selectedNode} />, contentClassName: "overflow-hidden" },
    { id: "details", label: "Details", icon: <UiListTree />, location: "right", width: 320,
      content: <SymbolDetails node={selectedNode} /> },
  ];

  return <div className="flex h-full min-h-0 flex-col">
    <div className="flex shrink-0 items-end gap-3 border-b border-border px-3 py-2">
      <Field label="Checkout"><Select value={route.location} onChange={(event) => {
        const next = locations.data?.find((item) => item.canonical_path === event.target.value);
        onRoute({ location: event.target.value, snapshot: next?.head_snapshot_id ?? "", source: "", node: "", fileSearch: "", symbolSearch: "", offset: 0 });
      }} options={(locations.data ?? []).map((item) => ({ value: item.canonical_path, label: item.canonical_path }))} /></Field>
    </div>
    {browse.loading && <div className="p-3"><Muted>Loading snapshot…</Muted></div>}
    {browse.error && <div className="p-3"><ErrorMessage error={browse.error} /></div>}
    {browse.data && route.source && !selectedSource && <div className="p-3"><ErrorMessage error={`Source ${route.source} is not in this snapshot`} /></div>}
    {browse.data && route.node && !selectedNode && <div className="p-3"><ErrorMessage error={`Symbol ${route.node} is not in this snapshot`} /></div>}
    <div className="min-h-0 flex-1"><Workspace panes={panes} storageKey="uir-explorer-workspace-v1" /></div>
  </div>;
}
