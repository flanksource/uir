import { useEffect, useMemo, useState } from "react";
import { Button, JsonSchemaForm, type JsonSchemaObject } from "@flanksource/clicky-ui/components";
import { createConnection, deleteConnection, errorMessage, testConnection, updateConnection, type ConnectionInput, type ConnectionType, type ConnectionView } from "./api";
import { ErrorMessage, Field, Muted, PanelForm, Row } from "./ui";

const credentialFields = ["url", "username", "password", "certificate"] as const;

function initialValue(connection?: ConnectionView): Record<string, unknown> {
  if (!connection) return { name: "" };
  return {
    name: connection.name,
    ...(connection.url ? { url: connection.url } : {}),
    ...(connection.username ? { username: connection.username } : {}),
    ...(connection.password ? { password: connection.password } : {}),
    ...(connection.certificate ? { certificate: connection.certificate } : {}),
    ...(connection.properties ? { properties: connection.properties } : {}),
    insecure_tls: connection.insecure_tls,
  };
}

function connectionInput(value: Record<string, unknown>, type: ConnectionType, touched: Set<string>, existing?: ConnectionView): ConnectionInput {
  const input: ConnectionInput = { name: String(value.name ?? "").trim(), type };
  for (const field of credentialFields) {
    if (!existing || touched.has(field)) {
      const candidate = value[field];
      if (typeof candidate === "string") input[field] = candidate;
    }
  }
  if (value.properties && typeof value.properties === "object") input.properties = value.properties as Record<string, string>;
  if (typeof value.insecure_tls === "boolean") input.insecure_tls = value.insecure_tls;
  return input;
}

export function ConnectionEditor({ schemas, connection, onSaved, onDeleted }: {
  schemas: Record<ConnectionType, JsonSchemaObject>;
  connection?: ConnectionView;
  onSaved: (connection: ConnectionView) => void;
  onDeleted: () => void;
}) {
  const [type, setType] = useState<ConnectionType>(connection?.type ?? "postgres");
  const [value, setValue] = useState<Record<string, unknown>>(() => initialValue(connection));
  const [touched, setTouched] = useState<Set<string>>(new Set());
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  useEffect(() => {
    setType(connection?.type ?? "postgres");
    setValue(initialValue(connection));
    setTouched(new Set());
    setError("");
    setMessage("");
  }, [connection?.id]);
  const formSchema = useMemo(() => {
    const source = schemas[type];
    if (!source) return undefined;
    if (!connection) return source;
    const copy = structuredClone(source);
    copy.required = copy.required?.filter((field) => !credentialFields.some((secret) => secret === field && connection[`has_${secret}`]));
    return copy;
  }, [schemas, type, connection]);
  const draftLiteral = credentialFields.some((field) => {
    const raw = value[field];
    return typeof raw === "string" && raw !== "" && (field === "url" ? raw.includes("@") || /[?&][^=&]*(?:password|pwd|token|secret|signature|credential|api[_-]?key)[^=&]*=/i.test(raw) : (field === "password" || field === "certificate" || (field === "username" && type === "s3")) && !/^(op|secret|configmap|helm|serviceaccount):\/\//.test(raw));
  });

  async function save(event: React.FormEvent) {
    event.preventDefault();
    setPending(true); setError(""); setMessage("");
    try {
      const input = connectionInput(value, type, touched, connection);
      const saved = connection ? await updateConnection(connection.id, input) : await createConnection(input);
      onSaved(saved);
      setMessage("Connection saved. Use Test connection to check its credentials and endpoint.");
    } catch (reason) { setError(errorMessage(reason)); }
    finally { setPending(false); }
  }

  async function probe() {
    if (!connection) return;
    setPending(true); setError(""); setMessage("");
    try { await testConnection(connection.id); setMessage("Connection test passed."); }
    catch (reason) { setError(errorMessage(reason)); }
    finally { setPending(false); }
  }

  async function remove() {
    if (!connection || !window.confirm(`Delete connection ${connection.name}?`)) return;
    setPending(true); setError("");
    try { await deleteConnection(connection.id); onDeleted(); }
    catch (reason) { setError(errorMessage(reason)); }
    finally { setPending(false); }
  }

  return <PanelForm onSubmit={save}>
    <h2>{connection ? `Edit ${connection.name}` : "New connection"}</h2>
    <Field label="Connection type"><select className="h-9 rounded-md border border-input bg-background px-3 text-sm" value={type} onChange={(event) => {
      setType(event.target.value as ConnectionType); setValue({ name: value.name ?? "" }); setTouched(new Set());
    }}>{Object.keys(schemas).sort().map((option) => <option key={option} value={option}>{option.replaceAll("_", " ")}</option>)}</select></Field>
    {formSchema && <JsonSchemaForm schema={formSchema} value={value} onChange={(next) => {
      const changed = credentialFields.filter((field) => next[field] !== value[field]);
      if (changed.length) setTouched((current) => new Set([...current, ...changed]));
      setValue(next);
    }} applyDefaults={false} showPreferencesMenu={false} />}
    {(connection?.literal_credentials || draftLiteral) && <div role="status" className="rounded-md border border-amber-500/50 bg-amber-500/10 p-3 text-sm">Literal credentials are stored in the UIR database. Use a secret reference when possible.</div>}
    {connection && credentialFields.filter((field) => connection[`has_${field}`] && !connection[field]).map((field) => <Row key={field}>
      <Muted>Saved {field.replaceAll("_", " ")} is configured; leave its field blank to keep it.</Muted>
      <Button type="button" variant="outline" onClick={() => { setValue((current) => ({ ...current, [field]: "" })); setTouched((current) => new Set([...current, field])); }}>Clear saved {field}</Button>
    </Row>)}
    <Row><Button type="submit" loading={pending}>Save connection</Button>
      {connection && <Button type="button" variant="outline" disabled={pending} onClick={probe}>Test connection</Button>}
      {connection && <Button type="button" variant="outline" disabled={pending} onClick={remove}>Delete connection</Button>}
    </Row>
    <ErrorMessage error={error} />{message && <Muted>{message}</Muted>}
  </PanelForm>;
}
