import { useEffect, useId, useRef, useState } from "react";
import type { Monaco } from "@monaco-editor/react";
import { MonacoEditor, MonacoProvider } from "@flanksource/clicky-ui/monaco";
import type { IDisposable, editor as MonacoEditorType } from "monaco-editor";
import { ApiError, errorMessage } from "./api";
import { getMonacoWorker } from "./monaco-workers";
import { queryCompletionContext } from "./query-completion";
import { queryRunnerErrorRange } from "./query-diagnostic";
import { queryCompletionProvider, queryLanguageId, registerQueryLanguage } from "./query-language";
import { queryScope } from "./query-model";
import type { Route } from "./route";
import "./query-expression.css";

function setRunnerMarker(model: MonacoEditorType.ITextModel, monaco: Monaco, error?: string | ApiError): void {
  monaco.editor.setModelMarkers(model, "uir-query-runner", error ? [{
    ...queryRunnerErrorRange(error, model.getValue()),
    message: errorMessage(error),
    source: "UIR query runner",
    severity: monaco.MarkerSeverity.Error,
  }] : []);
}

export function QueryExpressionInput({ draft, route, path, autoFocus = false, runError, onChange, onRun }: {
  draft: string; route: Route; path: string; autoFocus?: boolean; runError?: string | ApiError;
  onChange: (value: string) => void; onRun: () => void;
}) {
  const labelId = useId();
  const scope = useRef(queryScope(route));
  const run = useRef(onRun);
  const currentRunError = useRef(runError);
  const model = useRef<MonacoEditorType.ITextModel | null>(null);
  const monacoRef = useRef<Monaco | null>(null);
  const disposables = useRef<IDisposable[]>([]);
  const [completionError, setCompletionError] = useState("");
  const [context, setContext] = useState(() => queryCompletionContext(draft, draft.length));
  scope.current = queryScope(route);
  run.current = onRun;
  currentRunError.current = runError;
  useEffect(() => {
    if (model.current && monacoRef.current) setRunnerMarker(model.current, monacoRef.current, runError);
  }, [draft, runError]);
  useEffect(() => () => {
    if (model.current && monacoRef.current) setRunnerMarker(model.current, monacoRef.current);
    disposables.current.forEach((item) => item.dispose());
    disposables.current = [];
  }, []);
  const hint = context.mode === "symbol" ? "Type a Go symbol; indexed candidates appear as you type."
    : context.mode === "relation" ? "Choose a relation (<, >, =, :impl, :methods, ~w), set operator (&, |), or path (>>)."
    : context.mode === "filter" ? "Add -f, +pkg, or -pkg; chain another relation or combine expressions with &, |, or >>."
    : context.filter === "-f" ? "Enter a file suffix, such as _test.go."
    : "Enter a package name or full import path; append /... to include its subpackages.";

  return <div className="uir-query-expression min-w-64 flex-1 text-sm">
    <label className="mb-1 block" id={labelId}>Expression</label>
    <MonacoProvider getWorker={getMonacoWorker}>
      <MonacoEditor value={draft} onChange={onChange} language={queryLanguageId} path={path} height="2.75rem"
        beforeMount={registerQueryLanguage} onMount={(editor, monaco) => {
          const currentModel = editor.getModel();
          if (!currentModel) throw new Error(`Query expression editor ${path} has no model`);
          model.current = currentModel;
          monacoRef.current = monaco;
          setRunnerMarker(currentModel, monaco, currentRunError.current);
          disposables.current.forEach((item) => item.dispose());
          editor.updateOptions({
            ariaLabel: "Expression", lineNumbers: "off", glyphMargin: false, folding: false, lineDecorationsWidth: 0,
            renderLineHighlight: "none", overviewRulerLanes: 0, hideCursorInOverviewRuler: true,
            wordBasedSuggestions: "off", tabCompletion: "on", acceptSuggestionOnEnter: "on", wordWrap: "off",
          });
          editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter, () => run.current());
          disposables.current = [
            monaco.languages.registerCompletionItemProvider(queryLanguageId, queryCompletionProvider(monaco, { uri: path, scope: () => scope.current, reportError: setCompletionError })),
            editor.onDidFocusEditorText(() => editor.trigger("uir-query", "editor.action.triggerSuggest", {})),
            editor.onDidChangeCursorPosition((event) => {
              const model = editor.getModel();
              if (model) setContext(queryCompletionContext(model.getValue(), model.getOffsetAt(event.position)));
              if (event.reason === monaco.editor.CursorChangeReason.Explicit) editor.trigger("uir-query", "editor.action.triggerSuggest", {});
            }),
            editor.onDidChangeModelContent(() => {
              const model = editor.getModel();
              const position = editor.getPosition();
              if (model && position) setContext(queryCompletionContext(model.getValue(), model.getOffsetAt(position)));
            }),
          ];
          if (autoFocus) editor.focus();
        }} />
    </MonacoProvider>
    <p id="query-expression-help" className="mt-1 text-xs text-muted-foreground">{hint} Ctrl/Cmd+Space opens suggestions; Ctrl/Cmd+Enter runs the query.</p>
    {completionError && <p role="alert" className="mt-1 text-xs text-destructive">{completionError}</p>}
  </div>;
}
