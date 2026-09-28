import { useState, type FormEvent } from "react";
import { Button, Modal } from "@flanksource/clicky-ui/components";
import { CodeDiff } from "@flanksource/clicky-ui/data";
import { applyModuleRefactor, errorMessage, previewModuleRefactor, type ModuleNode, type ModuleRefactorPreview, type ModuleRefactorRequest, type ModuleSource } from "./api";
import { ErrorMessage, Field, Muted, TextInput } from "./ui";

export function ExplorerRefactorDialog({ action, snapshot, source, node, onClose, onApplied }: {
  action: "rename" | "move";
  snapshot: string;
  source: ModuleSource;
  node?: ModuleNode;
  onClose: () => void;
  onApplied: (snapshot: string) => void;
}) {
  const [value, setValue] = useState("");
  const [pending, setPending] = useState<"preview" | "apply" | null>(null);
  const [error, setError] = useState("");
  const [preview, setPreview] = useState<{ request: ModuleRefactorRequest; result: ModuleRefactorPreview }>();
  const target = node ? node.identifier.field || node.identifier.method || node.identifier.type : source.path;
  const request = (): ModuleRefactorRequest => ({ snapshot, source: source.id, ...(node ? { node: node.id } : {}), action,
    ...(action === "rename" ? { newName: value.trim() } : { destination: value.trim() }) });

  async function showPreview(event: FormEvent) {
    event.preventDefault();
    setPending("preview");
    setError("");
    setPreview(undefined);
    try {
      const selected = request();
      setPreview({ request: selected, result: await previewModuleRefactor(selected) });
    } catch (reason) {
      setError(errorMessage(reason));
    } finally {
      setPending(null);
    }
  }

  async function apply() {
    if (!preview) return;
    setPending("apply");
    setError("");
    try {
      const result = await applyModuleRefactor(preview.request, preview.result.preview_hash);
      setPreview(undefined);
      if (result.index_error) {
        setError(`Gopatch applied the change, but UIR could not refresh its index: ${result.index_error}`);
        return;
      }
      const updated = result.snapshots.find((item) => item.location === source.location);
      if (!updated) throw new Error(`Gopatch applied the change, but no refreshed snapshot was returned for ${source.location}`);
      onApplied(updated.snapshot_id);
    } catch (reason) {
      setError(errorMessage(reason));
    } finally {
      setPending(null);
    }
  }

  return <Modal open onClose={() => { if (pending === null) onClose(); }} title={`${action === "rename" ? "Rename" : "Move"} ${node ? "symbol" : "file"}`} size="full"
    footer={<div className="flex justify-end gap-2"><Button type="button" variant="outline" onClick={onClose} disabled={pending !== null}>Close</Button>
      <Button type="button" onClick={apply} disabled={!preview || pending !== null} loading={pending === "apply"}>Apply</Button></div>}>
    <form className="flex flex-col gap-3" onSubmit={showPreview}>
      <Muted>Selected: {target} in {source.path}</Muted>
      <Field label={action === "rename" ? node ? "New Go identifier" : "New .go file name" : "Destination .go path within this module"}>
        <TextInput required value={value} onChange={(event) => { setValue(event.target.value); setPreview(undefined); setError(""); }} />
      </Field>
      <div><Button type="submit" disabled={!value.trim() || pending !== null} loading={pending === "preview"}>Preview changes</Button></div>
    </form>
    <ErrorMessage error={error} />
    {preview && <div className="mt-4 flex flex-col gap-3">
      <strong>{preview.result.files.length} files will change</strong>
      <CodeDiff unified={preview.result.diff} language="go" />
    </div>}
  </Modal>;
}
