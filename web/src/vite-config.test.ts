import { fileURLToPath } from "node:url";
import { expect, it } from "vitest";
import { loadConfigFromFile } from "vite";

const configFile = fileURLToPath(new URL("../vite.config.ts", import.meta.url));
const clickyRoot = fileURLToPath(new URL("../../../clicky-ui/", import.meta.url));
const source = new URL("packages/ui/src/", `file://${clickyRoot}`);

it("resolves Clicky UI source and styles only in Vite development mode", async () => {
  const development = await loadConfigFromFile(
    { command: "serve", mode: "development" },
    configFile,
  );
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

  for (const environment of [
    { command: "build", mode: "production" },
    { command: "serve", mode: "test" },
  ] as const) {
    const config = await loadConfigFromFile(environment, configFile);
    expect(config?.config.resolve?.alias).toBeUndefined();
  }
});
