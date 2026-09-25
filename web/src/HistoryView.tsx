import { useEffect, useState } from "react";
import { Button, Combobox } from "@flanksource/clicky-ui/components";
import { compareGitRevisions, listGitHistory, type GitHistory, type SymbolDiff, type SymbolChange } from "./api";
import type { Route } from "./route";
import { useLoad } from "./use-load";
import { Card, ErrorMessage, Field, Heading, Muted, PanelForm, Row, Section } from "./ui";

function revisionOptions(history?: GitHistory) {
  return [
    ...(history?.branches ?? []).map((branch) => ({ value: branch.name, label: branch.name, description: branch.commit.slice(0, 12), group: "Branches" })),
    ...(history?.pull_requests ?? []).map((pr) => ({ value: `pr:${pr.number}`, label: `PR #${pr.number}`, description: pr.commit.slice(0, 12), group: "Pull requests" })),
    ...(history?.commits ?? []).map((commit) => ({ value: commit.commit, label: `${commit.commit.slice(0, 12)} ${commit.subject}`, description: commit.authored_at, group: "Commits" })),
  ];
}

function countChanges(diff: SymbolDiff): number {
  return diff.packages.reduce((count, item) => count + item.files.reduce((fileCount, file) => fileCount + file.rows.length, 0), 0);
}

function ChangeRow({ row }: { row: SymbolChange }) {
  return <div className="rounded-md border border-border p-2 text-sm">
    <div className="flex flex-wrap items-baseline gap-2">
      <span className="rounded bg-muted px-1.5 py-0.5 text-xs font-medium">{row.class}</span>
      <strong className="font-mono">{row.owner ? `${row.owner}.` : ""}{row.name}</strong>
      <Muted>{row.kind}</Muted>
      {row.lines && <span className="ml-auto font-mono text-xs">+{row.lines.added} −{row.lines.removed}</span>}
    </div>
    {(row.shape_diff?.length ?? 0) > 0 && <pre className="mt-2 overflow-auto rounded bg-muted p-2 font-mono text-xs">{row.shape_diff?.map((line, index) =>
      <div key={index} className={line.op === "insert" ? "text-green-600" : line.op === "delete" ? "text-red-600" : ""}>
        {line.op === "insert" ? "+ " : line.op === "delete" ? "- " : "  "}{line.text}
      </div>)}</pre>}
    {row.note && <Muted>{row.note}</Muted>}
  </div>;
}

function DiffResult({ diff }: { diff: SymbolDiff }) {
  return <Section>
    <h2 className="text-lg font-semibold">Symbol changes: {diff.from.commit.slice(0, 12)} → {diff.to.commit.slice(0, 12)}</h2>
    <Muted>{countChanges(diff)} symbol changes across {diff.packages.length} packages · snapshots {diff.from.snapshot_id.slice(0, 12)} and {diff.to.snapshot_id.slice(0, 12)}</Muted>
    <ErrorMessage error={diff.lines_error} />
    {diff.packages.length === 0 && <Card>No symbol changes in this range.</Card>}
    {diff.packages.map((pkg) => <Card key={pkg.path}>
      <h3 className="font-mono font-semibold">{pkg.path}</h3>
      {pkg.files.map((file) => <div key={file.path} className="flex flex-col gap-2 border-t border-border pt-2">
        <div className="flex flex-wrap gap-2 text-sm"><strong className="font-mono">{file.path}</strong><Muted>{file.status}</Muted>
          {file.lines && <span className="ml-auto font-mono text-xs">+{file.lines.added} −{file.lines.removed}</span>}</div>
        <ErrorMessage error={file.lines_error} />
        {file.excluded && <Muted>{file.excluded}</Muted>}
        {file.rows.map((row, index) => <ChangeRow key={`${row.class}:${row.owner}:${row.name}:${index}`} row={row} />)}
        {file.hidden_rows ? <Muted>{file.hidden_rows} rows hidden by visibility</Muted> : null}
      </div>)}
    </Card>)}
  </Section>;
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
  const choices = revisionOptions(history.data);

  return <>
    <Heading>Git history and symbol changes</Heading>
    {!route.module && <Muted>Select a module root to compare its Git revisions.</Muted>}
    <ErrorMessage error={history.error} />
    {route.module && <PanelForm onSubmit={(event) => { event.preventDefault(); onRoute({ compareFrom: from.trim(), compareTo: to.trim(), logCommit: "" }); }}>
      <h2 className="text-lg font-semibold">Compare snapshots</h2>
      <Muted>Choose branches, commits, or pull requests. Missing clean snapshots are indexed when you compare.</Muted>
      <Row>
        <Field label="From revision"><Combobox ariaLabel="From revision" value={from} onChange={setFrom} options={choices} allowCustomValue required /></Field>
        <Field label="To revision"><Combobox ariaLabel="To revision" value={to} onChange={setTo} options={choices} allowCustomValue required /></Field>
        <Button type="submit" disabled={!from.trim() || !to.trim() || diff.loading}>Compare</Button>
      </Row>
      <Row><span className="text-sm">Symbol visibility</span>{(["exported", "internal", "all"] as const).map((value) =>
        <label key={value} className="flex items-center gap-1 text-sm"><input type="radio" name="diff-visibility" checked={visibility === value}
          onChange={() => onRoute({ diffVisibility: value })} />{value}</label>)}</Row>
      <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={route.includeTests}
        onChange={(event) => onRoute({ includeTests: event.target.checked })} />Include Go test symbols</label>
      <ErrorMessage error={history.data?.pull_request_error} />
    </PanelForm>}
    {diff.loading && <Muted>Comparing symbols and indexing missing snapshots…</Muted>}
    <ErrorMessage error={diff.error} />
    {diff.data && <DiffResult diff={diff.data} />}
    {route.module && <Section>
      <h2 className="text-lg font-semibold">Changelog / Git log</h2>
      <Muted>Select a commit to see its symbol changes against its first parent.</Muted>
      {history.loading && <Muted>Loading Git history…</Muted>}
      {(history.data?.commits ?? []).map((commit) => <Card key={commit.commit}>
        <div className="flex flex-wrap items-center gap-3">
          <strong>{commit.subject}</strong><code className="text-xs">{commit.commit.slice(0, 12)}</code>
          <Muted>{commit.authored_at}</Muted>
          <Button size="sm" variant="outline" className="ml-auto" disabled={!commit.parents.length}
            onClick={() => onRoute({ compareFrom: commit.parents[0], compareTo: commit.commit, logCommit: commit.commit })}>
            {commit.parents.length ? "View symbol diff" : "Initial commit"}
          </Button>
        </div>
        {route.logCommit === commit.commit && diff.data && <Muted>{countChanges(diff.data)} symbol changes in this commit</Muted>}
      </Card>)}
    </Section>}
  </>;
}
