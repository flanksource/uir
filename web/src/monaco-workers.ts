import EditorWorker from "monaco-editor/editor/editor.worker?worker";

export function getMonacoWorker(): Worker {
  return new EditorWorker();
}
