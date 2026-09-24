# Snapshot-aware symbol index: schema review

**Status:** Proposed schema. This document is a review artifact; it does not describe tables that have already been migrated. The seven existing tables are implemented under `uir_`-prefixed names by [`04_module_roots.hcl`](../storage/migrations/04_module_roots.hcl) and [`05_source_deltas.hcl`](../storage/migrations/05_source_deltas.hcl); this document renames them, revises them, and adds four tables. The physical cutover is step 0 of the implementation sequence. The [ERD](uir-gorm-erd.tsx) shows the target model.

## Goal and storage boundary

Store, for every indexed file version, one document holding the facts extracted from it: the symbols it declares with their signatures and hashes, the symbols it mentions, and where. When the package type-checks those facts are compiler-proven; otherwise the document holds the syntax-only projection the AST extractor produces today. A reference result must include the source filename and exact range from the selected snapshot. Resolving a symbol's declaration is a second lookup against files active in the selected branch or snapshot, so one symbol identity may have several declaration locations across checkout heads. A diff between two commits must list symbols added, removed, and changed, filtered by visibility, and attribute line additions and removals to file and symbol.

Source bytes remain in Git or the local checkout. The database retains hashes, symbol identities, one document per file version, and an inverted index from symbol to the documents that mention it. Positions and per-symbol shape and body hashes live only inside documents. Historical source viewing and exact line counts require hash-verified bytes; symbol-level facts and reference locations remain queryable when those bytes are unavailable.

The schema uses the repository's portable Atlas HCL subset: `uuid`, `text`, `integer`/`bigint`, `jsonb`, and `timestamptz`; column-based B-tree indexes; primary, unique, foreign-key, and check constraints. PostgreSQL uses those types directly. The SQLite adapter maps them to compatible types and keeps keys and indexes. No view, materialized view, trigger, GIN, trigram, expression index, sequence, or column collation is required, because none of those is portable.

In the tables below, every column is `NOT NULL` unless suffixed with `?`. `PK` and `UQ` each imply an index. All paths are module-relative slash-separated `path_key` values. All line and UTF-16 column numbers are one-based; byte offsets are zero-based, with an exclusive end offset. Every foreign key uses `ON UPDATE NO ACTION`; its `ON DELETE` action is shown with the reference, and unqualified `NO ACTION` means deletion is blocked while a referencing row remains.

## As implemented today

| Table | Columns | Keys | References and checks |
| --- | --- | --- | --- |
| `uir_module_roots` | `id uuid`, `root_key text`, `name text`, `created_at timestamptz` | PK `id`; UQ `root_key` | `length(root_key) > 0` |
| `uir_module_locations` | `id uuid`, `root_id uuid`, `canonical_path text`, `parent_location_id uuid?`, `mount_path text`, `kind text`, `repository_uri text?`, `created_at timestamptz` | PK `id`; UQ `(root_id,id)`; UQ `(root_id,canonical_path)` | `root_id → roots.id` CASCADE; `parent_location_id → locations.id` NO ACTION; `length(canonical_path) > 0` |
| `uir_module_snapshots` | `id uuid`, `root_id uuid`, `location_id uuid`, `base_snapshot_id uuid?`, `state text`, `revision text`, `content_set_hash text`, `configuration_hash text`, `extractor_version text`, `started_at timestamptz`, `completed_at timestamptz?` | PK `id`; UQ `(root_id,id)`; index `(location_id,started_at)` | `root_id → roots.id` CASCADE; `(root_id,location_id) → locations(root_id,id)` CASCADE; `(root_id,base_snapshot_id) → snapshots(root_id,id)` NO ACTION; `state IN ('building','ready','failed')` |
| `uir_module_location_heads` | `root_id uuid`, `location_id uuid`, `snapshot_id uuid`, `version bigint` | PK `location_id` | `(root_id,location_id) → locations(root_id,id)` CASCADE; `(root_id,snapshot_id) → snapshots(root_id,id)` NO ACTION; `version >= 1` |
| `uir_module_primaries` | `root_id uuid`, `location_id uuid` | PK `root_id` | `(root_id,location_id) → locations(root_id,id)` CASCADE |
| `uir_source_revisions` | `id uuid`, `root_id uuid`, `path_key text`, `content_hash text`, `package_path text`, `extractor_version text`, `size_bytes bigint`, `projection jsonb` | PK `id`; UQ `(root_id,id)`; UQ `(root_id,path_key,content_hash,package_path,extractor_version)` | `root_id → roots.id` CASCADE; `length(path_key) > 0`; `size_bytes >= 0` |
| `uir_source_deltas` | `snapshot_id uuid`, `root_id uuid`, `path_key text`, `revision_id uuid?`, `operation text` | PK `(snapshot_id,path_key)`; index `(revision_id)` | `(root_id,snapshot_id) → snapshots(root_id,id)` CASCADE; `(root_id,revision_id) → source_revisions(root_id,id)` NO ACTION; `set` requires a revision and `delete` forbids one |

Weaknesses in this model, each fixed in the base schema below: a head's snapshot is not tied to the head's location; a delta's revision is not tied to the delta's path; `building` and `failed` are never observable, because publication is one transaction; `extractor_version` on snapshots duplicates `configuration_hash`; the syntax projection is a second document per file once typed documents exist, and every `nodes` query decodes all of them; `base_snapshot_id` and `parent_location_id` have no index behind their NO ACTION references; `kind` is unchecked; `revision` may be empty; and the snapshot trigger hashes only `.go` bytes.

## Base schema

| Table | Columns | Keys | References and checks |
| --- | --- | --- | --- |
| `modules` | `id uuid`, `root_key text`, `name text`, `created_at timestamptz` | PK `id`; UQ `root_key` | `length(root_key) > 0` |
| `locations` | `id uuid`, `root_id uuid`, `canonical_path text`, `parent_location_id uuid?`, `mount_path text`, `kind text`, `repository_uri text?`, `created_at timestamptz` | PK `id`; UQ `(root_id,id)`; UQ `(root_id,canonical_path)`; index `(parent_location_id)` | `root_id → modules.id` CASCADE; `parent_location_id → locations.id` NO ACTION; `length(canonical_path) > 0`; `kind IN ('module','git','git-submodule')` |
| `snapshots` | `id uuid`, `root_id uuid`, `location_id uuid`, `base_snapshot_id uuid?`, `revision text`, `worktree_state text`, `content_set_hash text`, `configuration_hash text`, `context_hash text`, `coverage text`, `package_count integer`, `diagnostics jsonb`, `started_at timestamptz`, `completed_at timestamptz` | PK `id`; UQ `(root_id,id)`; UQ `(location_id,id)`; index `(location_id,started_at)`; index `(base_snapshot_id)`; index `(root_id,revision)` | `root_id → modules.id` CASCADE; `(root_id,location_id) → locations(root_id,id)` CASCADE; `(root_id,base_snapshot_id) → snapshots(root_id,id)` NO ACTION; `worktree_state IN ('clean','dirty','unknown')`; `(worktree_state = 'unknown') OR length(revision) > 0`; `length(context_hash) = 64`; `coverage IN ('indexed','partial','syntax','excluded')`; `package_count >= 0` |
| `location_heads` | `root_id uuid`, `location_id uuid`, `snapshot_id uuid`, `version bigint` | PK `location_id` | `(root_id,location_id) → locations(root_id,id)` CASCADE; `(location_id,snapshot_id) → snapshots(location_id,id)` NO ACTION; `version >= 1` |
| `primary_locations` | `root_id uuid`, `location_id uuid` | PK `root_id` | `(root_id,location_id) → locations(root_id,id)` CASCADE |
| `source_revisions` | `id uuid`, `root_id uuid`, `path_key text`, `content_hash text`, `package_path text`, `size_bytes bigint` | PK `id`; UQ `(root_id,path_key,id)`; UQ `(root_id,path_key,content_hash,package_path)` | `root_id → modules.id` CASCADE; `length(path_key) > 0`; `length(content_hash) = 64`; `size_bytes >= 0` |
| `source_deltas` | `snapshot_id uuid`, `root_id uuid`, `path_key text`, `revision_id uuid?`, `operation text` | PK `(snapshot_id,path_key)`; index `(revision_id)` | `(root_id,snapshot_id) → snapshots(root_id,id)` CASCADE; `(root_id,path_key,revision_id) → source_revisions(root_id,path_key,id)` NO ACTION; `(operation = 'set' AND revision_id IS NOT NULL) OR (operation = 'delete' AND revision_id IS NULL)` |

Changes from the implemented model: tables renamed; `snapshots` loses `state` and `extractor_version` and gains `worktree_state`, `context_hash`, `coverage`, `package_count`, `diagnostics`, the `(location_id,id)` UQ and two indexes; `location_heads` references the snapshot through its location; `source_revisions` loses `projection` and `extractor_version` and keys the delta and document references by path; `locations` gains a `kind` check and an index. A snapshot row exists only when its publication committed, so `completed_at` is NOT NULL.

### Snapshot trigger

`indexer/discovery.go` hashes only `.go` files into `ContentSetHash`, and `indexer/modules.go` skips snapshot creation when revision, content-set hash, configuration hash, and extractor version match the base. Under that rule a `go get -u`, a `go.work` edit, or an exported-type change in a workspace sibling produces no snapshot and has nowhere to record new facts. Three changes to that seam are part of this design.

1. `ContentSetHash` also covers `go.mod`, `go.sum`, `go.work`, and `go.work.sum` when present, keyed by their module-relative paths like any other file. A dependency bump then yields a snapshot with zero source deltas, which the storage layer already permits.
2. `ConfigurationHash` covers the indexer version and the build variant: `GOOS`, `GOARCH`, build tags, `CGO_ENABLED`, and the Go toolchain version. Exactly one variant is indexed per snapshot; a second variant is a configuration change and a new snapshot.
3. The unchanged check compares `revision`, `content_set_hash`, `configuration_hash`, and `context_hash` (defined below) with the base snapshot. The last catches a workspace sibling whose export shape changed while this module's bytes did not.

`worktree_state` is `clean` when `git status` reports no changes under the module, `dirty` otherwise, and `unknown` without Git, in which case `revision` is empty. A commit-to-commit diff must know whether a snapshot's bytes are the commit's bytes.

## Hashes and identities

All digests are lowercase hexadecimal SHA-256 of a versioned, length-delimited canonical encoding.

**Symbol identity.** `symbols.id` digests `(identity_version, module_key, package_path, kind, owner_id, name, ordered parameter type identities)`. `canonical_key` stores that encoding and is unique, so an insert verifies that an existing digest has exactly the expected value. Parameter names, return types, field types, source path, line, and bodies do not enter identity. `module_key` is the Go module path (`root_key` for indexed roots), `std` for the standard library, and empty only for `kind = 'builtin'`. Parameter types use fully qualified named types and recursively encoded composite types; type parameters are encoded by ordinal. Local bindings, parameters, and labels are not canonical symbols: their occurrences stay inside the document with a null symbol, because they are never referenced across files. Canonical identities exist only for typed documents; a `syntax` document's symbol entries carry the v1 `IdentityKey()` and a null id, because unresolved parameter types cannot produce a trustworthy identity.

**Visibility.** A symbol is `exported` when its name is exported and every owner in its chain is exported; otherwise it is `internal`. A method on an unexported type is therefore `internal` even though its name is capitalised.

**Symbol shape.** `shape_hash` digests the symbol's full declared type: the complete signature including parameter names, results, and variadic-ness for functions and methods; the field list with names, types, tags, and embedding for structs; the method set for interfaces; the underlying type for other named types; the type and, for constants, the value for variables and constants. Equal identity and equal `shape_hash` mean the same interface to callers. `shape` is the rendered form for display, in a canonical layout chosen so that two shapes diff line by line: a function or method signature is one line; a struct renders one field per line and an interface one method per line, each line `gofmt`-aligned, between a `type Name struct {` or `interface {` line and a closing `}`; a variable or constant is one line with its type and, for constants, its value. `shape_hash` digests this rendering, so a shape diff never differs from a hash change.

**Symbol body.** `body_hash` digests the token stream of the declaration's extent with comments and whitespace removed. A differing `body_hash` with an equal `shape_hash` is an implementation-only change.

**Export shape.** `export_shape_hash(P)` digests the sorted `(symbol id, shape_hash)` pairs of P's exported symbols together with the sorted `export_shape_hash` of P's direct imports. Because it recurses through imports, a change in any transitively imported exported type changes every dependent's shape. For a package outside the indexed roots it is `H(module path, module version, toolchain version)`; for the standard library it is `H("std", toolchain version)`. Neither requires type-checking the dependency, so the unchanged decision stays cheap.

**Package input hash.** `input_hash(P)` digests `(configuration_hash, package_path, sorted (path_key, content_hash) of P's files, sorted (import path, export_shape_hash) of P's direct imports)`. Two files of one package share it. It is the complete input to extracting P: same input hash, same document.

**Snapshot context hash.** `context_hash(S)` digests `(configuration_hash, sorted (package_path, input_hash) of every package in the module)`.

**Document key.** A document is identified by `(root_id, path_key, input_hash)`. Since `input_hash` embeds the file's own content hash, the same bytes extracted under a changed dependency get a new document and the old one remains valid for older snapshots.

## Proposed tables

| Table | Columns | Keys | References and checks |
| --- | --- | --- | --- |
| `symbols` | `id text`, `identity_version integer`, `canonical_key text`, `module_key text`, `package_path text`, `kind text`, `owner_id text?`, `name text`, `search_name text`, `visibility text`, `parameter_types jsonb` | PK `id`; UQ `canonical_key`; index `(module_key,package_path,kind,owner_id,name)`; index `(search_name,module_key,id)` | `owner_id → symbols.id` NO ACTION; `length(id) = 64`; `identity_version >= 1`; `kind IN ('package','type','func','method','field','var','const','builtin')`; `visibility IN ('exported','internal')`; `length(name) > 0`; `length(search_name) > 0`; `length(canonical_key) > 0`; `(kind = 'builtin' AND length(module_key) = 0) OR (kind <> 'builtin' AND length(module_key) > 0)` |
| `documents` | `id uuid`, `root_id uuid`, `path_key text`, `source_revision_id uuid`, `package_path text`, `input_hash text`, `indexer_version text`, `coverage text`, `symbol_count integer`, `occurrence_count integer`, `content jsonb` | PK `id`; UQ `(root_id,id)`; UQ `(root_id,path_key,input_hash)`; index `(source_revision_id)` | `root_id → modules.id` CASCADE; `(root_id,path_key,source_revision_id) → source_revisions(root_id,path_key,id)` NO ACTION; `length(input_hash) = 64`; `length(path_key) > 0`; `length(indexer_version) > 0`; `coverage IN ('indexed','partial','syntax','excluded')`; `symbol_count >= 0`; `occurrence_count >= 0`; `coverage <> 'excluded' OR (symbol_count = 0 AND occurrence_count = 0)` |
| `symbol_postings` | `document_id uuid`, `root_id uuid`, `symbol_id text`, `role text`, `occurrence_count integer` | PK `(document_id,symbol_id,role)`; index `(symbol_id,role,root_id,document_id)` | `(root_id,document_id) → documents(root_id,id)` CASCADE; `symbol_id → symbols.id` NO ACTION; `role IN ('definition','reference','implements')`; `occurrence_count >= 1` |
| `package_coverage` | `snapshot_id uuid`, `root_id uuid`, `package_path text`, `input_hash text`, `export_shape_hash text?`, `coverage text`, `file_count integer`, `diagnostics jsonb` | PK `(snapshot_id,package_path)`; index `(root_id,package_path,input_hash)` | `(root_id,snapshot_id) → snapshots(root_id,id)` CASCADE; `length(input_hash) = 64`; `export_shape_hash IS NULL OR length(export_shape_hash) = 64`; `coverage IN ('indexed','partial','syntax','excluded')`; `coverage <> 'indexed' OR export_shape_hash IS NOT NULL`; `length(package_path) > 0`; `file_count >= 0` |

`symbols` is global and is never cascaded. A symbol with no posting is unreachable; a future garbage collector deletes such rows with `NOT EXISTS` over the posting index. No other retention is proposed, matching the existing policy that snapshots and source revisions are retained.

Coverage has one vocabulary at every level. `indexed`: type-checked without diagnostics. `partial`: type-checked with diagnostics; only proven facts are recorded. `syntax`: the package did not type-check, so the document holds the AST extractor's declarations and unresolved calls, and has no postings. `excluded`: not extracted (build constraints, `testdata`, generated exclusions); the document is empty and says why. A snapshot's `coverage` is the weakest of its packages'.

### The document

`documents.content` is one JSON object. Its shape is versioned by `indexer_version`, so a format change is a new indexer version and new documents; documents are never migrated in place.

```json
{
  "version": 1,
  "package_path": "example.org/service/invoices",
  "symbols": [
    {
      "id": "3f9c…64 hex…",
      "key": "v1:[\"method\",\"\",\"example.org/service/invoices\",\"Store\",\"Save\",\"\",\"(context.Context,Invoice)->error\"]",
      "kind": "method",
      "visibility": "exported",
      "shape": "func (s *Store) Save(ctx context.Context, inv Invoice) error",
      "shape_hash": "9d2a…64 hex…",
      "body_hash": "c4e1…64 hex…",
      "name": [12, 17, 12, 21],
      "name_bytes": [418, 422],
      "extent": [12, 1, 30, 2],
      "extent_bytes": [406, 902],
      "implements": ["7a10…64 hex…"]
    }
  ],
  "occurrences": [
    { "symbol": "b21e…64 hex…", "role": "call", "range": [15, 9, 15, 13], "bytes": [511, 515], "enclosing": "3f9c…64 hex…" },
    { "symbol": null, "role": "reference", "range": [16, 2, 16, 3], "bytes": [520, 521], "enclosing": "3f9c…64 hex…", "note": "local binding" }
  ],
  "diagnostics": [
    { "range": [40, 1, 40, 20], "message": "undefined: Foo" }
  ]
}
```

- `symbols` lists every symbol the file declares, sorted by extent start. `key` is the v1 `IdentityKey()` the syntax extractor already produces, which maps an explorer tree node to its entry and is the only identity a `syntax` document has (`id` is null there). `name` is the identifier range shown in navigation; `extent` is the whole declaration including its doc comment. `implements` lists interface symbols, among all packages loaded in the run, that a declared type satisfies. `shape`, `shape_hash`, `body_hash`, and `visibility` are the diff inputs; a `syntax` document renders `shape` from source text and omits `shape_hash`.
- `occurrences` lists every identifier use in the file, sorted by start byte. `role` is `definition`, `reference`, `call`, `write`, `type`, or `import`. `symbol` is null when the target is a local binding or could not be resolved; `note` then states why. In a `syntax` document every occurrence is a call with a null symbol and a `target` field holding the structured identifier the AST extractor records today. `enclosing` is the innermost declared symbol containing the occurrence, or null at file scope.
- Ranges are `[start_line, start_utf16_column, end_line, end_utf16_column]`, one-based; byte spans are `[start, end)`, zero-based. The publisher rejects any range whose end does not follow its start, and any two symbol extents that overlap other than by nesting.

The posting table collapses document roles: `definition` from `symbols`, `reference` from every occurrence with a non-null symbol regardless of document role, and `implements` from each `implements` entry. Builtins receive no postings; their fan-in is useless for navigation and dominates row counts.

The publisher verifies before writing that every posting's symbol appears in the document, that `symbol_count` and `occurrence_count` equal the document's array lengths, that every `symbol` and `enclosing` value is a known symbol id, that each entry's `visibility` and `kind` equal the `symbols` row, and that `package_path` equals the source revision's package path. Documents and postings are content-addressed, so a concurrent or repeated publication inserts them with `ON CONFLICT DO NOTHING` and re-reads, as `indexer/modules_sources.go` does for source revisions; postings for an existing document are skipped, not duplicated.

## Membership: which documents a snapshot activates

No delta or checkpoint table exists for documents. A snapshot's active documents are derived:

1. Effective sources for the snapshot give `(path_key, source_revision)` per path, from the source deltas.
2. `package_coverage` rows for the snapshot give `input_hash` per `package_path`.
3. For each path, the active document is the `documents` row with `(root_id, path_key, input_hash(package of that path))`, looked up in batches.

Because the lookup is content-addressed, a snapshot that changed only dependencies reuses every document of every package whose input hash did not change, and a package whose input hash did change gets fresh documents for all of its files in one run. Browse, `nodes` queries, and typed queries all read the same documents.

Step 1 today issues one query per base link (`storage.EffectiveSources`) and loads full projections. This design replaces it with one recursive CTE that walks the base chain and returns the first delta per path with lean columns. Both engines support `WITH RECURSIVE` and window functions:

```sql
WITH RECURSIVE chain(id, base_id, depth) AS (
  SELECT id, base_snapshot_id, 0 FROM snapshots WHERE id = ?
  UNION ALL
  SELECT s.id, s.base_snapshot_id, c.depth + 1
  FROM snapshots s JOIN chain c ON s.id = c.base_id
),
ranked AS (
  SELECT d.path_key, d.revision_id, d.operation,
         ROW_NUMBER() OVER (PARTITION BY d.path_key ORDER BY c.depth) AS rank
  FROM source_deltas d JOIN chain c ON c.id = d.snapshot_id
)
SELECT path_key, revision_id FROM ranked WHERE rank = 1 AND operation = 'set';
```

Chain length is bounded only by history depth; the CTE makes that one query rather than one per link. If measured chain walks become the dominant cost, a source checkpoint table is the follow-up. It is not part of this design.

## Diffing two commits

A diff takes two git commits `A..B` and a root, and is computed from the two snapshots' active documents plus, for exact line counts, the two commits' blobs. No diff table is stored: both inputs are immutable, so a result is reproducible and may be cached by the caller.

**Commit to snapshot.** For each commit, select from the `(root_id, revision)` index the snapshots of the root whose `revision` equals the commit and whose `worktree_state` is `clean`; take the newest. If the only matches are `dirty`, the diff fails and names them, because their bytes may not be the commit's bytes; `--snapshot-from` and `--snapshot-to` override the selection explicitly. If a commit has no snapshot, the diff fails and states that the commit must be indexed first; indexing an arbitrary commit through a temporary worktree is a separate command and is out of scope here. Both snapshots must share `configuration_hash`, otherwise `shape_hash` and `body_hash` are not comparable and the diff fails saying so.

**Changed files.** Derive both active document maps. Paths with equal document ids on both sides are unchanged and are skipped without reading them. Paths present on one side only are added or removed files.

**Symbol diff (a).** For every changed, added, or removed file, load its documents and index symbol entries by symbol id across the whole diff, so a symbol that moved between files is matched by identity. Each symbol is classified:

| Class | Condition |
| --- | --- |
| `added` | id only in B |
| `removed` | id only in A |
| `signature` | id on both sides, `shape_hash` differs |
| `body` | id on both sides, `shape_hash` equal, `body_hash` differs |
| `moved` | id on both sides, hashes equal, `path_key` differs |
| unchanged | id on both sides, hashes and path equal; omitted |

A rename appears as `removed` plus `added`, because identity includes the name; the output does not guess pairings. A parameter-type change likewise appears as `removed` plus `added` of the old and new identities, grouped under the old symbol's owner and name so the reader sees them together. The `--visibility` filter is `exported` by default and keeps a symbol when its `visibility` on either side matches; `internal` and `all` are the other values. The default output is therefore the interface change list: exported symbols added, removed, or changed in signature. Each row carries kind, owner, name, both `shape` strings when they differ, a shape diff for `signature` rows, and both file paths.

**Signature diff rendering.** A `signature` row shows what changed in the shape, not only that it changed. The two canonical `shape` renderings are line-diffed (Myers over lines). Unchanged lines print with a two-space prefix, removed lines with `-`, added lines with `+`. A removed line immediately followed by an added line is a replaced line, and the pair is additionally token-diffed with `go/scanner` tokens so the reader sees which parameter or type moved: removed tokens render red with strikethrough, added tokens green and bold, unchanged tokens plain. A one-line signature therefore prints as a single replaced pair with token marks, while a struct change prints as a field-level line diff. Rendering goes through clicky `api.Text`, whose builder already provides `Strikethrough()` and colour, so the terminal gets ANSI strikethrough, markdown gets `~~removed~~` and `**added**`, HTML gets `<del>` and `<ins>`, and JSON carries the structured form: `shape_diff` as a list of lines, each `{op: "equal"|"delete"|"insert", tokens: [{op, text}]}`. The same renderer serves the `removed`-plus-`added` grouping of a parameter-type change, diffing the old identity's shape against the new one so a type change reads as one signature edit. Package-level `export_shape_hash` equality is the fast pre-check: such a package contributes no exported `added`, `removed`, or `signature` rows and is skipped when the filter is `exported`. A `syntax` document on either side has no ids, so its symbols are matched by `key` and can only be classified `added`, `removed`, `body`, or `moved`; the row is marked `syntax`.

**Line attribution (b).** For each changed file, read both commits' bytes from Git (`git cat-file` on the blob at that commit in a registered checkout of the root) and verify each against the source revision's `content_hash`; a mismatch or a missing checkout fails the diff for that file with the reason, and the symbol diff for that file is still reported. Each side's document gives symbol extents in bytes. The file is partitioned into segments: one per outermost symbol extent, plus the gaps between them, which form a single `(file scope)` segment covering package clause, imports, comments, and anything not inside a declaration. Segments are paired by symbol id (or by `key` for `syntax` documents), and the gap segments are paired with each other. Each pair is line-diffed independently (Myers over lines), which yields `+added -removed` per symbol exactly, because a change inside one declaration cannot be attributed to another. A symbol present on one side only contributes all of its extent lines as added or removed. A `moved` symbol is diffed across files and reported under the destination file with the source path noted.

Per-file totals are the sums of their segment pairs, and per-package totals the sums of their files, so `store.go +14 -5` equals the sum of its symbol rows plus its file-scope row.

```text
example.org/service/invoices                          +14 -5
  invoices/store.go                                   +14 -5
    Store.Save          signature   +10 -3
      - func (s *Store) Save(ctx context.Context, inv Invoice) error
      + func (s *Store) Save(ctx context.Context, inv Invoice, opts ...SaveOption) (Receipt, error)
    Store.load          body        +4  -2
    (file scope)                    +0  -0
  invoices/model.go                                   +6  -6
    Invoice             signature   +1  -1
        type Invoice struct {
          ID       InvoiceID
      -   Total    int
      +   Total    Money
      +   Notes    string
        }
    Invoice.Total       added       +6  -0   func (i Invoice) Total() Money
    Invoice.total       removed     +0  -6   func (i Invoice) total() Money
```

In a terminal the `Store.Save` pair renders as one line with token marks rather than two full lines: the removed `) error` tail is red and struck through, and `, opts ...SaveOption) (Receipt, error)` is green and bold, so the reader sees the added variadic parameter and the new result tuple without comparing the lines by eye. In markdown the same pair is `func (s *Store) Save(ctx context.Context, inv Invoice~~) error~~**, opts ...SaveOption) (Receipt, error)**`. The plain-text form above is what `--format text` and non-TTY output print.

With `--visibility exported` the `Store.load` and `Invoice.total` rows are hidden; the file totals then state that hidden rows exist rather than silently summing to less than the file's diff.

**Coverage.** A `partial` file on either side still diffs the symbols it proved; the row is marked `partial` so a missing symbol is not read as removed. An `excluded` file is listed as excluded with its line diff attributed entirely to file scope.

**Cost.** Document reads are bounded by changed packages (package-granular input hashes re-store sibling files even when only one file changed; their ids differ, so they are read, but their symbols compare equal and are omitted). Git reads are bounded by changed files. No SQL beyond the two membership derivations and batched document loads.

## Index inventory

All indexes are listed with their tables above. Named secondary indexes, in HCL naming: `locations_parent_idx`, `snapshots_location_started_idx`, `snapshots_base_idx`, `snapshots_root_revision_idx`, `source_deltas_revision_id_idx`, `symbols_lookup_idx`, `symbols_search_idx`, `documents_source_idx`, `symbol_postings_symbol_idx`, `package_coverage_input_idx`. Unique indexes are named `<table>_<columns>_key`.

`search_name` is the symbol's `name` lowercased and restricted to `[a-z0-9_]`, with every other character removed. A prefix search is then `search_name >= ? AND search_name < ?` with the upper bound formed by incrementing the last byte, which is correct under any collation for that alphabet and uses the B-tree on both engines. Column collations and operator classes are outside the portable subset, so a general `LIKE 'prefix%'` must not be relied on. Qualified input such as `Store.Save` is split by the query layer: resolve owners by `search_name = 'store'`, then filter children by `owner_id`. Substring and fuzzy search need a separate design.

## Query and update examples

**References to B at every head.** Resolve B through `symbols_lookup_idx`; a selector matching several symbols returns candidates first. For each selected location head, derive its active document ids. Then read `symbol_postings_symbol_idx` for `(B, 'reference', root)` and intersect with the active set; the query layer fetches whichever side is smaller into memory and probes the other with batched `IN` lists. Load the matching documents, filter occurrences with `symbol = B`, and return one row per occurrence with path, range, role, and enclosing symbol. Declaration locations come from the same intersection with role `definition`; a same-location declaration is preferred for display and other heads remain visible as candidates. Reference rows are never multiplied by the number of declaration locations.

**References in a historical snapshot.** Identical, with the active set derived for that snapshot. A `syntax` or `excluded` package in scope is reported as such so an empty result is not mistaken for proven absence.

**Callers of B, interface-aware.** Postings with role `implements` for interface I give the concrete types that satisfy I. Callers of a concrete method `T.M` are the references to `T.M` plus the references to `I.M` for every I that T implements, when the caller asks for dispatch-inclusive results. Dispatch through an interface declared outside the loaded packages is not proven.

**Callees of A.** Load A's defining document (posting `(A, 'definition')` intersected with the active set) and return occurrences whose `enclosing` is A and whose role is `call`.

**Nodes and browse.** The existing `nodes where …` predicates and the explorer read `symbols` entries from active documents, typed or syntax, so the current AST-only behaviour is preserved for packages that do not type-check.

**Name search.** Range-scan `symbols_search_idx`, keep symbols with a `definition` posting in the active set of the selected heads, and rank by kind and module.

**Diff.** `uir diff <from>..<to> --root example.org/service` lists exported symbol changes; `--visibility all` includes internal symbols; `--stat` adds per-file and per-symbol line counts and requires readable Git blobs.

| Edit | Symbols | Documents and postings | Diff classification |
| --- | --- | --- | --- |
| Insert a comment above a use of B | Unchanged | Package input hash changes; every file in the package gets a new document with identical postings | `body` if inside a symbol's extent, otherwise file scope `+1 -0` |
| Add a second call from A to B | Unchanged | New documents for the package; posting `(doc, B, reference)` count becomes 2 | `body` on A |
| Change A's parameter name | Unchanged | New documents for the package | `signature` on A (names are in the shape, not the identity) |
| Change A's parameter type | New symbol for A; old retained | New documents for the package and, if A is exported, for every dependent package | `removed` old A, `added` new A, grouped |
| Move B to another file in the same package | Unchanged | New documents; B's `definition` posting moves | `moved` |
| Change an exported return or field type | Identity may remain | Export shape changes; every transitively dependent package is re-extracted | `signature` |
| `go get` bumping a dependency | Unchanged | New snapshot with zero source deltas; only importing packages get new documents | No symbol rows; `go.sum` is a changed file with file-scope lines |

**Cost note on package granularity.** Because a package's input hash covers all of its files, editing one file re-extracts and re-stores documents for every file in that package. That is the price of keeping the key content-addressed and the membership derivation free of per-file delta rows. If measurement shows this dominating, the refinement is a per-file input hash that combines the file's content hash with the package's export-and-imports hash rather than with sibling file hashes; it changes only the key definition, not the tables, and shrinks diff reads to exactly the changed files.

## Expected size and cost

Let `F` be files per module, `P` packages, `S` snapshots, `c` files re-extracted per snapshot, `k` distinct symbols mentioned per file, and `L` active location heads.

- Documents: one row per `(path, input_hash)`. History grows by `c` documents per snapshot, each a few kilobytes; this replaces the projection column rather than adding to it.
- Postings: `k` rows per document, typically 3 to 5 times fewer than occurrences in real files, each about 100 bytes plus a 64-byte symbol key and one index entry.
- Package coverage: `P` rows per snapshot, `O(S × P)`.
- Symbols: one row per distinct identity ever observed, retained.
- Membership: no rows; one recursive CTE plus `P` package rows plus `F / 256` batched key lookups per snapshot derivation.
- Reference lookup: one derivation, one posting range scan bounded by `(symbol, role, root)`, then document reads equal to the number of matching files. No `O(L × R)` posting tables exist.
- Diff: two derivations, document reads for changed packages, Git reads for changed files, in-memory line diffs per symbol pair.

Using the earlier illustration (100 modules, 500 files, 10,000 occurrences per module, 1,000 snapshots, one 20-occurrence file changing per snapshot, 5-file packages): about 550,000 documents, roughly 8 million posting rows at 15 distinct symbols per document, 5 million package coverage rows, and no occurrence, checkpoint, head, or diff rows. The occurrence-row design stored about 3 million occurrence rows plus 1 million head postings plus 800,000 checkpoint pointers under an assumption that ignored package re-typing. With realistic files of 200 to 500 occurrences and 60 to 100 distinct symbols the posting design stores four to five times fewer rows; with the toy 20-occurrence file it does not, and its saving is row width and the absence of position rows. These are algorithmic expectations, not measured latency guarantees; step 8 below measures them.

Discovery still reads and hashes all included files on each run. Extraction runs through `go/packages` on changed packages and their dependents and completes before the publication transaction opens: the transaction writes symbols, documents, postings, package rows, and the snapshot row, then advances the head with the existing version check. SQLite's single connection is never held across type-checking.

## Phase-2 option: head materialization

If measured head queries across many locations are too slow because each head derivation repeats the CTE and key lookups, add `head_documents (location_id, root_id, path_key, document_id)` with PK `(location_id, path_key)`, replaced for changed paths in the publication transaction. It holds `O(L × F)` small rows, not `O(L × R)`, and reference queries join it directly with postings. The criterion for adding it is a benchmark on a real workspace showing derivation above a fixed budget per head; it is not part of the initial schema.

## Implementation and verification sequence

0. Cutover. Rewrite `04_module_roots.hcl` and `05_source_deltas.hcl` to the base schema above and add `06_symbol_index.hcl` for the four proposed tables; add it to the `//go:embed` list in `storage/database.go`. Extend `storage/legacy_cutover.go` to drop the `uir_*` tables before migration, child tables first, one transaction, refusing if an unmanaged table references them. Update the GORM models and `TableName()` methods, drop `SnapshotState`, `storage/README.md`, `docs/module-indexing.md`, `docs/symbols.md`, `docs/query.md`, and the ERD. This discards the local index; `uir reindex` rebuilds it. Test that an old database opens, loses the prefixed tables, and reindexes cleanly on both engines, and add schema assertions for SQLite and PostgreSQL.
1. Extend `ContentSetHash` and `ConfigurationHash` in `indexer/discovery.go` and `indexer/modules.go`, record `worktree_state`, and compare `context_hash` in the unchanged check, with tests showing that a `go.sum` change and a variant change each produce a new snapshot with zero source deltas.
2. Replace the per-link loop in `storage.EffectiveSources` with the recursive CTE and a lean row type. Test a 1,000-link chain and a branch that bases on another location's head.
3. Move the AST extractor's output into `syntax` documents and port `query/modules.go`, `storage/module_browse.go`, and the explorer to read documents. Existing query and browse tests must pass unchanged in behaviour.
4. Add identity canonicalization, visibility, shape and body hashing, export-shape and input hashing, and typed document extraction over `go/packages` with `go/types.Info`, falling back to a `syntax` document per package that fails to type-check. Test parameter-name stability of identity, parameter-type changes, shape changes on return and field types, body-only changes, duplicate use sites, file moves, interface `implements`, builtins excluded from postings, syntax fallback, and the `context_hash` unchanged decision with and without a changed sibling module.
5. Add publication: symbols and documents with `ON CONFLICT DO NOTHING`, postings, package rows, and the snapshot row inside the existing head compare-and-swap transaction. Test rollback after a failed compare-and-swap and idempotent republication.
6. Add the diff: commit-to-snapshot selection with the clean-worktree rule and overrides, symbol classification with the visibility filter, line attribution by symbol extent over hash-verified Git blobs, and the shape diff (line diff of canonical shapes, token diff of replaced lines, rendered through clicky `api.Text` with `Strikethrough()` for removed tokens). Test each classification row in the edit table above, a moved symbol across files, a partial file, a syntax file, an excluded file, a dirty-only snapshot rejection, a hash mismatch failing one file while the rest of the diff completes, and the shape diff in ANSI, markdown, HTML, and JSON for a one-line signature, a struct field change, and a parameter-type change grouped as removed plus added.
7. Extend the PEG query (`references`, `definitions`, `callers`, `callees`, `implements`, `search`) and the CLI and API (`diff`), then wire the explorer tree, Monaco ranges, a symbol search box, and a diff view. Test exact positions, several heads defining the same symbol, incomplete coverage reporting, and pagination. Verify the visible actions in the local browser.
8. Benchmark initial, unchanged, body-only, exported-shape, dependency-bump, deep-history, multi-head, and diff runs on a real workspace; inspect both engines' plans for the posting semi-join and the CTE. Decide the phase-2 head table from those numbers. Run `make build`, `make lint`, and `go test ./...`.
