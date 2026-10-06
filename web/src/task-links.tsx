import { useEffect, useRef, type MouseEvent, type ReactNode } from "react";
import { useTaskRun, useTaskRuns } from "@flanksource/clicky-ui/hooks";
import { routeURL, type Route } from "./route";

export const TASKS_API = "/api/v1";
export const MODULE_INDEX_KIND = "module-index";

type RouteProps = { route: Route; onRoute: (patch: Partial<Route>) => void };

/** Opens the Tasks view focused on one run; a modified click keeps the browser's own link handling. */
export function TaskRunLink({ runId, route, onRoute, children }: RouteProps & { runId: string; children: ReactNode }) {
  const open = (event: MouseEvent<HTMLAnchorElement>) => {
    event.stopPropagation();
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onRoute({ view: "tasks", taskRun: runId });
  };
  return <a className="text-primary underline-offset-2 hover:underline" href={routeURL({ ...route, view: "tasks", taskRun: runId })} onClick={open}>{children}</a>;
}

/** Calls onEnd once the run's groups all reach a terminal status on the task stream. */
export function useRunEnd(runId: string, onEnd: () => void) {
  const { isComplete } = useTaskRun({ id: runId, basePath: TASKS_API, enabled: Boolean(runId) });
  const ended = useRef(onEnd);
  ended.current = onEnd;
  useEffect(() => {
    if (runId && isComplete) ended.current();
  }, [runId, isComplete]);
}

/** The module-index runs that index one commit of a root, as History's auto-index starts them. */
export function CommitIndexRuns({ root, commit, route, onRoute }: RouteProps & { root: string; commit: string }) {
  const { runs } = useTaskRuns({ basePath: TASKS_API, kind: MODULE_INDEX_KIND, labels: { root, commit } });
  return <>{runs.map((run) => <span key={run.id} className="text-sm" data-testid="commit-index-run">
    <TaskRunLink runId={run.id} route={route} onRoute={onRoute}>Indexing {commit.slice(0, 7)}</TaskRunLink>
    <span className="text-muted-foreground"> · {run.status} · {run.completed}/{run.total} tasks</span>
  </span>)}</>;
}
