import { Icon } from "@iconify/react/offline";
import defaultFile from "@iconify-icons/vscode-icons/default-file";
import defaultFolder from "@iconify-icons/vscode-icons/default-folder";
import defaultFolderOpened from "@iconify-icons/vscode-icons/default-folder-opened";
import rootFolder from "@iconify-icons/vscode-icons/default-root-folder";
import rootFolderOpened from "@iconify-icons/vscode-icons/default-root-folder-opened";
import go from "@iconify-icons/vscode-icons/file-type-go";
import java from "@iconify-icons/vscode-icons/file-type-java";
import js from "@iconify-icons/vscode-icons/file-type-js";
import json from "@iconify-icons/vscode-icons/file-type-json";
import markdown from "@iconify-icons/vscode-icons/file-type-markdown";
import python from "@iconify-icons/vscode-icons/file-type-python";
import reactts from "@iconify-icons/vscode-icons/file-type-reactts";
import rust from "@iconify-icons/vscode-icons/file-type-rust";
import sql from "@iconify-icons/vscode-icons/file-type-sql";
import typescript from "@iconify-icons/vscode-icons/file-type-typescript";
import yaml from "@iconify-icons/vscode-icons/file-type-yaml";

const fileIcons: Record<string, typeof defaultFile> = {
  go, java, js, jsx: js, json, md: markdown, markdown, py: python,
  rs: rust, sql, ts: typescript, tsx: reactts, yaml, yml: yaml,
};

export function FileTypeIcon({ filename }: { filename: string }) {
  const extension = filename.toLowerCase().split(".").at(-1) ?? "";
  return <Icon icon={filename.startsWith("go.") ? go : fileIcons[extension] ?? defaultFile} className="size-4 shrink-0" aria-hidden="true" />;
}

export function FolderTypeIcon({ open, root = false }: { open: boolean; root?: boolean }) {
  return <Icon icon={root ? open ? rootFolderOpened : rootFolder : open ? defaultFolderOpened : defaultFolder}
    className="size-4 shrink-0" aria-hidden="true" />;
}
