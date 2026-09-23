# UIR snapshot browser and serve command

Add `uir --dsn <database> serve` with a Vite-built React UI, Clicky API routes, and Clicky UI components. Browse saved snapshots, query a selected snapshot, and start reindexing from the browser.

## Backend

- Add `serve --host localhost --port 8080 [--dev]`, use the existing command runtime and database, mark the process command local-only, and serve embedded production assets or a managed Vite proxy.
- Register paged Clicky snapshot, root, source, and node entities; retain the project query and reindex actions. Expose source content through a read-only source action and give query rows stable IDs for navigation.
- Read source from the recorded Git revision in a local checkout when available. Otherwise read the recorded local file path without a hash check. For a remote-only Git root, show its repository URI, revision, and path without fetching it.

## Frontend

- Adapt arch-unit overview, explorer, nodes, and query layouts to UIR snapshots with Clicky UI shell, tables, filters, and tabs.
- Keep project, snapshot, root, source, node, and query state in the URL. Offer reindexing and navigate to the new published snapshot on success.
- Package Vite assets in the standalone Go binary, add pnpm build and CI wiring, and document production and development usage.

## Verification

- Add Ginkgo command, storage, HTTP, source-resolution, and reindex coverage, plus API integration tests under `packages/api/test` and focused Vitest UI tests.
- Run frontend lint/build/test, `make lint`, `make build`, `go test ./...`, and an agent-browser smoke test of navigation, source viewing, query, and reindex flows.

Arch-unit analysis and code generation are outside this first version. Preserve unrelated working-tree changes.
