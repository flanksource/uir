# Module snapshot browser

Build and serve UIR against a local SQLite database or PostgreSQL:

```sh
pnpm --dir web install
make build
go run ./cmd/uir --dsn ./state/uir.db serve --host localhost --port 8080
```

`--host` defaults to `localhost` and `--port` to `8080`. The database is migrated and checked before the listener opens. The command serves embedded Vite assets and Clicky operations on one origin. For live frontend development, use `go run ./cmd/uir --dsn ./state/uir.db serve --dev`; the command starts the Vite server and proxies its UI while API requests remain on the same origin. Dev mode resolves `@flanksource/clicky-ui` from the sibling `clicky-ui/packages/ui/src` checkout, which must have its dependencies installed. The local `web/node_modules/.bin/vite` executable is also required. Production builds and tests use the pinned package.

The browser lists module roots, their registered checkout locations, and immutable snapshots. The scope picker in the top-right of the header defaults to "All modules"; choosing a root scopes the browser to that root's primary checkout head. Select a checkout to inspect effective sources and node projections; source content is displayed only after its bytes match the indexed content hash. The Query tab runs the [compact PEG language](query.md) against the selected scope: with "All modules", `clicky.StartTask <` reads the primary head of every indexed root and returns recorded callers with their Root column. Add indexes a directory or package path, discovering enclosing and nested Go modules; Reindex refreshes a registered location. The URL retains module, location, snapshot, source, node, the revealed `line` and `column`, search, expression, and offset so a view can be reopened.

The browser uses these Clicky routes:

| Method and route | Purpose |
| --- | --- |
| `GET /api/v1/modules` and `GET /api/v1/modules/by-key/{root}` | Root list and detail. |
| `GET /api/v1/modules/locations?root=...` | Registered checkouts for one root. |
| `GET /api/v1/modules/snapshots?root=...&location=...` | Paged snapshots for one checkout. |
| `GET /api/v1/modules/browse?snapshot=...` | Effective sources and node/call projections for one snapshot. |
| `GET /api/v1/modules/content?snapshot=...&path=...` | Hash-verified bytes for one module-relative source. |
| `GET /api/v1/modules/dependencies?snapshot=...` | Declared go.mod requirements, selected versions, linked target snapshots, and unresolved reasons. |
| `POST /api/v1/modules/query` | Compact PEG query with `args` containing the expression and `root`, `location`, `snapshot`, and `limit` as JSON fields. Omitting scope fields reads the primary head of every indexed root. Returns the [query envelope](query.md#results-and-limits): `operation`, `total`, `matches`, `declarations`, `symbols`, `coverage`, `stages`, and an optional call `path`, with `X-Total-Count` set to `total`. |
| `GET /api/v1/modules/graph?selector=...` | [Call graph](graph.md) around one function or method, selected by `selector` (a symbol expression) or `symbol` (a canonical symbol id), with optional `direction` (`callees`, `callers`, `both`), `depth` (1 through 8), `limit` (1 through 1000), `exclude` (a comma list of [package patterns](graph.md#excluding-packages), `std,builtin,gorm.io/...` when absent, `none` for nothing), `root`, `location`, and `snapshot` query parameters. Returns `roots`, `nodes`, `edges`, `groups`, and `omitted` beside the effective `exclude` patterns, the `packages` reached, `stages`, `warnings`, and `candidates`; an ambiguous selector returns its `candidates` with an empty graph. A pattern that is not an import path, a `/...` pattern or a keyword is a 400 `invalid_query`. |
| `GET /api/v1/modules/suggest` | Active canonical symbol completions for `prefix`, with optional `root`, `location`, `snapshot`, and `limit` query parameters. |
| `POST /api/v1/modules/diff` | [Commit-to-commit symbol diff](diff.md) with `args` containing `<from>..<to>` and `root`, `visibility`, `stat`, `snapshot-from`, and `snapshot-to` as JSON fields. |
| `POST /api/v1/modules/add` and `POST /api/v1/modules/reindex` | Index a new directory or a registered location. |
| `POST /api/v1/modules/refactor/preview` and `POST /api/v1/modules/refactor/apply` | Preview a gopatch rename or move, then apply the reviewed diff. |

URL-encode module paths, checkout paths, and source paths in requests. `/openapi.json` describes the generated API. The removed `/api/v1/project`, `/api/v1/snapshot`, `/api/v1/root`, `/api/v1/source`, and `/api/v1/node` routes are not compatibility aliases.

The content route first verifies that the requested path belongs to the selected snapshot and cannot escape its module location. If the current local file has the saved SHA-256 hash, it returns those bytes. Otherwise, dirty and non-Git snapshots read their stored source blob; clean snapshots can read the pinned Git blob. Every source is hash-verified. If no source matches, the route returns an error instead of displaying current bytes as historical content. No remote repository is fetched automatically.

Explorer Rename and Move require a current, fully indexed local checkout and a gopatch executable. `serve --gopatch-bin` defaults to the Go bin directory; set it to an absolute path when gopatch is installed elsewhere. Symbol renames pass this server's UIR DSN to gopatch, which checks current indexed references in the module and its consumers. Preview sends `snapshot`, `source`, optional `node`, `action`, and either `new-name` or module-relative `destination`; Apply sends the same fields plus `preview-hash`. Apply rejects a changed diff, then reindexes affected registered checkouts. The preview includes changes to workspace and configured consumer modules. The refactor routes accept only local, same-origin requests.

Opening an older database discards its legacy project tables. Back it up first if the old project history matters; no browser path imports it. The [storage design](../storage/README.md) documents the cutover and multi-root behavior.
