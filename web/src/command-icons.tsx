import { UiCode2, UiLayoutDashboard, UiListTree, UiTable } from "@flanksource/clicky-ui/icons";
import { FileTypeIcon, FolderTypeIcon } from "./file-icons";
import { SymbolIcon } from "./symbol-icons";

export const commandNavigationIcons = {
  overview: UiLayoutDashboard,
  explorer: UiListTree,
  query: UiCode2,
  tasks: UiTable,
} as const;

export function commandModuleIcon() {
  return <FolderTypeIcon open={false} root />;
}

export function commandFileIcon(filename: string) {
  return function CommandFileIcon() {
    return <FileTypeIcon filename={filename} />;
  };
}

export function commandSymbolIcon(nodeType: string) {
  return function CommandSymbolIcon() {
    return <SymbolIcon nodeType={nodeType} />;
  };
}
