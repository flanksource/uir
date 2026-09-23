# Snapshot browser

Build and start the browser against a local UIR database:

```sh
pnpm --dir web install
make build
go run ./cmd/uir --dsn "$UIR_DSN" serve --host localhost --port 8080
```

The command serves embedded Vite assets and Clicky operations on one origin. Set `UIR_DSN` to a PostgreSQL DSN for a standalone build. The current published `commons-db` dependency does not include SQLite migration support, so SQLite paths require a workspace with the newer local `commons-db` implementation until that dependency is released. `--host` defaults to `localhost`; `--port` defaults to `8080`. The database is required and checked before the listener opens.

Select a project and snapshot on Overview. Explorer lists roots and sources, Nodes lists indexed symbols with details, and Query runs the [PEG expression language](query.md) against the selected snapshot. The URL keeps project, snapshot, root, source, node, search, and query choices for sharing or reopening. The Reindex form takes a project key and local path, then moves to the published snapshot after a successful run.

Source content is resolved from the saved root reference. A Git or Git submodule root with a local checkout and revision reads the file from that revision, even when the working tree has changed. A directory root, or a root without a Git revision, reads its current local file without checking a content hash. A remote-only Git root shows the repository link, revision, and path; it does not fetch the repository. Missing or invalid local references surface an error.

The read API is exposed through Clicky's `/api/v1/project`, `/api/v1/snapshot`, `/api/v1/root`, `/api/v1/source`, and `/api/v1/node` operations. Source content is `GET /api/v1/source/{id}/content`; queries and reindexing use the existing project actions. `/openapi.json` describes the operations.

For frontend development from the repository root, use `go run ./cmd/uir --dsn "$UIR_DSN" serve --dev`. The command starts the Vite development server and proxies its UI through the same HTTP origin; API requests still reach Clicky. The Vite executable comes from `web/node_modules/.bin/vite`.
