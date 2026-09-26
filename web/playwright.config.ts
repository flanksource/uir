import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  outputDir: "../.tmp/playwright-results",
  use: {
    baseURL: "http://127.0.0.1:5179",
  },
  webServer: {
    command: "pnpm dev --port 5179 --strictPort",
    url: "http://127.0.0.1:5179",
    reuseExistingServer: false,
  },
});
