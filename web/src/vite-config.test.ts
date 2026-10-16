import { fileURLToPath } from "node:url";
import { expect, it } from "vitest";
import { loadConfigFromFile } from "vite";

const configFile = fileURLToPath(new URL("../vite.config.ts", import.meta.url));
const clickyRoot = fileURLToPath(new URL("../../../clicky-ui/", import.meta.url));
const source = new URL("packages/ui/src/", `file://${clickyRoot}`);

it.each(["development", "test"])("resolves Clicky UI source and styles from the sibling checkout when Vite serves %s", async (mode) => {
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

it("builds against the published Clicky UI package", async () => {
  const production = await loadConfigFromFile({ command: "build", mode: "production" }, configFile);
  expect(production?.config.resolve?.alias).toBeUndefined();
});
