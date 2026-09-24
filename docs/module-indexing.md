# Module-root indexing and queries

The root-level `uir` commands index Go modules. A module path from `go.mod` is the stable root key; a canonical filesystem path is a location of that root, not its identity. Two clones or worktrees declaring the same module path share a root but have independent location heads. The first registered location remains the primary location, and `uir query` reads its head by default.

```sh
uir add .
uir list
uir get example.org/service
uir reindex .
uir query 'service.Run =' --root example.org/service
uir query 'service.Run <' --root example.org/service --location ../service-feature
uir query 'service.Run =' --snapshot 6f8b2c6e-79af-4b59-8736-e45696f0c546
```

`uir add [path]` defaults to `.` and indexes immediately. It discovers every `go.mod` beneath the selected directory, including nested modules without a separate `.git` marker. If the path is a package directory, it infers the nearest enclosing `go.mod` and scans that whole module, not just the package directory. Source files beneath a nested module are excluded from the parent module's scan. A nested location stores its nearest parent location and mount path; `.gitmodules` provenance marks a declared submodule. Symlinked input paths are canonicalized, while directory symlinks are not traversed during discovery.

If an enclosing `go.work` references `use` paths outside the selected directory, an interactive `add` asks whether to include them. A non-interactive invocation must specify `--include-workspace-uses` or `--no-workspace-uses`; the mutually exclusive flags make the scope explicit. Discovery resolves every `go.work use` path before applying that choice, so a missing or invalid path currently fails even with `--no-workspace-uses`. `uir reindex [path]` accepts only previously registered module locations and uses the same extractor. `--include-tests` includes `_test.go` files; `--force` re-extracts every file, fails if a stored document differs from the fresh extraction under the same indexer version, and publishes a new snapshot even when the content is unchanged.

Without `--dsn`, the CLI stores SQLite at `~/.config/uir/uir.db` and creates the directory if needed. Existing databases at other paths are not moved; select one explicitly with `--dsn`. Plain `.db` paths, `sqlite://` URLs, and PostgreSQL DSNs remain supported. The database owner is `storage.UirDB`; its embedded HCL is applied to the selected target before queries or indexing.

## Module API

`uir serve` exposes the root-level operations as structured JSON at `GET /api/v1/modules`, `GET /api/v1/modules/by-key/{root}`, `POST /api/v1/modules/add`, `POST /api/v1/modules/reindex`, and `POST /api/v1/modules/query`. `GET /api/v1/modules/locations?root=...` lists registered checkouts; `GET /api/v1/modules/snapshots?root=...&location=...` pages through one checkout's immutable snapshots. `GET /api/v1/modules/browse?snapshot=...` returns effective sources and the node projections read from their documents; `GET /api/v1/modules/content?snapshot=...&path=...` returns hash-verified source bytes. URL-encode full module paths and checkout paths in request parameters. Positional CLI arguments are the JSON `args` array; flags are JSON fields, for example `{"args":["service.Run <"],"root":"example.org/service"}` for a query. HTTP add does not prompt: when an enclosing `go.work` has external uses, send `include-workspace-uses` or `no-workspace-uses` explicitly. These operations use Clicky's typed result path, so HTTP clients receive data rows rather than captured terminal tables. Indexing operations appear as `module-index` runs at `GET /api/v1/tasks` and in the browser's Tasks view. The browser's Cmd+K palette searches module roots plus files and symbols in the selected snapshot.

`GET /api/v1/modules/heads` lists each registered checkout's current head and effective source files without symbol payloads. The Explorer Files pane groups those heads by module, adding checkout rows when a module has multiple locations, then by source folder. Selecting a file switches the active module, checkout, and snapshot together. File and folder rows use bundled VS Code icons.

The sidebar's System details dropdown shows the frontend build version, backend binary version, and live database type, version, location, and size. `GET /api/v1/system/info` provides the backend and database fields without exposing connection credentials. SQLite size is the live page count multiplied by page size; PostgreSQL size is `pg_database_size(current_database())`. `make build VERSION=...` and `make install VERSION=...` embed the same version in the frontend and backend.

## Storage hierarchy

```text
ModuleRoot (go.mod module path)
├── ModulePrimary ── primary ModuleLocation
├── ModuleLocation (canonical checkout path, parent location and mount)
│   └── ModuleLocationHead ── latest published ModuleSnapshot for that checkout
└── ModuleSnapshot (immutable, optional base snapshot)
    ├── SourceDelta (set revision or delete path)
    │   └── SourceRevision (path, content hash, and package)
    │       └── Document (typed or syntax facts for that revision under one package input hash)
    └── PackageCoverage (input hash selecting each package's documents)
```

`modules.root_key` is the full module path; `name` is its basename. `locations` uses `(root_id, canonical_path)` as its unique physical identity. A location head is scoped by root and location, references a snapshot of that same location, and carries a monotonically increasing version. The primary row is set once, when the first location is registered; adding a second location does not redirect default queries.

Each new location bases its first snapshot on the current primary head, if one exists. A later run at the same location bases on that location's previous head, including after a branch switch. A run publishes a child snapshot unless its Git revision, content-set hash, configuration hash, and context hash all equal the base snapshot's; `--force` always publishes. The content set is the included `.go` files plus the dependency manifests: the module's `go.mod` and `go.sum` under their module-relative paths, and the `go.work` and `go.work.sum` of the enclosing workspace, wherever it sits, under the fixed keys `go.work` and `go.work.sum`. The manifests enter only the hash; they are never stored as source revisions, so a dependency bump publishes a snapshot with zero source deltas that reuses every document. The configuration hash covers the indexer version, test inclusion, and the build variant that `go env`, run in the module directory, reports for the toolchain: `GOOS`, `GOARCH`, `CGO_ENABLED`, `GOVERSION`, and an empty build-tag list. A new variant is a new configuration, a new snapshot, and new documents; if `go env` fails, indexing fails. The context hash digests every package input hash, so a different context with identical bytes also publishes. Changes to other non-Go files that do not change the Git revision are outside the snapshot trigger.

A snapshot stores only changed path operations. `set` points to a `SourceRevision` keyed by root, path, content hash, and package path. `delete` is a tombstone and has no revision ID. Unchanged content reuses a revision row across locations and snapshots. The extracted facts live in a document keyed by root, path, and the package input hash, which digests the configuration hash (indexer version, test inclusion, and build variant), the package path, every `(path, content hash)` of the package's files, and the export-shape hash of each direct import. A package that type-checks under `go/packages` gets typed documents (`indexed`, or `partial` when it has type errors) with canonical symbol ids and `symbol_postings`; a package that cannot be type-checked falls back to `syntax` documents holding the AST extractor's declarations and call locators, without ids or postings. Each snapshot records one `package_coverage` row per package with that input hash, and a `context_hash` over all of them. `EffectiveSources` walks the base chain newest-first, takes the first operation per path, validates the root and revision path, and loads revisions in bounded batches; `ActiveDocuments` then selects each path's document by its package's input hash. Queries reconstruct the effective view; they do not assume every snapshot physically contains all source rows. Because the input hash covers the whole package, editing, adding, or removing one file gives every file of its package a new document, while other packages keep theirs.

Discovery, hashing, and type-checked extraction finish before the publication transaction opens; revision, symbol, document, and posting writes happen inside it. All modules discovered in one `IndexModules` call publish in that transaction: a malformed Go file in a later nested module rolls back earlier root, location, revision, document, snapshot, and head writes from the call. A snapshot row is written only in that transaction, after its documents, and together with its deltas and package coverage, before its location head is advanced; there is no building or failed snapshot state. Each snapshot records `worktree_state`: `clean` when `git status --porcelain` under the module is empty, `dirty` otherwise, and `unknown` with an empty revision outside Git. Existing heads use a version-checked update; a concurrent writer changing the version makes publication fail instead of silently overwriting it. External `go.work` uses selected during the same `uir add` invocation are indexed in a separate `IndexModules` call and therefore a separate transaction; successful primary-path publication is not rolled back if that later call fails.

## Query scope and call references

The compact PEG grammar accepts a Go qualified symbol followed by `<`, `>`, `=`, `<<n`, `:impl`, `:methods`, or `~w`; filters, set operators, and bounded `>>` call paths compose these operations. See the [query guide](query.md). `--root` selects a module path, `--location` selects one registered checkout, and `--snapshot` selects one historical immutable snapshot. `--location` and `--snapshot` cannot be combined. Without an explicit location or snapshot, queries read the primary head of each selected root.

The symbol operations resolve a spelling to canonical symbols and read the postings of those symbols in each selected snapshot's active documents. One identity declared at several heads is one target whose declarations are listed separately; if a spelling matches several identities in scope, the command returns candidate rows with their full query name, root, checkout, source position, and snapshot instead of choosing one arbitrarily. Select `--location`, `--snapshot`, or a full import path to disambiguate. Every result lists the packages in scope that are `partial`, `syntax`, or `excluded`, because those hold no or only some proven facts.

The extractor type-checks each package with `go/packages` under the recorded build variant and records only what the type checker proves; a package that cannot be type-checked falls back to `syntax` coverage from the AST extractor, and a file that build constraints exclude gets an `excluded` document. It skips hidden, underscore-prefixed, `vendor`, and `testdata` directories and excludes tests unless requested. A long chain of tiny snapshots keeps writes small but makes effective-view reads traverse more ancestors; compaction and retention are not implemented. PostgreSQL is appropriate for shared writers; SQLite remains a local single-process store.

## Legacy cutover

The earlier `uir project ...` command, project browser API, and project GORM tables are removed. Opening an existing database discards those old tables after applying the module schema; it does not backfill project snapshots. Back up the database before opening it with this version if that history is needed. See [storage design](../storage/README.md) for the exact table allowlist and dependency checks.
