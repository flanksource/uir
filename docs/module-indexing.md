# Module-root indexing and queries

The root-level `uir` commands index Go modules. A module path from `go.mod` is the stable root key; a canonical filesystem path is a location of that root, not its identity. Two clones or worktrees declaring the same module path share a root but have independent location heads. The first registered location remains the primary location, and `uir query` reads its head by default.

```sh
uir add .
uir list
uir get example.org/service
uir reindex .
uir query 'nodes where method = "Run"' --root example.org/service
uir query 'nodes where method = "Run"' --root example.org/service --location ../service-feature
uir query 'nodes where method = "Run"' --snapshot 6f8b2c6e-79af-4b59-8736-e45696f0c546
```

`uir add [path]` defaults to `.` and indexes immediately. It discovers every `go.mod` beneath the selected directory, including nested modules without a separate `.git` marker. If the path is a package directory, it infers the nearest enclosing `go.mod` and scans that whole module, not just the package directory. Source files beneath a nested module are excluded from the parent module's scan. A nested location stores its nearest parent location and mount path; `.gitmodules` provenance marks a declared submodule. Symlinked input paths are canonicalized, while directory symlinks are not traversed during discovery.

If an enclosing `go.work` references `use` paths outside the selected directory, an interactive `add` asks whether to include them. A non-interactive invocation must specify `--include-workspace-uses` or `--no-workspace-uses`; the mutually exclusive flags make the scope explicit. When external uses are included, an invalid or missing referenced path fails before indexing. `uir reindex [path]` accepts only previously registered module locations and uses the same AST extractor. `--include-tests` includes `_test.go` files; `--force` reparses sources and publishes a new snapshot even when their content is unchanged.

Without `--dsn`, the CLI stores SQLite at `os.UserConfigDir()/uir/uir.db`. Explicit plain `.db` paths, `sqlite://` URLs, and PostgreSQL DSNs remain supported. The database owner is `storage.UirDB`; its embedded HCL is applied to the selected target before queries or indexing.

## Module API

`uir serve` exposes the root-level operations as structured JSON at `GET /api/v1/modules`, `GET /api/v1/modules/by-key/{root}`, `POST /api/v1/modules/add`, `POST /api/v1/modules/reindex`, and `POST /api/v1/modules/query`. `GET /api/v1/modules/locations?root=...` lists registered checkouts; `GET /api/v1/modules/snapshots?root=...&location=...` pages through one checkout's immutable snapshots. `GET /api/v1/modules/browse?snapshot=...` returns effective sources and node projections; `GET /api/v1/modules/content?snapshot=...&path=...` returns hash-verified source bytes. URL-encode full module paths and checkout paths in request parameters. Positional CLI arguments are the JSON `args` array; flags are JSON fields, for example `{"args":["nodes where method = \"Run\""],"root":"example.org/service"}` for a query. HTTP add does not prompt: when an enclosing `go.work` has external uses, send `include-workspace-uses` or `no-workspace-uses` explicitly. These operations use Clicky's typed result path, so HTTP clients receive data rows rather than captured terminal tables.

## Storage hierarchy

```text
ModuleRoot (go.mod module path)
├── ModulePrimary ── primary ModuleLocation
├── ModuleLocation (canonical checkout path, parent location and mount)
│   └── ModuleLocationHead ── latest ready ModuleSnapshot for that checkout
└── ModuleSnapshot (immutable, optional base snapshot)
    └── SourceDelta (set revision or delete path)
        └── SourceRevision (content hash and immutable Go AST projection)
```

`uir_module_roots.root_key` is the full module path; `name` is its basename. `uir_module_locations` uses `(root_id, canonical_path)` as its unique physical identity. A location head is scoped by root and location and carries a monotonically increasing version. The primary row is set once, when the first location is registered; adding a second location does not redirect default queries.

Each new location bases its first snapshot on the current primary head, if one exists. A later run at the same location bases on that location's previous head, including after a branch switch. A Git revision change creates a child snapshot even when the indexed Go source set is byte-for-byte identical. The source set is the included `.go` files, so changes to non-Go files that do not change the Git revision are outside the snapshot trigger.

A snapshot stores only changed path operations. `set` points to a `SourceRevision` keyed by root, path, content hash, package path, and extractor version. `delete` is a tombstone and has no revision ID. A source revision stores the extracted node/call projection as JSON; unchanged content reuses that row across locations and snapshots. `EffectiveSources` walks the base chain newest-first, takes the first operation per path, validates the root and revision path, and loads revisions in bounded batches. Queries reconstruct the effective view; they do not assume every snapshot physically contains all source rows.

Discovery and parsing finish before publication. All modules discovered in one `IndexModules` call publish in a single database transaction: a malformed Go file in a later nested module rolls back earlier root, location, revision, snapshot, and head writes from that call. Each snapshot is marked ready before its location head is advanced within that transaction. Existing heads use a version-checked update; a concurrent writer changing the version makes publication fail instead of silently overwriting it. External `go.work` uses selected during the same `uir add` invocation are indexed in a separate `IndexModules` call and therefore a separate transaction; successful primary-path publication is not rolled back if that later call fails.

## Query scope and call references

The PEG grammar supports `nodes`, `callers of node where ...`, `callees of node where ...`, and `unresolved calls`. `--root` selects a module path, `--location` selects one registered checkout, and `--snapshot` selects one historical immutable snapshot. `--location` and `--snapshot` cannot be combined. Node queries without a location or snapshot use primary heads, even though unscoped graph-target resolution considers all checkout heads.

For graph queries without an explicit location or snapshot, target selection examines the latest head of every registered checkout in the selected roots. If a target selector matches more than one location, the command returns candidate rows with their root, checkout, source position, and snapshot instead of choosing one arbitrarily. Select `--location` or `--snapshot` to disambiguate. Call edges retain a structured symbolic identifier; target matching permits a signature-free call locator to match a declaration's signature. `unresolved calls` means no target was found in the selected index scope or the syntax extractor could not resolve the expression; it is not a compiler diagnostic.

The extractor uses `go/ast`, not `go/types`. It does not evaluate build tags, resolve dynamic receiver types, or prove code compiles. It skips hidden, underscore-prefixed, `vendor`, and `testdata` directories and excludes tests unless requested. A long chain of tiny snapshots keeps writes small but makes effective-view reads traverse more ancestors; compaction and retention are not implemented. PostgreSQL is appropriate for shared writers; SQLite remains a local single-process store.

## Legacy cutover

The earlier `uir project ...` command, project browser API, and project GORM tables are removed. Opening an existing database discards those old tables after applying the module schema; it does not backfill project snapshots. Back up the database before opening it with this version if that history is needed. See [storage design](../storage/README.md) for the exact table allowlist and dependency checks.
