import { useEffect, useRef, useState } from "react";
import { Button, Combobox } from "@flanksource/clicky-ui/components";
import { Badge, formatDateTimeRelative } from "@flanksource/clicky-ui/data";
import { UiHistory } from "@flanksource/clicky-ui/icons";
import { compareGitRevisions, errorMessage, listGitHistory, type GitCommit, type GitHistory, type SymbolDiff } from "./api";
import { snapshotExplorerPatch } from "./explorer-navigation";
import { countChanges, DiffResult } from "./HistoryDiff";
import { resolveRevision } from "./history-model";
import type { Route } from "./route";
import { CommitIndexRuns } from "./task-links";
import { useLoad, type Load } from "./use-load";
import { ErrorMessage, Field, Heading, Muted, Section, TextInput } from "./ui";

type OnRoute = (patch: Partial<Route>) => void;

function revisionOptions(history?: GitHistory) {
  return [
    ...(history?.branches ?? []).map((branch) => ({ value: branch.name, label: branch.name, description: branch.commit.slice(0, 12), group: "Branches" })),
    ...(history?.pull_requests ?? []).map((pr) => ({ value: `pr:${pr.number}`, label: `PR #${pr.number}`, description: pr.commit.slice(0, 12), group: "Pull requests" })),
    ...(history?.commits ?? []).map((commit) => ({ value: commit.commit, label: `${commit.commit.slice(0, 12)} ${commit.subject}`, description: formatDateTimeRelative(commit.authored_at), group: "Commits" })),
  ];
}

function ChoiceBar<T extends string>({ choices, value, onChange }: { choices: { value: T; label: string }[]; value: T; onChange: (value: T) => void }) {
  return <div className="inline-flex rounded-md bg-muted p-0.5" role="group">{choices.map((choice) => <button key={choice.value} type="button" aria-pressed={value === choice.value}
    className={`rounded px-2.5 py-1 text-xs ${value === choice.value ? "bg-background font-semibold shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
    onClick={() => onChange(choice.value)}>{choice.label}</button>)}</div>;
}

/** The snapshot the listed checkout holds for a commit, opened in the Explorer on click. */
function IndexedBadge({ commit, location, onRoute }: { commit: GitCommit; location: string; onRoute: OnRoute }) {
  const snapshot = commit.snapshot_id;
  if (!snapshot) return null;
  if (!commit.snapshot_completed_at || !commit.snapshot_reason) throw new Error(`Commit ${commit.commit} has snapshot ${snapshot} without its completion time or reason`);
  return <button type="button" className="text-left" title={`Open snapshot ${snapshot} in Explorer`} aria-label={`Open indexed snapshot of ${commit.commit.slice(0, 7)} in Explorer`}
    onClick={() => onRoute(snapshotExplorerPatch({ location, snapshot }))}>
    <Badge tone="success" size="sm" clickToCopy={false}>indexed {formatDateTimeRelative(commit.snapshot_completed_at)} · {commit.snapshot_reason}</Badge>
  </button>;
}

/** While a diff runs, its auto-index runs for either commit link to the Tasks view. */
function DiffPending({ history, route, onRoute }: { history?: GitHistory; route: Route; onRoute: OnRoute }) {
  const commits = history ? [...new Set([route.compareFrom, route.compareTo].map((revision) => resolveRevision(history, revision)))].filter((commit) => commit !== undefined) : [];
  return <div className="flex flex-wrap items-center gap-3" data-testid="diff-pending"><Muted>Comparing symbols; commits without a snapshot are indexed first…</Muted>
    {commits.map((commit) => <CommitIndexRuns key={commit} root={route.module} commit={commit} route={route} onRoute={onRoute} />)}</div>;
}

function DiffOutcome({ diff, history, route, onRoute }: { diff: Load<SymbolDiff>; history?: GitHistory; route: Route; onRoute: OnRoute }) {
  if (diff.loading) return <DiffPending history={history} route={route} onRoute={onRoute} />;
  if (diff.error) return <ErrorMessage error={diff.error} />;
  return diff.data ? <DiffResult diff={diff.data} group={route.historyGroup} location={route.location} onRoute={onRoute} /> : null;
}

function CommitLog({ history, log, diff, route, onRoute }: { history: Load<GitHistory>; log: GitCommit[]; diff: Load<SymbolDiff>; route: Route; onRoute: OnRoute }) {
  return <section className="min-w-0 overflow-hidden rounded-lg border border-border bg-card">
    <h2 className="flex items-center gap-2 border-b border-border bg-muted/40 px-3 py-2 text-sm font-semibold"><UiHistory className="size-4" />Git log</h2>
    <div className="p-2"><TextInput aria-label="Search Git commits" placeholder="Search commits…" value={route.historySearch} onChange={(event) => onRoute({ historySearch: event.target.value })} /></div>
    {history.loading && <div className="px-3 pb-3"><Muted>Loading Git history…</Muted></div>}
    {!history.loading && log.length === 0 && <div className="px-3 pb-3"><Muted>No commits match this search.</Muted></div>}
    <div className="max-h-[48rem] overflow-auto">{log.map((commit) => <div key={commit.commit} data-testid="commit-row" className={`border-t border-border ${route.logCommit === commit.commit ? "border-l-2 border-l-blue-500 bg-blue-50/50" : ""}`}>
      <button type="button" className="flex w-full flex-col gap-1 px-3 pt-2 text-left hover:bg-muted/50" aria-expanded={route.logCommit === commit.commit}
        onClick={() => onRoute({ compareFrom: commit.parents[0] ?? "", compareTo: commit.commit, logCommit: commit.commit })}>
        <span className="flex w-full items-baseline gap-2 text-sm"><strong className="min-w-0 flex-1 truncate">{commit.subject}</strong><code className="text-xs text-muted-foreground">{commit.commit.slice(0, 7)}</code></span>
        <span className="text-xs text-muted-foreground" title={commit.authored_at}>{formatDateTimeRelative(commit.authored_at)}{commit.parents.length === 0 ? " · Initial commit" : ""}</span>
      </button>
      <div className="px-3 pb-2 pt-1">{history.data && <IndexedBadge commit={commit} location={history.data.location} onRoute={onRoute} />}</div>
      {route.historyLayout === "inline" && route.logCommit === commit.commit && <div className="px-3 pb-3">
        {!commit.parents.length ? <Muted>The initial commit has no parent to compare.</Muted> : <DiffOutcome diff={diff} history={history.data} route={route} onRoute={onRoute} />}
      </div>}
    </div>)}</div>
  </section>;
}

// useIndexedHistory re-reads the history once after a diff of commits it listed without a snapshot,
// since the diff auto-indexed them and the listing only names snapshots that existed when it loaded.
function useIndexedHistory(history: Load<GitHistory>, historyKey: string, diff: SymbolDiff | undefined, route: Route): Load<GitHistory> {
  const [reread, setReread] = useState<{ key: string; data?: GitHistory; error?: string }>();
  const current = reread?.key === historyKey && reread.data ? reread.data : history.data;
  const checked = useRef<SymbolDiff>();
  useEffect(() => {
    if (!diff || !current || checked.current === diff) return;
    checked.current = diff;
    const indexed = [diff.from.commit, diff.to.commit].some((commit) => current.commits.some((item) => item.commit === commit && !item.snapshot_id));
    if (!indexed) return;
    listGitHistory(route.module, route.location).then((data) => setReread({ key: historyKey, data }),
      (error: unknown) => setReread({ key: historyKey, error: `Cannot refresh indexed commits: ${errorMessage(error)}` }));
  }, [diff, current, historyKey, route.module, route.location]);
  return { ...history, data: current, error: history.error ?? (reread?.key === historyKey ? reread.error : undefined) };
}

export function HistoryView({ route, refresh, onRoute }: { route: Route; refresh: number; onRoute: OnRoute }) {
  const [from, setFrom] = useState(route.compareFrom);
  const [to, setTo] = useState(route.compareTo);
  useEffect(() => { setFrom(route.compareFrom); setTo(route.compareTo); }, [route.compareFrom, route.compareTo]);
  const historyKey = `history:${route.module}:${route.location}:${refresh}`;
  const listed = useLoad<GitHistory>(route.module && route.location ? () => listGitHistory(route.module, route.location) : null, historyKey);
  const visibility = route.diffVisibility || "all";
  const diff = useLoad<SymbolDiff>(route.module && route.compareFrom && route.compareTo ?
    () => compareGitRevisions(route.module, route.compareFrom, route.compareTo, visibility, route.includeTests, route.location) : null,
    `diff:${route.module}:${route.location}:${route.compareFrom}:${route.compareTo}:${visibility}:${route.includeTests}`);
  const history = useIndexedHistory(listed, historyKey, diff.data, route);
  const selectedCommit = history.data?.commits.find((commit) => commit.commit === route.logCommit);
  const log = (history.data?.commits ?? []).filter((commit) => `${commit.subject} ${commit.commit}`.toLowerCase().includes(route.historySearch.toLowerCase()));

  return <Section>
    <div className="flex flex-wrap items-center gap-3"><Heading>UIR change history</Heading>{route.module && <span className="font-mono text-sm text-muted-foreground">{route.module}</span>}</div>
    {!route.module && <Muted>Select an indexed module root to browse its Git commits.</Muted>}
    <ErrorMessage error={history.error} />
    {route.module && <>
      <Muted>Indexed Go declarations · a commit without a snapshot is indexed when it is compared</Muted>
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
      {!route.logCommit && <DiffOutcome diff={diff} history={history.data} route={route} onRoute={onRoute} />}
      <div className={route.historyLayout === "sidebar" ? "grid min-w-0 gap-3 lg:grid-cols-[minmax(20rem,0.8fr)_minmax(0,1.2fr)]" : "flex min-w-0 flex-col gap-3"}>
        <CommitLog history={history} log={log} diff={diff} route={route} onRoute={onRoute} />
        {route.historyLayout === "sidebar" && <section className="min-w-0">
          {!selectedCommit ? <div className="rounded-lg border border-border bg-card p-4"><Muted>Select a commit to see its logical changes.</Muted></div>
            : !selectedCommit.parents.length ? <div className="rounded-lg border border-border bg-card p-4"><Muted>The initial commit has no parent to compare.</Muted></div>
              : <DiffOutcome diff={diff} history={history.data} route={route} onRoute={onRoute} />}
        </section>}
      </div>
    </>}
  </Section>;
}
