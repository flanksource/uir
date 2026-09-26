import type { IRange } from "monaco-editor";
import { ApiError } from "./api";

export function queryRunnerErrorRange(error: string | ApiError, expression: string): IRange {
  const lines = expression.split(/\r\n|\n|\r/);
  const position = error instanceof ApiError ? [error.context.line, error.context.column]
    : /parse error near[^\n]*\(line (\d+) symbol (\d+) - line \d+ symbol \d+\)/.exec(error)?.slice(1, 3);
  if (position && position.every((value) => Number.isInteger(Number(value)))) {
    const lineNumber = Number(position[0]);
    if (lineNumber >= 1 && lineNumber <= lines.length) {
      const maxColumn = lines[lineNumber - 1].length + 1;
      const column = Math.min(Math.max(Number(position[1]), 1), maxColumn);
      const startColumn = column === maxColumn && maxColumn > 1 ? column - 1 : column;
      return {
        startLineNumber: lineNumber,
        startColumn,
        endLineNumber: lineNumber,
        endColumn: Math.min(startColumn + 1, maxColumn),
      };
    }
  }
  return {
    startLineNumber: 1,
    startColumn: 1,
    endLineNumber: lines.length,
    endColumn: lines[lines.length - 1].length + 1,
  };
}
