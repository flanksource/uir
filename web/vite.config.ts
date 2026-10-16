import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vitest/config";

const webDir = fileURLToPath(new URL(".", import.meta.url)).replace(/\/$/, "");
const clickyRoot = fileURLToPath(new URL("../../clicky-ui/", import.meta.url));
const escapeRegExp = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
const clickySource = resolve(clickyRoot, "packages/ui/src");

const clickyEntries = {
  "monaco/schema": "monaco-schema.ts",
  "expressions/playground": "expressions/playground.ts",
  "ai/runtime-profile": "runtime-profile.ts",
  components: "components.ts",
  connections: "connections.ts",
  data: "data.ts",
  icons: "icons.ts",
  rpc: "rpc.ts",
  monaco: "monaco.ts",
  "tailwind-preset": "tailwind-preset.ts",
  utils: "utils.ts",
  hooks: "hooks.ts",
  "mdx-editor": "mdx-editor.ts",
  jotai: "jotai.ts",
  comments: "comments.ts",
  devtools: "devtools.ts",
  clicky: "clicky.ts",
  expressions: "expressions.ts",
  profiles: "profiles.ts",
  chat: "chat.ts",
  ai: "ai.ts",
} as const;

// `uir serve --dev` and vitest both run Vite's serve command and resolve clicky-ui from the sibling
// checkout, so local clicky-ui changes need no publish or install; `vite build` uses the published package.
export default defineConfig(({ command }) => {
  const localClicky = command === "serve";
  return {
    plugins: [react(), tailwindcss()],
    resolve: localClicky
      ? {
          alias: [
            {
              find: "@flanksource/clicky-ui/styles.css",
              replacement: resolve(clickySource, "styles/full.css"),
            },
            {
              find: "@flanksource/clicky-ui/mdx-editor.css",
              replacement: resolve(clickySource, "styles/mdx-editor.css"),
            },
            ...Object.entries(clickyEntries).map(([name, entry]) => ({
              find: `@flanksource/clicky-ui/${name}`,
              replacement: resolve(clickySource, entry),
            })),
            {
              find: "@flanksource/clicky-ui",
              replacement: resolve(clickySource, "index.ts"),
            },
          ],
          dedupe: ["react", "react-dom", "react/jsx-runtime", "@tanstack/react-query", "monaco-editor"],
        }
      : undefined,
    server: {
      host: "127.0.0.1",
      fs: { allow: localClicky ? ["..", clickyRoot] : [".."] },
    },
    build: {
      outDir: "dist",
      emptyOutDir: true,
    },
    test: {
      include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
      // The aliased clicky-ui source imports packages installed in the checkout's own node_modules.
      // Node would load those externally with that tree's React; inlining them lets Vite dedupe React.
      server: { deps: { inline: [new RegExp(`^(?!${escapeRegExp(webDir)}/)`)] } },
    },
  };
});
