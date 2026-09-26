import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { Button, CommandPalette, type CommandGroup } from "@flanksource/clicky-ui/components";
import { ApiError, browseModule, errorMessage, runModuleQuery, type ModuleQueryResult, type ModuleQueryRow } from "./api";
import { queryMatchLabel, queryMatchRoute, queryPaletteRows, searchInputMode } from "./query-palette-model";
import { queryScope } from "./query-model";
import type { Route } from "./route";

const QueryExpressionInput = lazy(() => import("./QueryExpressionInput").then(({ QueryExpressionInput }) => ({ default: QueryExpressionInput })));

export function QueryCommandPalette({ open, onOpenChange, route, onRoute, commands, status }: {
  open: boolean; onOpenChange: (open: boolean) => void; route: Route;
  onRoute: (patch: Partial<Route>) => void; commands: CommandGroup[]; status: string;
}) {
  const [mode, setMode] = useState<"search" | "expression">("search");
  const [search, setSearch] = useState("");
  const [expression, setExpression] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [result, setResult] = useState<ModuleQueryResult>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [diagnostic, setDiagnostic] = useState<ApiError>();
  const [focusListKey, setFocusListKey] = useState<number>();
  const request = useRef(0);
  const switchTimer = useRef<ReturnType<typeof setTimeout>>();

  useEffect(() => {
    request.current += 1;
    setResult(undefined);
    setSubmitted("");
    setBusy(false);
    setError("");
  }, [route.module, route.snapshot]);
  useEffect(() => () => { request.current += 1; clearTimeout(switchTimer.current); }, []);
  function changeOpen(next: boolean) {
    if (!next) {
      request.current += 1;
      clearTimeout(switchTimer.current);
      setMode("search"); setSearch(""); setExpression(""); setSubmitted("");
      setResult(undefined); setBusy(false); setError(""); setFocusListKey(undefined);
    }
    onOpenChange(next);
  }

  function changeSearch(value: string) {
    clearTimeout(switchTimer.current);
    const next = searchInputMode(value);
    if (next.mode === "search") { setSearch(next.value); setExpression(""); return; }
    setExpression(next.value);
    setSearch(value);
    switchTimer.current = setTimeout(() => { setSearch(""); setMode("expression"); }, 200);
  }

  function changeExpression(value: string) {
    request.current += 1;
    setExpression(value); setResult(undefined); setSubmitted(""); setBusy(false); setError("");
    setFocusListKey(undefined);
  }

  async function runExpression() {
    const text = expression.trim();
    if (!text) { setDiagnostic(undefined); setError("Enter an expression before running it"); return; }
    const scope = queryScope(route);
    if (!scope) { setDiagnostic(undefined); setError(`Snapshot for ${route.module} is not ready`); return; }
    const current = ++request.current;
    setBusy(true); setResult(undefined); setError("");
    try {
      const { data } = await runModuleQuery(text, scope.root, scope.snapshot);
      if (current !== request.current) return;
      queryPaletteRows(data);
      setResult(data); setSubmitted(text); setFocusListKey(current);
    } catch (reason) {
      if (current === request.current) { setDiagnostic(reason instanceof ApiError ? reason : undefined); setError(errorMessage(reason)); }
    } finally {
      if (current === request.current) setBusy(false);
    }
  }

  async function openMatch(row: ModuleQueryRow) {
    const current = request.current;
    setBusy(true); setError("");
    try {
      const { data } = await browseModule(row.snapshot_id);
      if (current !== request.current) return;
      onRoute(queryMatchRoute(row, data.sources));
      changeOpen(false);
    } catch (reason) {
      if (current === request.current) { setDiagnostic(undefined); setError(errorMessage(reason)); }
    } finally {
      if (current === request.current) setBusy(false);
    }
  }

  const { matches, candidates } = result ? queryPaletteRows(result) : { matches: [], candidates: [] };
  const groups: CommandGroup[] = mode === "search" ? commands : result ? [
    { id: "query-matches", heading: result.path ? "Call path" : `Matches (${result.total})`, items: matches.map((row, index) => ({
      id: `match:${index}`, label: queryMatchLabel(row), description: `${row.root} · ${row.source}`,
      onSelect: () => { void openMatch(row); },
    })) },
    { id: "query-candidates", heading: "Candidates", items: candidates.map((row, index) => {
      const symbol = result.symbols.find((item) => item.id === row.symbol_id);
      if (!symbol) throw new Error(`Candidate ${row.symbol_id} has no resolved symbol`);
      return { id: `candidate:${index}`, label: `Resolve ${symbol.query_name}`, description: "Open named symbol on Query page",
        onSelect: () => { onRoute({ view: "query", expression: symbol.query_name }); changeOpen(false); } };
    }) },
    { id: "query-actions", items: [{ id: "query:full", label: "Open full results on Query page", shortcut: "↵",
      onSelect: () => { onRoute({ view: "query", expression: submitted }); changeOpen(false); } }] },
  ] : [];

  return <CommandPalette open={open} onOpenChange={changeOpen} groups={groups}
    query={mode === "search" ? search : expression} onQueryChange={changeSearch}
    filter={mode === "expression" ? false : undefined} closeOnSelect={mode === "search"}
    focusListKey={mode === "expression" ? focusListKey : undefined}
    customInput={mode === "expression" ? <div className="flex items-start gap-2">
      <Button type="button" size="sm" variant="outline" onClick={() => {
        request.current += 1; setMode("search"); setExpression(""); setResult(undefined); setError(""); setFocusListKey(undefined);
      }}>Search</Button>
      <Suspense fallback={<span className="text-sm text-muted-foreground">Loading expression editor…</span>}>
        <QueryExpressionInput draft={expression} route={route} path="file:///uir/palette/expression.uirq" autoFocus
          runError={error && !result ? diagnostic ?? error : undefined} onChange={changeExpression} onRun={() => { void runExpression(); }} />
      </Suspense>
    </div> : undefined}
    loading={mode === "expression" && busy} emptyState={mode === "expression" ? "Ctrl/Cmd+Enter runs the expression" : undefined}
    footer={mode === "expression" ? error || (result ? `${result.total} matches · Enter opens the selected result` : "Ctrl/Cmd+Enter runs the expression") : status}
    size="lg" />;
}
