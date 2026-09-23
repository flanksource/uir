# UIR storage

The `storage` package owns the relational projection of the canonical UIR model. `UirDB` applies the embedded Atlas HCL schema and returns an initialized GORM handle; GORM `AutoMigrate` is deliberately not part of schema ownership.

The module-root delta tables and their current coexistence with the older project tables are documented in [module-root indexing](../docs/module-indexing.md). The project/snapshot hierarchy below describes the older full-snapshot path, not the `uir add`/`uir reindex` path.

## Opening a database

The DSN is the only backend selector. A PostgreSQL URL or keyword DSN opens PostgreSQL, `sqlite://` opens SQLite, and a plain path ending in `.db` opens SQLite.

```go
database, err := storage.UirDB(ctx, storage.DBOptions{
    DSN: "/var/lib/example/uir.db",
})
if err != nil {
    return err
}
sqlDB, err := database.DB()
if err != nil {
    return err
}
defer sqlDB.Close()
```

An explicit relative SQLite URL is resolved from the process working directory:

```go
database, err := storage.UirDB(ctx, storage.DBOptions{
    DSN: "sqlite://state/uir.sqlite",
})
```

PostgreSQL accepts the URL or keyword DSN used by `commons-db`. `Schema` defaults to `public`; a non-default schema is created, migrated, and selected on the returned connection:

```go
database, err := storage.UirDB(ctx, storage.DBOptions{
    DSN:    "postgres://user:password@localhost/uir?sslmode=disable",
    Schema: "analysis",
})
```

SQLite is file-backed and intended for a local single-process store. `:memory:` and `mode=memory` are rejected because migration and application handles are separate connections and an in-memory DSN would make their state-sharing semantics ambiguous. SQLite accepts an empty `Schema` or the portable default `public`; non-default schema names are rejected. Each SQLite connection enables foreign keys, a 5-second busy timeout, and WAL; the pool is limited to one open connection.

Missing DSNs, unsupported URL schemes, invalid PostgreSQL schema names, and incompatible SQLite options fail initialization. `UirDB` completes migrations and pings the returned connection before it succeeds.

## One migration source, two targets

`migrations/*.hcl` is the only schema source. It uses the PostgreSQL Atlas vocabulary because it retains the strongest canonical types and constraints: `uuid`, `jsonb`, `timestamptz`, named unique constraints, composite foreign keys, and partial indexes.

`commons-db/migrate.Apply` resolves the target from the DSN. PostgreSQL evaluates the HCL directly. SQLite evaluates the same HCL with the PostgreSQL evaluator and projects its portable subset before reconciling tables in one transaction:

| Canonical HCL | PostgreSQL | SQLite |
| --- | --- | --- |
| `uuid` | `uuid` | `text` |
| `jsonb` | `jsonb` | `text` plus `json_valid` check |
| `timestamptz` | `timestamptz` | `datetime` |
| integer types | declared integer type | `integer` |
| boolean | `boolean` | `bool` |
| unique constraint | named constraint | unique index |
| partial index predicate | PostgreSQL predicate | SQLite predicate |

The projection fails fast on dialect-specific objects it cannot preserve, including additional schemas, views, functions, procedures, triggers, non-B-tree index methods, roles, permissions, raw SQL migration files, inspection exclusions, and destructive migration options. This prevents a schema from appearing to work while silently losing behavior on one target.

Both targets are safe to initialize on every process start. PostgreSQL uses the full `commons-db` migration lifecycle. SQLite adds only missing tables, columns, and indexes and refuses drift that would rebuild a table or discard rows. Integration tests open both variants twice against the same database to prove the steady-state migration is empty.

## Identity hierarchy

The storage model separates logical ownership from physical checkout layout:

```text
Project
└── Snapshot (immutable analysis run)
    ├── Root (repository, submodule, directory, generated or virtual boundary)
    │   ├── Source (root-relative path)
    │   └── Node (root-scoped semantic identity)
    │       ├── NodeLocation (provenance in a Source)
    │       └── Field (optional structured detail)
    ├── Relationship (durable cross-node or unresolved target locator)
    └── ProjectHead (published snapshot pointer)
```

`Project.ProjectKey` identifies a logical project independently of any clone or working directory. A project may retain many immutable snapshots, while `ProjectHead` names the one readers should treat as published.

Every independently versioned or path-normalized input is a `Root`. The top-level checkout normally has an empty `mount_path`; nested repositories and submodules get their own root with a `parent_root_id`, stable `root_key`, and mount path relative to the snapshot. `repository_key`, `repository_uri`, `revision`, `submodule_path`, and `content_set_hash` describe provenance. `local_path` is diagnostic only and must never participate in identity because it changes across machines, worktrees, containers, and CI agents.

Sources are unique by `(root_id, path_key)`, so two roots may safely contain the same path such as `internal/model.go`. `display_path` preserves presentation while `path_key`, `path_case`, and `normalization_version` define comparison behavior. Paths do not escape their root.

Nodes are unique by `(root_id, identity_key)`. Their `snapshot_id` and `root_id` are repeated intentionally so composite foreign keys enforce scope rather than trusting application joins. A node parent must share its root; a node location can only combine a node and source from the same root; a resolved relationship target must belong to the relationship snapshot.

## Multiple projects, roots, and submodules

The boundary rules are designed for the cases most likely to produce accidental identity collisions:

- Two projects may analyze the same repository and revision without sharing rows. Their snapshots and roots remain distinct.
- One snapshot may contain several unrelated repositories. Each gets a separate root, even when their package names, symbol names, and relative paths overlap.
- A Git submodule is not flattened into its parent root. It gets a child root with its own repository identity, revision, path normalization, sources, and nodes.
- A nested Git checkout that is not a declared submodule is still modeled as another root; `kind` and `properties` record how it was discovered.
- The same repository mounted twice in one snapshot must use distinct root keys and mount paths. The content hash may match, but placement and traversal identity do not.
- Symlinks or generated trees that cross root boundaries must be represented by an explicit root or relationship. They must not manufacture a `path_key` containing `..` to escape the owning root.
- Cross-root and cross-project references retain `to_project_key`, `to_root_key`, `to_identity_key`, `to_symbol_key`, and `to_identifier` even when `to_node_id` cannot resolve. Resolution enriches the locator; it never replaces it.

These rules allow a monorepo, polyrepo workspace, nested checkout, and submodule graph to coexist in one snapshot without treating filesystem coincidence as semantic identity.

## Relational projection

The exported GORM models map explicitly to these tables:

- `Project` and `Snapshot` establish a logical project and its immutable analysis versions.
- `ProjectHead` is the only publication pointer. Readers begin at the head and never observe a building snapshot.
- `Root` records every Git, nested repository, submodule, directory, generated, or virtual boundary independently.
- `Source` belongs to one root through its normalized root-relative `path_key`.
- `Node` owns canonical symbol identity and a lossless node-local JSON payload.
- `NodeLocation` retains multi-file provenance. A partial unique index permits at most one primary location per node.
- `Field` is optional one-to-one detail for nodes that model fields or columns.
- `Relationship` preserves a mandatory scoped target locator even when no same-snapshot target node resolves.

Application-generated UUIDs keep identity portable across both engines. `storage.JSON` validates values before driver writes, scans either database representation, and marshals as JSON rather than a base64 byte slice.

## Enforced scope and lifecycle rules

Composite foreign keys make redundant scope columns executable invariants:

- project heads can reference only a snapshot of the same project;
- nested roots can reference only a parent in the same snapshot;
- parent nodes, node locations, and relationship sources stay inside one root;
- relationship endpoints resolve only inside the relationship snapshot;
- deleting a resolved target clears the resolved IDs but retains its durable locator;
- deleting a project cascades through its unpublished and published snapshot graph.

The database also enforces snapshot states, unique root mount paths, unique root-local node identities, traversal-free source keys, complete resolved-target pairs, non-empty relationship identity keys, and JSON validity on both targets.

Higher-level publication remains an application transaction: create a `building` snapshot, write and validate its complete root graph, transition it to `ready`, then compare-and-swap `ProjectHead.Version`. Writers must never mutate a published snapshot in place. Failed or abandoned builds remain isolated because readers follow only `ProjectHead`.

The `indexer` package implements this lifecycle for Go syntax. It hashes root-relative sources, reconstructs unchanged file projections into a new snapshot, reparses changed files, omits deleted files, resolves only unambiguous same-snapshot calls, and advances the head with a version compare-and-swap. An entirely unchanged input returns the existing head without writing another snapshot. See [`../docs/indexing.md`](../docs/indexing.md) for root discovery and syntax-only resolution limits.

## Operational limits

SQLite is the local option, not a multi-writer service database. WAL and the busy timeout reduce incidental contention but do not change the single-process ownership contract. PostgreSQL is the target for concurrent writers, shared services, non-default schemas, role management, SQL migration phases, views, and other server-side objects.

Migration compatibility is intentionally asymmetric: every UIR migration must fit the documented portable HCL subset, while PostgreSQL may retain stronger native representation for that same declaration. If a future storage requirement cannot be projected without losing semantics, initialization must fail until the shared migration layer gains an explicit mapping or UIR declares PostgreSQL-only support.
