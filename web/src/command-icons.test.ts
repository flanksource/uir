import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { commandFileIcon, commandModuleIcon, commandNavigationIcons, commandSymbolIcon } from "./command-icons";
import { FileTypeIcon, FolderTypeIcon } from "./file-icons";
import { SymbolIcon } from "./symbol-icons";

describe("command result icons", () => {
  it("uses the Explorer file icon for each filename", () => {
    for (const filename of ["main.go", "config.yaml", "README.md"]) {
      expect(renderToStaticMarkup(createElement(commandFileIcon(filename)))).toBe(renderToStaticMarkup(createElement(FileTypeIcon, { filename })));
    }
  });

  it("uses the Explorer module and symbol icons", () => {
    expect(renderToStaticMarkup(createElement(commandModuleIcon))).toBe(renderToStaticMarkup(createElement(FolderTypeIcon, { open: false, root: true })));
    for (const nodeType of ["class", "function", "unknown"]) {
      expect(renderToStaticMarkup(createElement(commandSymbolIcon(nodeType)))).toBe(renderToStaticMarkup(createElement(SymbolIcon, { nodeType })));
    }
  });

  it("provides a distinct icon for each navigation result", () => {
    expect(Object.keys(commandNavigationIcons).sort()).toEqual(["explorer", "overview", "query", "tasks"]);
    expect(new Set(Object.values(commandNavigationIcons)).size).toBe(4);
  });
});
