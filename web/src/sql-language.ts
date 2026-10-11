import type { Monaco } from "@monaco-editor/react";
import type * as monacoEditor from "monaco-editor";
import { getQueryCatalog, suggestTypedSelectors } from "./api";
import { sqlCompletions } from "./sql-completion";

export async function completeSQL(monaco: Monaco, model: monacoEditor.editor.ITextModel, position: monacoEditor.Position,
  scope: { root: string; snapshot: string } | null, cancellation: monacoEditor.CancellationToken): Promise<monacoEditor.languages.CompletionList> {
  const controller = new AbortController();
  const subscription = cancellation.onCancellationRequested(() => controller.abort());
  try {
    const catalog = await getQueryCatalog(controller.signal);
    const context = sqlCompletions(model.getValue(), model.getOffsetAt(position), catalog);
    if (context.selector && scope) {
      const choices = await suggestTypedSelectors(context.selector, scope.root, scope.snapshot, controller.signal, "sql");
      context.choices.push(...choices.map((choice) => ({ label: choice, insert: `${choice} `, detail: "Indexed selector" })));
    }
    if (cancellation.isCancellationRequested) return { suggestions: [] };
    const start = model.getPositionAt(context.start);
    const end = model.getPositionAt(context.end);
    const range = { startLineNumber: start.lineNumber, startColumn: start.column, endLineNumber: end.lineNumber, endColumn: end.column };
    return { suggestions: context.choices.map((choice) => ({ label: choice.label, insertText: choice.insert, detail: choice.detail,
      kind: monaco.languages.CompletionItemKind.Reference, range })) };
  } catch (error) {
    if (controller.signal.aborted) return { suggestions: [] };
    throw error;
  } finally { subscription.dispose(); }
}
