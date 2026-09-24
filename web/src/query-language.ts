import type { Monaco } from "@monaco-editor/react";
import type * as monacoEditor from "monaco-editor";
import { suggestModuleSymbols, suggestTypedSelectors } from "./api";
import { queryCompletionContext, querySyntaxCompletions } from "./query-completion";

export const queryLanguageId = "uir-query";
type Scope = { root: string; snapshot: string } | null;

export function registerQueryLanguage(monaco: Monaco): void {
  if (monaco.languages.getLanguages().some((language: { id: string }) => language.id === queryLanguageId)) return;
  monaco.languages.register({ id: queryLanguageId });
  monaco.languages.setMonarchTokensProvider(queryLanguageId, {
    tokenizer: { root: [
      [/\s+/, "white"],
      [/<<[1-8]?|>>[1-8]?|[<>=&|]/, "operator"],
      [/[+-]?(?:pkg|mod|func|field|struct):/, "keyword"],
      [/:impl|:methods|~w|[+-]pkg|-f/, "keyword"],
      [/[()]/, "delimiter.parenthesis"],
      [/[A-Za-z_][A-Za-z0-9_./*?@#$!\-]*/, "identifier"],
    ] },
  });
  monaco.languages.setLanguageConfiguration(queryLanguageId, {
    brackets: [["(", ")"]],
    wordPattern: /[A-Za-z_][A-Za-z0-9_./-]*/g,
  });
}

export function queryCompletionProvider(monaco: Monaco, scope: () => Scope, reportError: (message: string) => void): monacoEditor.languages.CompletionItemProvider {
  return {
    triggerCharacters: [".", "/", "*", "?", "<", ">", "=", ":", "~", "+", "-", "&", "|", "(", ")", " "],
    async provideCompletionItems(model, position, _context, cancellation) {
      const context = queryCompletionContext(model.getValue(), model.getOffsetAt(position));
      const start = model.getPositionAt(context.start);
      const end = model.getPositionAt(context.end);
      const range = { startLineNumber: start.lineNumber, startColumn: start.column, endLineNumber: end.lineNumber, endColumn: end.column };
      if (context.mode === "selector") {
        const selected = scope();
        if (!selected) return { suggestions: [] };
        const sign = /^[+-]/.test(context.prefix) ? context.prefix[0] : "";
        const controller = new AbortController();
        const subscription = cancellation.onCancellationRequested(() => controller.abort());
        try {
          const choices = await suggestTypedSelectors(context.prefix.slice(sign.length), selected.root, selected.snapshot, controller.signal);
          reportError("");
          if (cancellation.isCancellationRequested) return { suggestions: [] };
          return { suggestions: choices.map((choice) => ({
            label: sign + choice, insertText: `${sign}${choice} `, detail: "Indexed selector", documentation: "Select matching symbols in the active snapshot.",
            kind: monaco.languages.CompletionItemKind.Reference, range,
            command: { id: "editor.action.triggerSuggest", title: "Suggest next query token" },
          })) };
        } catch (error) {
          if (controller.signal.aborted) return { suggestions: [] };
          reportError(String(error));
          throw error;
        } finally {
          subscription.dispose();
        }
      }
      if (context.mode !== "symbol") {
        reportError("");
        return { suggestions: querySyntaxCompletions(context, scope()?.root).map((option) => ({
          label: option.label, insertText: option.insert, detail: option.help, documentation: option.help,
          kind: monaco.languages.CompletionItemKind.Operator, range,
          command: { id: "editor.action.triggerSuggest", title: "Suggest next query token" },
        })) };
      }
      if (!context.prefix) {
        reportError("");
        return { suggestions: [{
          label: "package.Type.Method", insertText: "${1:pkg}.${2:Type}.${3:Method}",
          insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
          detail: "Go method", documentation: "Start with a package, receiver type, and method.",
          kind: monaco.languages.CompletionItemKind.Snippet, range,
        }, {
          label: "package.Symbol", insertText: "${1:pkg}.${2:Symbol}",
          insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
          detail: "Go symbol", documentation: "Start with a package and a function or type.",
          kind: monaco.languages.CompletionItemKind.Snippet, range,
        }, ...querySyntaxCompletions(context).map((option) => ({
          label: option.label, insertText: option.insert, detail: option.help, documentation: option.help,
          kind: monaco.languages.CompletionItemKind.Operator, range,
        }))] };
      }
	  if (/^(?:pkg|mod|func|field|struct)$/.test(context.prefix)) {
	    return { suggestions: querySyntaxCompletions(context).map((option) => ({
	      label: option.label, insertText: option.insert, detail: option.help, documentation: option.help,
	      kind: monaco.languages.CompletionItemKind.Operator, range,
	    })) };
	  }
      const selected = scope();
      if (!selected) return { suggestions: [] };
      const controller = new AbortController();
      const subscription = cancellation.onCancellationRequested(() => controller.abort());
      try {
        const symbols = await suggestModuleSymbols(context.prefix, selected.root, selected.snapshot, controller.signal);
        reportError("");
        if (cancellation.isCancellationRequested) return { suggestions: [] };
        return { suggestions: symbols.map((symbol) => ({
          label: {
            label: symbol.query_name.startsWith(`${symbol.package_path}.`)
              ? symbol.query_name.slice(symbol.package_path.length + 1)
              : symbol.query_name.slice(symbol.query_name.lastIndexOf("/") + 1),
            description: symbol.package_path,
          },
          insertText: `${symbol.query_name} `, filterText: `${context.prefix} ${symbol.query_name}`,
          detail: symbol.kind, documentation: symbol.query_name,
          kind: monaco.languages.CompletionItemKind.Reference, range,
          command: { id: "editor.action.triggerSuggest", title: "Suggest next query token" },
        })) };
      } catch (error) {
        if (controller.signal.aborted) return { suggestions: [] };
        reportError(String(error));
        throw error;
      } finally {
        subscription.dispose();
      }
    },
  };
}
