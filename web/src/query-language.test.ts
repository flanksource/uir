import type { Monaco } from "@monaco-editor/react";
import type { CancellationToken, editor } from "monaco-editor";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { suggestModuleSymbols, suggestTypedSelectors } from "./api";
import { queryCompletionProvider } from "./query-language";

vi.mock("./api", () => ({ suggestModuleSymbols: vi.fn(), suggestTypedSelectors: vi.fn() }));

const monaco = { languages: {
  CompletionItemKind: { Operator: 11, Reference: 17, Snippet: 27 },
  CompletionItemInsertTextRule: { InsertAsSnippet: 4 },
} } as unknown as Monaco;
const cancellation = {
  isCancellationRequested: false,
  onCancellationRequested: () => ({ dispose() {} }),
} as unknown as CancellationToken;

function complete(expression: string) {
  const model = {
    getValue: () => expression,
    getOffsetAt: (position: { column: number }) => position.column - 1,
    getPositionAt: (offset: number) => ({ lineNumber: 1, column: offset + 1 }),
  } as unknown as editor.ITextModel;
  const reportError = vi.fn();
  const provider = queryCompletionProvider(monaco, () => ({ root: "example.org/shop", snapshot: "snapshot-1" }), reportError);
  return { result: provider.provideCompletionItems!(model, { lineNumber: 1, column: expression.length + 1 } as never, {} as never, cancellation), reportError };
}

describe("query Monaco completion", () => {
  beforeEach(() => vi.clearAllMocks());

  it("completes a symbol after a path operator using the active snapshot", async () => {
    vi.mocked(suggestModuleSymbols).mockResolvedValue([{
      id: "symbol-1", module_key: "example.org/shop", package_path: "example.org/shop/store",
      kind: "method", name: "Save", query_name: "example.org/shop/store.Store.Save",
      visibility: "exported", parameter_types: [],
    }, {
      id: "symbol-2", module_key: "example.org/shop", package_path: "example.org/shop/store",
      kind: "method", name: "Start", query_name: "example.org/shop/store.Store.Start",
      visibility: "exported", parameter_types: [],
    }]);
    const { result } = complete("main.* >> store.St");
    const suggestions = (await result)?.suggestions ?? [];
    expect(suggestModuleSymbols).toHaveBeenCalledWith("store.St", "example.org/shop", "snapshot-1", expect.any(AbortSignal));
    expect(suggestions).toHaveLength(2);
    expect(suggestions).toContainEqual(expect.objectContaining({
      label: { label: "Store.Save", description: "example.org/shop/store" },
      insertText: "example.org/shop/store.Store.Save ",
      filterText: "store.St example.org/shop/store.Store.Save",
      range: { startLineNumber: 1, startColumn: 11, endLineNumber: 1, endColumn: 19 },
    }));
  });

  it("offers operators after a symbol without requesting symbol candidates", async () => {
    const { result } = complete("store.Store.Save ");
    expect((await result)?.suggestions.map((item) => item.label)).toEqual(expect.arrayContaining(["+pkg:", "-pkg:", "<", ">", "=", "&", "|", ">>"]));
    expect(suggestModuleSymbols).not.toHaveBeenCalled();
  });

  it("completes a relative package within the active module snapshot", async () => {
    vi.mocked(suggestTypedSelectors).mockResolvedValue(["pkg:example.org/shop:store"]);
    const { result } = complete("func:Run > pkg:example.org/shop:st");
    expect((await result)?.suggestions).toContainEqual(expect.objectContaining({
      label: "pkg:example.org/shop:store", insertText: "pkg:example.org/shop:store ",
    }));
    expect(suggestTypedSelectors).toHaveBeenCalledWith("pkg:example.org/shop:st", "example.org/shop", "snapshot-1", expect.any(AbortSignal));
  });
});
