import { useEffect, useRef, useState } from "react";
import { MonacoEditor, MonacoProvider } from "@flanksource/clicky-ui/monaco";
import type { IDisposable } from "monaco-editor";
import { getMonacoWorker } from "./monaco-workers";
import { queryCompletionContext } from "./query-completion";
import { queryCompletionProvider, queryLanguageId, registerQueryLanguage } from "./query-language";
import { queryScope } from "./query-model";
import type { Route } from "./route";
import "./query-expression.css";

export function QueryExpressionInput({ draft, route, onChange }: { draft: string; route: Route; onChange: (value: string) => void }) {
  const scope = useRef(queryScope(route));
  const disposables = useRef<IDisposable[]>([]);
  const [error, setError] = useState("");
  const [context, setContext] = useState(() => queryCompletionContext(draft, draft.length));
  scope.current = queryScope(route);
  useEffect(() => () => { disposables.current.forEach((item) => item.dispose()); disposables.current = []; }, []);
  const hint = context.mode === "symbol" ? "Type a Go symbol; indexed candidates appear as you type."
    : context.mode === "relation" ? "Choose a relation (<, >, =, :impl, :methods, ~w), set operator (&, |), or path (>>)."
    : context.mode === "filter" ? "Add -f, +pkg, or -pkg; chain another relation or combine expressions with &, |, or >>."
    : context.filter === "-f" ? "Enter a file suffix, such as _test.go."
    : "Enter a package name or full import path; append /... to include its subpackages.";

  return <div className="uir-query-expression min-w-64 flex-1 text-sm">
    <label className="mb-1 block" id="query-expression-label">Expression</label>
    <MonacoProvider getWorker={getMonacoWorker}>
      <MonacoEditor value={draft} onChange={onChange} language={queryLanguageId} path="file:///uir/query/expression.uirq" height="2.75rem"
        beforeMount={registerQueryLanguage} onMount={(editor, monaco) => {
          disposables.current.forEach((item) => item.dispose());
          editor.updateOptions({
            ariaLabel: "Expression", lineNumbers: "off", glyphMargin: false, folding: false, lineDecorationsWidth: 0,
            renderLineHighlight: "none", overviewRulerLanes: 0, hideCursorInOverviewRuler: true,
            wordBasedSuggestions: "off", tabCompletion: "on", acceptSuggestionOnEnter: "on", wordWrap: "off",
          });
          editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter, () => editor.getDomNode()?.closest("form")?.requestSubmit());
          disposables.current = [
            monaco.languages.registerCompletionItemProvider(queryLanguageId, queryCompletionProvider(monaco, () => scope.current, setError)),
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
        }} />
    </MonacoProvider>
    <p id="query-expression-help" className="mt-1 text-xs text-muted-foreground">{hint} Ctrl/Cmd+Space opens suggestions; Ctrl/Cmd+Enter runs the query.</p>
    {error && <p role="alert" className="mt-1 text-xs text-destructive">{error}</p>}
  </div>;
}
