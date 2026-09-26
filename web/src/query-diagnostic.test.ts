import { describe, expect, it } from "vitest";
import { ApiError } from "./api";
import { queryRunnerErrorRange } from "./query-diagnostic";

describe("query runner diagnostics", () => {
  it("marks the position from a structured API query error", () => {
    expect(queryRunnerErrorRange(new ApiError(400, "invalid_query", "Invalid query syntax",
      "Enter a symbol after &", { line: 1, column: 11 }, "trace-1"), "func:Save &"))
      .toEqual({ startLineNumber: 1, startColumn: 11, endLineNumber: 1, endColumn: 12 });
  });
  it("marks the failing token at the PEG parser position", () => {
    expect(queryRunnerErrorRange(
      'Error: 400 Bad Request: parse compact UIR query "func:Save &": parse error near Primary (line 1 symbol 12 - line 0 symbol 0)',
      "func:Save &",
    )).toEqual({ startLineNumber: 1, startColumn: 11, endLineNumber: 1, endColumn: 12 });
  });

  it("marks the expression when an execution error has no parser position", () => {
    expect(queryRunnerErrorRange('Error: 500 Internal Server Error: symbol lookup failed', "func:Save <"))
      .toEqual({ startLineNumber: 1, startColumn: 1, endLineNumber: 1, endColumn: 12 });
  });

  it("uses a parser position on a later line", () => {
    expect(queryRunnerErrorRange(
      "parse error near Primary (line 2 symbol 3 - line 0 symbol 0)",
      "func:Save <\n&",
    )).toEqual({ startLineNumber: 2, startColumn: 1, endLineNumber: 2, endColumn: 2 });
  });
});
