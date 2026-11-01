import { spawn, type ChildProcess } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:net";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { afterAll, beforeAll, expect, it } from "vitest";

const root = fileURLToPath(new URL("../../..", import.meta.url));
let processHandle: ChildProcess | undefined;
let directory: string;
let baseURL: string;

beforeAll(async () => {
  await mkdir(join(root, ".tmp"), { recursive: true });
  directory = await mkdtemp(join(root, ".tmp", "publication-api-"));
  const reservation = createServer();
  await new Promise<void>((resolve, reject) => {
    reservation.once("error", reject);
    reservation.listen(0, "127.0.0.1", resolve);
  });
  const address = reservation.address();
  if (!address || typeof address === "string") throw new Error("Missing API test port");
  await new Promise<void>((resolve, reject) => reservation.close((error) => error ? reject(error) : resolve()));
  baseURL = `http://127.0.0.1:${address.port}`;
  const child = spawn("go", ["run", "./cmd/uir", "--dsn", join(directory, "uir.db"), "serve", "--host", "127.0.0.1", "--port", String(address.port)], {
    cwd: root, detached: true, stdio: ["ignore", "pipe", "pipe"], env: { ...process.env, UIR_SCHEMA: "" },
  });
  processHandle = child;
  await new Promise<void>((resolve, reject) => {
    let output = "";
    child.once("error", reject);
    child.once("exit", (code) => reject(new Error(`API server exited with ${code}: ${output}`)));
    child.stdout.on("data", (chunk: Buffer) => { output += chunk.toString(); });
    child.stderr.on("data", (chunk: Buffer) => {
      output += chunk.toString();
      if (output.includes("UIR browser listening on")) resolve();
    });
  });
}, 120_000);

afterAll(async () => {
  if (processHandle?.pid && processHandle.exitCode === null) {
    const stopped = new Promise<void>((resolve) => processHandle?.once("exit", () => resolve()));
    process.kill(-processHandle.pid, "SIGTERM");
    await stopped;
  }
  if (directory) await rm(directory, { recursive: true });
});

const rootKey = "example.org/comparison";
const packagePath = `${rootKey}/definitions`;
const document = (path: string, source: string) => ({
  path_key: path, package_path: packagePath, source,
  content_hash: createHash("sha256").update(source).digest("hex"),
  content: { version: 1, package_path: packagePath, symbols: [], occurrences: [], diagnostics: [] },
});

async function publish(publication: Record<string, unknown>) {
  const response = await fetch(`${baseURL}/api/v1/modules/import`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ publication }),
  });
  const body: unknown = await response.json();
  expect(response.status, JSON.stringify(body)).toBe(200);
  expect(body).toHaveProperty("snapshot_id");
  return body as { snapshot_id: string; files: number };
}

it("imports a historical delta and reads the exact selected old and new XML", async () => {
  const first = await publish({
    root_key: rootKey, name: "Comparison", revision: "first", location: { uri: "oipa://example/comparison" },
    options: { historical: true, git_commit: "a".repeat(40), producer: "api-fixture-v1" },
    documents: [document("rule.xml", "<Before/>\n"), document("removed.xml", "<Removed/>\n")],
  });
  const next = await publish({
    root_key: rootKey, name: "Comparison", revision: "next", location: { uri: "oipa://example/comparison" },
    options: { historical: true, git_commit: "b".repeat(40), producer: "api-fixture-v1", base_snapshot_id: first.snapshot_id },
    documents: [document("rule.xml", "<After/>\n")], delta: { deleted_paths: ["removed.xml"] },
  });
  expect(next.files).toBe(1);
  for (const [snapshot, expected] of [[first.snapshot_id, "<Before/>\n"], [next.snapshot_id, "<After/>\n"]]) {
    const response = await fetch(`${baseURL}/api/v1/modules/content?snapshot=${snapshot}&path=rule.xml`);
    expect(response.status).toBe(200);
    expect(await response.json()).toMatchObject({ content: expected });
  }
});
