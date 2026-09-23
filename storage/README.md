# UIR storage design

`storage.UirDB` owns the GORM connection and applies the embedded Atlas HCL through `commons-db/migrate`. GORM models map rows; `AutoMigrate` does not own the schema. The seven module-root tables are specified by [04_module_roots.hcl](migrations/04_module_roots.hcl) and [05_source_deltas.hcl](migrations/05_source_deltas.hcl). The [Facet ERD](../docs/uir-gorm-erd.tsx) shows their keys and references.

## Opening and cutover

The DSN selects PostgreSQL (URL or keyword DSN) or SQLite (`sqlite://` URL or plain `.db` path). `Schema` selects a PostgreSQL schema and defaults to `public`; SQLite rejects a non-default schema. In-memory SQLite is rejected because migration and application connections would not reliably share state. SQLite enables foreign keys, WAL, a five-second busy timeout, and one open connection. PostgreSQL is the shared-writer option. Migration and a connection ping must succeed before `UirDB` returns.

```go
database, err := storage.UirDB(ctx, storage.DBOptions{DSN: "./state/uir.db"})
if err != nil {
    return err
}
sqlDB, err := database.DB()
if err != nil {
    return err
}
defer sqlDB.Close()
```

Opening an existing database **discards legacy project history**. After applying the module schema, `UirDB` drops only `uir_relationships`, `uir_fields`, `uir_node_locations`, `uir_nodes`, `uir_sources`, `uir_roots`, `uir_project_heads`, `uir_snapshots`, and `uir_projects`, child first, in one transaction. Project rows are not imported into module roots. Back up an old database *before* opening it with this version if its project snapshots matter. PostgreSQL refuses a drop blocked by an external dependency; SQLite checks nonlegacy tables for foreign keys to legacy tables and refuses the cutover. Initialization then fails instead of silently removing dependent data. Migration and cutover are separate transactions: if cutover fails, the new module schema may already exist while legacy tables remain. Reopening a successfully cut-over database has no legacy tables left to drop.

The HCL files are one schema source for both targets. PostgreSQL uses canonical `uuid`, `jsonb`, and `timestamptz`; the SQLite adapter in `commons-db/migrate` maps them to text, JSON-valid text, and datetime while preserving keys, checks, and indexes. The `commons-db/migrate/README.md` guide defines the portable HCL subset and incompatibilities. UIR does not request destructive reconciliation; unexpected schema drift fails rather than rebuilding populated tables.

## Relational hierarchy

| Table / GORM model | Identity | Responsibility |
| --- | --- | --- |
| `uir_module_roots` / `ModuleRoot` | unique `root_key` from `go.mod` | Logical Go module, independent of checkout path. |
| `uir_module_locations` / `ModuleLocation` | unique `(root_id, canonical_path)` | Physical checkout or nested module location, with optional parent, mount path, kind, and repository URI. |
| `uir_module_primaries` / `ModulePrimary` | one row per root | First registered location, used by default node queries and browse. |
| `uir_module_snapshots` / `ModuleSnapshot` | UUID; `(root_id,id)` for scoped references | Immutable index run for one root and location, optionally based on another snapshot of that root. |
| `uir_module_location_heads` / `ModuleLocationHead` | one row per location | Ready snapshot published for that checkout, with compare-and-swap version. |
| `uir_source_revisions` / `SourceRevision` | unique `(root_id,path_key,content_hash,package_path,extractor_version)` | Reusable content-addressed Go AST projection: nodes and calls in JSON. |
| `uir_source_deltas` / `SourceDelta` | `(snapshot_id,path_key)` | `set` to a revision or `delete` tombstone for a path. |

`root_key` is the full module path, not a display basename. Canonical checkout path is physical location, not logical identity. Multiple worktrees and clones of one module share a root but keep separate heads. A source revision belongs to one root; reuse across its checkouts is intentional, but different module paths never share one row. The hash covers source bytes; `package_path` and `extractor_version` prevent reuse under a different interpretation. The indexer supplies module-relative source paths, and the content reader rejects absolute or traversal paths; the HCL itself checks only that a stored path is nonempty.

A snapshot references one root and one location in that root. A new location may base its first snapshot on the current primary head; subsequent runs base on its own head. Composite foreign keys enforce root agreement for locations, snapshots, bases, heads, deltas, and revisions. Readers additionally verify that a head's snapshot belongs to its location, because the HCL does not express a three-column FK tying those location IDs together.

An effective source set is not a full physical copy. `storage.EffectiveSources` walks the base chain newest first, takes the first delta for each path, ignores tombstones, and loads referenced revisions in batches. Revision JSON preserves each declaration's structured identifier, parent/slot/order, payload, semantic hash, source position, optional field detail, and call occurrences. These are **not** separate GORM node or relationship rows. Historical queries reconstruct the saved projection even after a later head changes or removes a file. Raw source bytes are **not** stored; source viewing verifies a local file's hash or a pinned Git blob against the saved hash, and fails if neither matches.

## Publication and adversarial boundaries

Discovery and hashing complete before publication. Changed-file parsing and source-revision writes occur inside the `IndexModules` transaction, which also marks snapshots ready and advances location heads with version checks. A syntax error or conflicting writer aborts that call. Selected external `go.work use` paths are indexed in a later call and therefore a separate transaction. A revision-only snapshot can have zero source deltas. Unchanged source sets avoid a new snapshot unless the Git revision changes or `--force` is used.

- **Multiple Git roots and clones:** Git repository boundaries and Go module boundaries are different. `go.mod` defines root identity; the nearest checkout supplies Git provenance. Two modules in one Git repository have distinct roots; two clones of the same module path share a root but retain distinct heads. Remote URI is diagnostic, not identity.
- **Nested modules and submodules:** discovery excludes a nested module's Go files from its parent scan. The child location stores its nearest parent and mount path; `.gitmodules` may mark a declared submodule. A nested module without `.git` is still separate. Not every Git submodule contains a `go.mod`, and not every nested `go.mod` is a submodule.
- **External workspace modules:** `go.work` can refer outside the selected directory. Interactive add asks whether to include them; non-interactive add must choose `--include-workspace-uses` or `--no-workspace-uses`. Discovery currently resolves every use path before that choice, so even an excluded missing path fails. Because publication calls are separate, inspect every returned result.
- **Moved checkout:** canonical path is the location key. Moving a directory registers a new location; it does not rewrite the old location or historical snapshots. The primary remains first-registered, so default queries can still point to an unavailable checkout. Select `--location` or `--snapshot` when needed. Automatic pruning and primary reassignment do not exist.
- **Branch switches and dirty trees:** snapshots describe indexed bytes, not branch names. A Git revision change creates a new snapshot even if included Go files are unchanged. Uncommitted bytes may not match the recorded commit; historical source viewing refuses a mismatched local file or Git blob instead of showing wrong content.
- **Symlinks and mounts:** input paths are canonicalized, directory symlinks are not traversed during discovery, and source viewing rejects paths escaping a registered location. The database cannot prove that a path still points to the same physical mount.
- **Call graph ambiguity:** extraction uses `go/ast`, not type checking. Calls retain structured identifiers in projection JSON, but the current extractor does not populate a target root key and query-time matching ignores the projection's root metadata. Target selection across checkout heads returns candidates when ambiguous; edge traversal itself matches identifier keys across the selected scopes and cannot prove an intended root. Dynamic receiver calls, build tags, and dependencies outside indexed roots prevent a sound whole-program graph.
- **Retention and read cost:** snapshots and source revisions are retained; compaction and garbage collection are not implemented. Long base chains cost more to reconstruct, and browse currently materializes all nodes in a snapshot. Do not delete a base snapshot while descendants reference it.

See [module indexing](../docs/module-indexing.md), [querying](../docs/query.md), and [serving](../docs/serve.md) for the operational contracts. HCL and integration tests are the executable schema specification.
