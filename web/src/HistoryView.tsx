import { useEffect, useState } from "react";
import { Button, Combobox } from "@flanksource/clicky-ui/components";
import { UiHistory } from "@flanksource/clicky-ui/icons";
import { compareGitRevisions, listGitHistory, type GitHistory, type SymbolDiff, type SymbolChange } from "./api";
import { FileTypeIcon, FolderTypeIcon } from "./file-icons";
import type { Route } from "./route";
import { SymbolIcon } from "./symbol-icons";
import { useLoad } from "./use-load";
import { ErrorMessage, Field, Heading, Muted, Section, TextInput } from "./ui";

function revisionOptions(history?: GitHistory) {
  return [
    ...(history?.branches ?? []).map((branch) => ({ value: branch.name, label: branch.name, description: branch.commit.slice(0, 12), group: "Branches" })),
    ...(history?.pull_requests ?? []).map((pr) => ({ value: `pr:${pr.number}`, label: `PR #${pr.number}`, description: pr.commit.slice(0, 12), group: "Pull requests" })),
    ...(history?.commits ?? []).map((commit) => ({ value: commit.commit, label: `${commit.commit.slice(0, 12)} ${commit.subject}`, description: commit.authored_at, group: "Commits" })),
  ];
}

function countChanges(diff: SymbolDiff): number {
  return diff.packages.reduce((count, pkg) => count + pkg.files.reduce((fileCount, file) => fileCount + file.rows.length, 0), 0);
}

function ChoiceBar<T extends string>({ choices, value, onChange }: { choices: { value: T; label: string }[]; value: T; onChange: (value: T) => void }) {
  return <div className="inline-flex rounded-md bg-muted p-0.5" role="group">{choices.map((choice) => <button key={choice.value} type="button" aria-pressed={value === choice.value}
    className={`rounded px-2.5 py-1 text-xs ${value === choice.value ? "bg-background font-semibold shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
    onClick={() => onChange(choice.value)}>{choice.label}</button>)}</div>;
}

function ChangeRow({ row }: { row: SymbolChange }) {
  return <div className="flex min-w-0 items-start gap-2 py-1 pl-5 text-sm">
    <span className="mt-0.5 text-sky-600"><SymbolIcon nodeType={row.kind} /></span>
    <div className="min-w-0 flex-1">
      <div className="flex flex-wrap items-baseline gap-2">
        <code className="break-all font-medium">{row.owner ? `${row.owner}.` : ""}{row.name}</code>
        <span className={`text-xs font-medium ${row.class === "added" ? "text-emerald-600" : row.class === "removed" ? "text-red-600" : "text-sky-600"}`}>{row.class}</span>
        {row.lines && <span className="ml-auto font-mono text-xs text-muted-foreground">+{row.lines.added} −{row.lines.removed}</span>}
      </div>
      {row.shape_diff?.length ? <div className="mt-1 font-mono text-xs">{row.shape_diff.map((line, index) => <div key={index}
        className={line.op === "insert" ? "text-emerald-700" : line.op === "delete" ? "text-red-700 line-through" : "text-muted-foreground"}>{line.text}</div>)}</div>
        : row.shape_after || row.shape_before ? <code className={`block break-all text-xs text-muted-foreground ${row.class === "removed" ? "line-through" : ""}`}>{row.shape_after || row.shape_before}</code> : null}
      {row.path_before && row.path_after && row.path_before !== row.path_after && <div className="text-xs text-muted-foreground">{row.path_before} → {row.path_after}</div>}
      {row.note && <div className="text-xs text-muted-foreground">{row.note}</div>}
      {row.coverage?.length ? <div className="text-xs text-amber-700">Coverage: {row.coverage.join(", ")}</div> : null}
    </div>
  </div>;
}

function FileContents({ file }: { file: SymbolDiff["packages"][number]["files"][number] }) {
  return <div className="pb-2"><ErrorMessage error={file.lines_error} />
    {file.excluded && <div className="text-xs text-amber-700">{file.excluded}</div>}
    {file.rows.map((row, index) => <ChangeRow key={`${row.class}:${row.owner}:${row.name}:${index}`} row={row} />)}
    {file.hidden_rows ? <div className="pl-5 text-xs text-muted-foreground">{file.hidden_rows} changes hidden by visibility</div> : null}
  </div>;
}

function DiffTree({ diff, group }: { diff: SymbolDiff; group: Route["historyGroup"] }) {
  if (countChanges(diff) === 0) return <div className="rounded-md border border-border bg-card p-4 text-sm">No symbol changes in this range.</div>;
  if (group === "change") {
    const rows = diff.packages.flatMap((pkg) => pkg.files.flatMap((file) => file.rows.map((row) => ({ pkg: pkg.path, file: file.path, row }))));
    return <div className="rounded-md border border-border bg-card p-3">{(["added", "removed", "signature", "body", "moved"] as const).map((kind) => {
      const matching = rows.filter(({ row }) => row.class === kind);
      return matching.length ? <details key={kind} open className="border-b border-border last:border-0">
        <summary className="cursor-pointer py-2 text-sm font-semibold capitalize">{kind} <span className="font-normal text-muted-foreground">{matching.length}</span></summary>
        {matching.map(({ pkg, file, row }, index) => <div key={`${pkg}:${file}:${index}`}><div className="pl-5 text-xs text-muted-foreground">{pkg} · {file}</div><ChangeRow row={row} /></div>)}
      </details> : null;
    })}</div>;
  }
  if (group === "file") return <div className="rounded-md border border-border bg-card p-3">{diff.packages.flatMap((pkg) => pkg.files.map((file) => ({ pkg, file }))).map(({ pkg, file }) => <details key={`${pkg.path}:${file.path}`} open className="border-b border-border last:border-0">
    <summary className="flex cursor-pointer items-center gap-2 py-2 text-sm"><FileTypeIcon filename={file.path} /><span className="font-mono">{file.path}</span><span className="text-muted-foreground">· {pkg.path}</span><span className="ml-auto text-xs text-muted-foreground">{file.rows.length} changes</span></summary>
    <FileContents file={file} />
  </details>)}</div>;
  return <div className="rounded-md border border-border bg-card p-3">{diff.packages.map((pkg) => <details key={pkg.path} open className="border-b border-border last:border-0">
    <summary className="flex cursor-pointer items-center gap-2 py-2 text-sm font-semibold"><FolderTypeIcon open /><span className="font-mono">{pkg.path}</span><span className="ml-auto text-xs font-normal text-muted-foreground">{pkg.files.reduce((sum, file) => sum + file.rows.length, 0)} changes</span></summary>
    <div className="pl-3">{pkg.files.map((file) => <details key={file.path} open className="border-l border-border pl-3">
      <summary className="flex cursor-pointer items-center gap-2 py-1 text-sm"><FileTypeIcon filename={file.path} /><span className="font-mono">{file.path}</span><span className="ml-auto text-xs text-muted-foreground">{file.rows.length}</span></summary>
      <FileContents file={file} />
    </details>)}</div>
  </details>)}</div>;
}

function DiffResult({ diff, group }: { diff: SymbolDiff; group: Route["historyGroup"] }) {
  return <div className="flex min-w-0 flex-col gap-2">
    <div className="flex flex-wrap items-center gap-2 text-sm"><strong>{countChanges(diff)} changes · {diff.packages.length} packages</strong><Muted>{diff.from.commit.slice(0, 12)} → {diff.to.commit.slice(0, 12)}</Muted></div>
    <ErrorMessage error={diff.lines_error} /><DiffTree diff={diff} group={group} />
  </div>;
}

export function HistoryView({ route, refresh, onRoute }: { route: Route; refresh: number; onRoute: (patch: Partial<Route>) => void }) {
  const [from, setFrom] = useState(route.compareFrom);
  const [to, setTo] = useState(route.compareTo);
  useEffect(() => { setFrom(route.compareFrom); setTo(route.compareTo); }, [route.compareFrom, route.compareTo]);
  const history = useLoad<GitHistory>(route.module && route.location ? () => listGitHistory(route.module, route.location) : null,
    `history:${route.module}:${route.location}:${refresh}`);
  const visibility = route.diffVisibility || "all";
  const diff = useLoad<SymbolDiff>(route.module && route.compareFrom && route.compareTo ?
    () => compareGitRevisions(route.module, route.compareFrom, route.compareTo, visibility, route.includeTests) : null,
    `diff:${route.module}:${route.compareFrom}:${route.compareTo}:${visibility}:${route.includeTests}`);
  const selectedCommit = history.data?.commits.find((commit) => commit.commit === route.logCommit);
  const log = (history.data?.commits ?? []).filter((commit) => `${commit.subject} ${commit.commit}`.toLowerCase().includes(route.historySearch.toLowerCase()));

  return <Section>
    <div className="flex flex-wrap items-center gap-3"><Heading>UIR change history</Heading>{route.module && <><span className="font-mono text-sm text-muted-foreground">{route.module}</span>{route.snapshot && <code className="rounded bg-muted px-2 py-1 text-xs">{route.snapshot.slice(0, 12)}</code>}</>}</div>
    {!route.module && <Muted>Select an indexed module root to browse its Git commits.</Muted>}
    <ErrorMessage error={history.error} />
    {route.module && <>
      <div className="flex items-center gap-2 border-b border-border pb-2 text-sm"><span className="rounded border border-border bg-card px-3 py-1 font-medium">Git</span><Muted>Indexed Go declarations · snapshots created when selected</Muted></div>
      <form className="flex flex-wrap items-end gap-3 rounded-lg border border-border bg-card p-3" onSubmit={(event) => { event.preventDefault(); onRoute({ compareFrom: from.trim(), compareTo: to.trim(), logCommit: "" }); }}>
        <strong className="self-center text-sm">Compare</strong>
        <Field label="From revision"><Combobox ariaLabel="From revision" value={from} onChange={setFrom} options={revisionOptions(history.data)} allowCustomValue required /></Field>
        <Field label="To revision"><Combobox ariaLabel="To revision" value={to} onChange={setTo} options={revisionOptions(history.data)} allowCustomValue required /></Field>
        <Button type="submit" disabled={!from.trim() || !to.trim() || diff.loading}>Compare</Button>
        <ChoiceBar choices={[{ value: "all", label: "All" }, { value: "exported", label: "Exported" }, { value: "internal", label: "Internal" }]} value={visibility}
          onChange={(value) => onRoute({ diffVisibility: value })} />
        <label className="flex items-center gap-1.5 text-sm"><input type="checkbox" checked={route.includeTests} onChange={(event) => onRoute({ includeTests: event.target.checked })} />Go tests</label>
      </form>
      <div className="flex flex-wrap items-center gap-3">
        <ChoiceBar choices={[{ value: "package", label: "By package" }, { value: "file", label: "By file" }, { value: "change", label: "By change" }]} value={route.historyGroup} onChange={(value) => onRoute({ historyGroup: value })} />
        <ChoiceBar choices={[{ value: "sidebar", label: "Sidebar" }, { value: "inline", label: "Inline" }]} value={route.historyLayout} onChange={(value) => onRoute({ historyLayout: value })} />
        {diff.data && <span className="ml-auto rounded-full bg-blue-50 px-2 py-1 text-xs text-blue-700">{countChanges(diff.data)} changes · {diff.data.packages.length} packages</span>}
      </div>
      <ErrorMessage error={history.data?.pull_request_error} />
      {!route.logCommit && diff.loading && <Muted>Indexing missing commit snapshots and comparing symbols…</Muted>}
      {!route.logCommit && <ErrorMessage error={diff.error} />}
      {!route.logCommit && diff.data && <DiffResult diff={diff.data} group={route.historyGroup} />}
      <div className={route.historyLayout === "sidebar" ? "grid min-w-0 gap-3 lg:grid-cols-[minmax(20rem,0.8fr)_minmax(0,1.2fr)]" : "flex min-w-0 flex-col gap-3"}>
        <section className="min-w-0 overflow-hidden rounded-lg border border-border bg-card">
          <h2 className="flex items-center gap-2 border-b border-border bg-muted/40 px-3 py-2 text-sm font-semibold"><UiHistory className="size-4" />Git log</h2>
          <div className="p-2"><TextInput aria-label="Search Git commits" placeholder="Search commits…" value={route.historySearch} onChange={(event) => onRoute({ historySearch: event.target.value })} /></div>
          {history.loading && <div className="px-3 pb-3"><Muted>Loading Git history…</Muted></div>}
          {!history.loading && log.length === 0 && <div className="px-3 pb-3"><Muted>No commits match this search.</Muted></div>}
          <div className="max-h-[48rem] overflow-auto">{log.map((commit) => <div key={commit.commit} className={`border-t border-border ${route.logCommit === commit.commit ? "border-l-2 border-l-blue-500 bg-blue-50/50" : ""}`}>
            <button type="button" className="flex w-full flex-col gap-1 px-3 py-2 text-left hover:bg-muted/50" aria-expanded={route.logCommit === commit.commit}
              onClick={() => onRoute({ compareFrom: commit.parents[0] ?? "", compareTo: commit.commit, logCommit: commit.commit })}>
              <span className="flex w-full items-baseline gap-2 text-sm"><strong className="min-w-0 flex-1 truncate">{commit.subject}</strong><code className="text-xs text-muted-foreground">{commit.commit.slice(0, 7)}</code></span>
              <span className="text-xs text-muted-foreground">{new Date(commit.authored_at).toLocaleString()}{commit.parents.length === 0 ? " · Initial commit" : ""}</span>
            </button>
            {route.historyLayout === "inline" && route.logCommit === commit.commit && <div className="px-3 pb-3">
              {!commit.parents.length ? <Muted>The initial commit has no parent to compare.</Muted> : diff.loading ? <Muted>Indexing missing commit snapshots and comparing symbols…</Muted> : diff.error ? <ErrorMessage error={diff.error} /> : diff.data ? <DiffResult diff={diff.data} group={route.historyGroup} /> : null}
            </div>}
          </div>)}</div>
        </section>
        {route.historyLayout === "sidebar" && <section className="min-w-0">
          {!selectedCommit ? <div className="rounded-lg border border-border bg-card p-4"><Muted>Select a commit to see its logical changes.</Muted></div> : !selectedCommit.parents.length ? <div className="rounded-lg border border-border bg-card p-4"><Muted>The initial commit has no parent to compare.</Muted></div> : diff.loading ? <Muted>Indexing missing commit snapshots and comparing symbols…</Muted> : diff.error ? <ErrorMessage error={diff.error} /> : diff.data ? <DiffResult diff={diff.data} group={route.historyGroup} /> : null}
        </section>}
      </div>
    </>}
  </Section>;
}
