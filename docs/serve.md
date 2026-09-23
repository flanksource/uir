# Module snapshot browser

Build and serve UIR against a local SQLite database or PostgreSQL:

```sh
pnpm --dir web install
make build
go run ./cmd/uir --dsn ./state/uir.db serve --host localhost --port 8080
```

`--host` defaults to `localhost` and `--port` to `8080`. The database is migrated and checked before the listener opens. The command serves embedded Vite assets and Clicky operations on one origin. For live frontend development, use `go run ./cmd/uir --dsn ./state/uir.db serve --dev`; the command starts the Vite server and proxies its UI while API requests remain on the same origin. The local `web/node_modules/.bin/vite` executable is required in dev mode.

The browser lists module roots, their registered checkout locations, and immutable snapshots. Select a root and checkout to inspect effective sources and node projections; source content is displayed only after its bytes match the indexed content hash. The Query tab runs the [PEG language](query.md) against the selected scope. Add indexes a directory or package path, discovering enclosing and nested Go modules; Reindex refreshes a registered location. The URL retains module, location, snapshot, source, node, search, expression, and offset so a view can be reopened.

The browser uses these Clicky routes:

| Method and route | Purpose |
| --- | --- |
| `GET /api/v1/modules` and `GET /api/v1/modules/by-key/{root}` | Root list and detail. |
| `GET /api/v1/modules/locations?root=...` | Registered checkouts for one root. |
| `GET /api/v1/modules/snapshots?root=...&location=...` | Paged snapshots for one checkout. |
| `GET /api/v1/modules/browse?snapshot=...` | Effective sources and node/call projections for one ready snapshot. |
| `GET /api/v1/modules/content?snapshot=...&path=...` | Hash-verified bytes for one module-relative source. |
| `POST /api/v1/modules/query` | PEG query with `args` containing the expression and scope flags as JSON fields. |
| `POST /api/v1/modules/add` and `POST /api/v1/modules/reindex` | Index a new directory or a registered location. |

URL-encode module paths, checkout paths, and source paths in requests. `/openapi.json` describes the generated API. The removed `/api/v1/project`, `/api/v1/snapshot`, `/api/v1/root`, `/api/v1/source`, and `/api/v1/node` routes are not compatibility aliases.

The content route first verifies that the requested path belongs to the selected snapshot and cannot escape its module location. If the current local file has the saved SHA-256 hash, it returns those bytes. Otherwise, when the snapshot has a pinned Git revision, it reads the corresponding blob and verifies the same hash. If neither source matches, it returns an error rather than displaying current bytes as historical content. A removed checkout or unavailable Git object may therefore leave the node projection browsable while source text cannot be retrieved. No remote repository is fetched automatically.

Opening an older database discards its legacy project tables. Back it up first if the old project history matters; no browser path imports it. The [storage design](../storage/README.md) documents the cutover and multi-root behavior.
