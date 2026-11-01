import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { afterEach, beforeEach, expect, it } from "vitest";
import { loadConfigFromFile } from "vite";

const configFile = fileURLToPath(new URL("../vite.config.ts", import.meta.url));

let clickyRoot: string;

beforeEach(() => {
  clickyRoot = `${mkdtempSync(join(tmpdir(), "clicky-ui-"))}/`;
  process.env.CLICKY_UI_ROOT = clickyRoot;
});

afterEach(() => {
  delete process.env.CLICKY_UI_ROOT;
  rmSync(clickyRoot, { recursive: true, force: true });
});

it.each(["development", "test"])("resolves Clicky UI source and styles from the sibling checkout when Vite serves %s", async (mode) => {
  mkdirSync(join(clickyRoot, "packages/ui/src"), { recursive: true });
  const source = new URL("packages/ui/src/", pathToFileURL(clickyRoot));
  const development = await loadConfigFromFile({ command: "serve", mode }, configFile);
  expect(development?.config.resolve?.alias).toEqual(
    expect.arrayContaining([
      {
        find: "@flanksource/clicky-ui/monaco/schema",
        replacement: fileURLToPath(new URL("monaco-schema.ts", source)),
      },
      {
        find: "@flanksource/clicky-ui/monaco",
        replacement: fileURLToPath(new URL("monaco.ts", source)),
      },
      {
        find: "@flanksource/clicky-ui/components",
        replacement: fileURLToPath(new URL("components.ts", source)),
      },
      {
        find: "@flanksource/clicky-ui/connections",
        replacement: fileURLToPath(new URL("connections.ts", source)),
      },
      {
        find: "@flanksource/clicky-ui/styles.css",
        replacement: fileURLToPath(new URL("styles/full.css", source)),
      },
      {
        find: "@flanksource/clicky-ui",
        replacement: fileURLToPath(new URL("index.ts", source)),
      },
    ]),
  );
  expect(development?.config.server?.fs?.allow).toContain(clickyRoot);
});

it("serves the published Clicky UI package when there is no sibling checkout", async () => {
  const development = await loadConfigFromFile({ command: "serve", mode: "test" }, configFile);
  expect(development?.config.resolve?.alias).toBeUndefined();
  expect(development?.config.server?.fs?.allow).not.toContain(clickyRoot);
});

it("builds against the published Clicky UI package", async () => {
  mkdirSync(join(clickyRoot, "packages/ui/src"), { recursive: true });
  const production = await loadConfigFromFile({ command: "build", mode: "production" }, configFile);
  expect(production?.config.resolve?.alias).toBeUndefined();
});
