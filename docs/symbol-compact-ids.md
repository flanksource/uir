# Compact symbol handles and symbol deltas

**Status:** Landed (uncommitted on `main` as of 2026-09-27). The recommendation below was adopted: 64-bit attribute-major handles plus per-snapshot symbol deltas, as a storage surrogate next to the SHA-256 identity. The measurements in Stage 1 and Stage 2 were taken on the prototype (branch `feat/compact-symbol-handles`, worktree `../uir-compact-handles`) before it landed; where the landed code differs from the prototype, this document says so. The landed schema is described in [symbol index storage](symbol-index-storage.md) and the [storage design](../storage/README.md#compact-symbol-handles).

This builds on the [symbol index design](symbol-index-storage.md) and its [benchmarks](benchmarks.md). It asks whether a structured, bit-packed symbol id could shrink the index and speed it up:

- module and package get sequential numbers
- visibility and kind occupy fixed bit fields
- sorting therefore groups related symbols together
- bitmap or bitwise filtering works directly on the id
- snapshot-to-snapshot changes become lists of added and removed ids

Both engines are covered, and three things are measured: database and index size, indexing time, and query time.

## Constraints that shaped the options

- **Symbol ids are content-addressed.** A symbol's id is the SHA-256 of its canonical key. That digest feeds several other things:
  - `export_shape_hash`, sorted by hex id (`indexer/export_shape.go`)
  - the package input hash
  - the document key
  - every child's canonical key, through its owner id

  Sequential module and package numbers depend on indexing order, so they differ between databases. Using them as the identity would break "same input, same document" across databases. It would also force extraction, which runs before the publication transaction, to read a number registry. The structured id is therefore a **handle**: a local surrogate stored next to the SHA id, never written into documents or hashes.
- **SQLite stores the portable `uuid` type as 36-character text** (`commons-db/migrate`). Only `bytea` maps to a blob. A 128-bit handle costs 16 bytes as a blob and 36 as text, against 8 for `bigint`. SQL has no bitwise operators on `uuid`.
- **The module registry must be keyed by `module_key` text.** `std` and builtins have no `modules` row, so the registry reserves 0 for builtin (`''`) and 1 for `std`.
- **Visibility can be packed; `type_form` cannot.** Visibility is a function of the name and the owner chain, both identity inputs. `type_form` can change while the id stays the same (see the [typed selector plan](typed-selector-query-plan.md)).
- **Deltas must carry fingerprints.** A bare list of added and removed ids cannot express a signature or body change, so every delta row carries a `shape_fp` and a `body_fp`.

## Variants

Bit layouts are listed most significant bit first.

| Variant | Handle | Layout |
| --- | --- | --- |
| B0 | 64-hex SHA text everywhere (today) | – |
| H128 | `uuid` (UUIDv8) | module 12, package 16, visibility 1, kind 3, truncated SHA; version nibble and variant bits fixed |
| H64a | `bigint`, attribute-major | 0, module 11, package 16, visibility 1, kind 3, local 32 |
| H64o | `bigint`, owner-major | 0, module 11, package 16, top-level local 20, member kind 3, visibility 1, member local 12 |
| +Δ | on any H variant | `symbol_deltas(snapshot_id, root_id, handle, operation, shape_fp, body_fp)` as measured; landed keyed by `(snapshot_ordinal, symbol_handle)` with a `root_ordinal` column. The effective set is the recursive base-chain CTE used for source deltas |

**Bit budgets.** Measured on the five-checkout fixture corpus:

| Field | Used | Capacity (H64a) | Share |
| --- | ---: | ---: | ---: |
| module keys | 207 | 2,047 | 10 % |
| packages per module | 97 | 65,535 | 0.1 % |
| locals per bucket | 1,117 | 2³² − 1 | ≈ 0 % |
| H64o top-level declarations per package | 1,617 | 1,048,575 | 0.2 % |
| H64o members per owner | 223 | 4,095 | 5 % |

Dependency modules count as modules, so the module field is the only one with limited headroom. A database indexing many unrelated workspaces could reach 2,047. Taking one bit from `local` would give 4,095 modules.

## Stage 1: schema-level comparison of all variants

`hack/symbolids` replays a source database into every variant on both engines and measures each one. It publishes one transaction per snapshot, writing the same rows publication writes. The baseline schema comes from the real migrations. It checks correctness:

- The reference, membership and search queries return identical results in every variant.
- The delta diff matches `symboldiff` on the 200-commit commons history.

Results live in `.tmp/symbolids/{uir-corpus-s1,uir-corpus-s3,commons-deep-s1}/results.md`, with query plans under `plans/`.

**Fixture corpus, scale 1:**

| Engine | Variant | Database | vs B0 | Bytes/posting | Replay total |
| --- | --- | ---: | ---: | ---: | ---: |
| SQLite | B0 | 720.9 MiB | – | 452 | 15.7 s |
| SQLite | H128 | 608.8 MiB | −15.5 % | 88 | 10.3 s |
| SQLite | H64a / H64o | 598.2 MiB | −17.0 % | 64 | 10.0 s |
| PostgreSQL | B0 | 416.4 MiB | – | 527 | 21.1 s |
| PostgreSQL | H128 | 317.4 MiB | −23.8 % | 197 | 16.4 s |
| PostgreSQL | H64a / H64o | 303.5 MiB | −27.1 % | 165 | 16.0 s |

**What Stage 1 showed:**

- **Size.** H64a and H64o are the same size, and H128 is larger with no query advantage. On the history database, H64a cut size by 20.6 % on SQLite and 35.4 % on PostgreSQL. Handle allocation costs 2–5 % of transaction time, and delta writes about 5 %.
- **Where the time goes.** Deltas carry the query wins. Median SQL times, B0 against H64a with deltas:

  | Query | SQLite | PostgreSQL |
  | --- | --- | --- |
  | effective set of a head | 334 ms → 59 ms | 458 ms → 11.5 ms |
  | exported diff | 2,248 ms → 118 ms | 2,570 ms → 24.5 ms |
  | definition membership | 3.3 ms → 2.3 ms | 9.3 ms → 1.2 ms |
  | exported functions of a package (handle range) | 0.40 ms → 0.16 ms | 1.03 ms → 0.28 ms |

- **Posting table corrections.** The first posting layout, `(handle, role, document_ordinal)` with no root column, made the count-then-range-or-probe choice global. At three roots it ran 50–60× slower. The prototype keeps a root ordinal in the lookup index, and orders it `(symbol_handle, role, root_ordinal, document_ordinal)`, so a handle range no longer reads every role.
- **Set encodings.** The largest full definition set is 17,240 handles:
  - 135 KiB raw
  - 19.3 KiB as sorted varint
  - 13.1 KiB as roaring

  A sorted-slice intersection takes 57 µs, against 164 µs with a map. Deltas on the history database have a median of one row. **Roaring is not worth adding as a dependency:** sorted `[]int64` merges are enough at this scale.
- **A defect in identity.** Every `func init` in a package gets the same canonical id, affecting 11 ids in the corpus and up to 62 declarations under one id. The prototype folds their fingerprints deterministically. The underlying fix is tracked as gavel TODO `34ea4d17`.

## Stage 2: prototype with the real CLI and Gavel fixtures

The prototype implemented H64a with deltas end to end, and this is what landed:

- **Storage:** migration `07_symbol_handles.hcl` for the `symbol_modules` and `symbol_packages` registry and `symbol_deltas`; `symbols.handle` and `modules.ordinal`, `snapshots.ordinal`, and `documents.ordinal` added to 04 and 06 in place; postings keyed by handle and ordinals; and the `storage/symbolhandle` package. The landed `symbol_deltas` is keyed by snapshot and root ordinals rather than the prototype's `uuid`s (see Size below).
- **Indexer:** allocation inside the publication transaction. Each new local is one past the largest handle in its bucket, read through the handle range, with no sequences. PostgreSQL unique conflicts are retried up to three times. Deltas are computed from changed paths only.
- **Queries:**
  - postings are read by handle and root ordinal
  - existence checks read the snapshot's defined set for just the handles asked about (`storage.DefinedSymbols`), remembered per snapshot for the query
  - selectors read candidates through `handle BETWEEN` ranges instead of scanning `symbols`
  - owner chains load in batches
- **symboldiff:** added, removed, signature, body and moved are classified from fingerprints, and documents are decoded only for rows that need shape text, `--stat`, or key matching for syntax-only packages. Membership is read without content (`storage.ActiveDocuments` with `ActiveDocumentOptions{}`), and a row whose document was not decoded is named from its symbol row through `storage.DeclarationIdentifier`, the same rule the indexer uses for declarations the AST extractor does not project.
- **Cutover:** opening a database built before handles discards its index and keeps its registrations; see Risks and costs.

Behaviour is unchanged by these checks:
- All 77 corpus fixtures and the 5 history fixtures pass with both binaries on both engines.
- The query slice was also compared byte for byte against a build of its own pre-change query code, over 99 commands: every fixture plus 23 extra all-heads queries.
- `go test ./...` passes on both engines.

**Method.** `hack/stage2-bench.sh` runs each variant on each engine three times, alternating B0 (main) with H64a (prototype) so both see the same host load. Each run:
1. cold-indexes the five checkouts with `make fixture-corpus GAVEL_FLAGS=--benchmark` (`fixtures/bench/index-corpus.md`)
2. runs `gavel fixtures 'fixtures/*.md' --benchmark` (77 fixtures, 77/77 passing in every run)
3. records sizes with `hack/symbolids sizes`
4. runs `fixtures/bench/history.md`: 20 scripted commits to commons, 21 index runs, and four `uir diff` commands, with `UIR_BIN` selecting the binary. `make fixture-history GAVEL_FLAGS=--benchmark` runs it against this checkout's `.bin/uir`: it writes `.tmp/uir-history.env` from `UIR_HISTORY_DSN` (default `.tmp/uir-history.db`, recreated on every run) and `UIR_HISTORY_SCHEMA`, as `make fixture-corpus` does for the corpus

The figures below are medians of the three runs; spread between runs was at most 4 %. Raw reports are in `.tmp/stage2/{b0alt,h64a}/`.

### Size (cold corpus: 2,541 documents, 60,007 symbols, 217,911 postings)

| Engine | Object | B0 | H64a |
| --- | --- | ---: | ---: |
| SQLite | database | 467.2 MiB | 392.6 MiB (−16.0 %) |
| SQLite | `symbol_postings` incl. indexes | 103.5 MiB (498 B/row) | 15.2 MiB (73 B/row) |
| SQLite | `symbol_deltas` (`uuid` keys) | – | 11.9 MiB (52,676 rows, 237 B/row) |
| SQLite | `symbols` | 59.6 MiB | 61.3 MiB |
| PostgreSQL | database | 289.8 MiB | 227.1 MiB (−21.6 %) |
| PostgreSQL | `symbol_postings` incl. indexes | 110.3 MiB (531 B/row) | 34.2 MiB (165 B/row) |
| PostgreSQL | `symbol_deltas` (`uuid` keys) | – | 10.3 MiB (205 B/row) |
| PostgreSQL | `symbols` | 77.5 MiB | 80.8 MiB |

Documents are unchanged: 301.6 MiB on SQLite, and 87.9 MiB on PostgreSQL where the content is TOAST-compressed. They are now 77 % of the SQLite file. The measured `symbol_deltas` was keyed by `uuid` snapshot and root ids. The landed table is keyed by snapshot and root ordinals: on SQLite it measured 84 B/row and 4.2 MiB on the same corpus, before landing.

### Indexing

| Run | SQLite B0 | SQLite H64a | PostgreSQL B0 | PostgreSQL H64a |
| --- | ---: | ---: | ---: | ---: |
| cold corpus, five checkouts | 66.1 s | 58.6 s (−11 %) | 68.7 s | 64.7 s (−6 %) |
| of which captain | 16.2 s | 13.8 s | 16.7 s | 15.0 s |
| of which gavel | 16.4 s | 13.1 s | 16.0 s | 15.2 s |
| 20-commit history, 21 index runs | 71.5 s | 70.5 s | 75.0 s | 74.0 s |

Cold indexing gets faster because fewer index bytes are written. Incremental reindexing is dominated by the `go/packages` load and moves by about 1 %, which is within noise.

### Queries (77 corpus fixtures)

| Fixture file | SQLite B0 | SQLite H64a | PostgreSQL B0 | PostgreSQL H64a |
| --- | ---: | ---: | ---: | ---: |
| globs (24) | 47.1 s | 34.7 s | 35.0 s | 27.0 s |
| grammar (23) | 28.5 s | 19.1 s | 11.9 s | 10.9 s |
| bench-ops (8) | 13.9 s | 10.3 s | 8.7 s | 8.3 s |
| scoped (10) | 12.7 s | 8.6 s | 5.7 s | 5.1 s |
| invalid-grammar (7) | 6.8 s | 4.3 s | 1.2 s | 1.2 s |
| cross-root (2) | 4.3 s | 3.5 s | 3.5 s | 3.5 s |
| set-operators (3) | 4.0 s | 2.3 s | 1.8 s | 1.1 s |
| **total** | **119.5 s** | **84.9 s (−29 %)** | **69.7 s** | **59.4 s (−15 %)** |

**Attribution matters on SQLite.**
- **The fixed per-process cost.** Each `uir` process on SQLite pays a cost that grows with the database file before any query runs. A query that fails in the parser takes 0.94 s on B0 and 0.59 s on H64a, against about 0.17 s on PostgreSQL (`invalid-grammar`: 1.2 s over 7 fixtures). About 27 s of the 34.6 s SQLite gain is that fixed cost shrinking with the smaller file, not faster query plans.
- **The query-plan gain.** On PostgreSQL, which has no such floor, the gain is about 0.13 s per fixture. It is concentrated in recursive globs and selectors: the handle ranges replace full `symbols` scans, and statement counts drop, for example from 3,001 to 68 for `func:captain/...`.

### Diff (`fixtures/bench/history.md`, whole-process time)

| Command | SQLite B0 | SQLite H64a | PostgreSQL B0 | PostgreSQL H64a |
| --- | ---: | ---: | ---: | ---: |
| first..last, exported | 383 ms | 201 ms | 425 ms | 241 ms |
| first..last, `--visibility all` | 377 ms | 203 ms | 423 ms | 238 ms |
| adjacent commit | 285 ms | 193 ms | 328 ms | 237 ms |
| first..last, `--stat` | 1,188 ms | 1,165 ms | 1,245 ms | 1,213 ms |

The prototype's diff reads active document headers without `content` (then `storage.ActiveDocumentHeaders`; landed as `storage.ActiveDocuments` with `ActiveDocumentOptions{}`) and classifies from delta fingerprints. The header-only read is an id-independent optimization, the "content-free membership" follow-up in [benchmarks](benchmarks.md), and part of the diff gain comes from it. Stage 1 isolates the delta path alone: 2,248 ms → 118 ms (SQLite) and 2,570 ms → 24.5 ms (PostgreSQL) for a diff at the SQL level. `--stat` still reads both sides' documents and Git blobs of changed files, so it barely moves.

## Recommendation (adopted)

1. **Adopt H64a handles with symbol deltas as a surrogate.** Keep the SHA-256 id as the identity in documents, hashes and canonical keys. Postings shrink 3–7×, the database shrinks 16–22 %, cold indexing is 6–11 % faster, and query fixtures are 15–29 % faster, with byte-identical output.
2. **Do not use H128.** It is larger than H64a on both engines, offers no query advantage, and is only portable across databases if the module and package fields are hashed. With a 16-bit hash, collisions are expected after about 256 modules by the birthday bound.
3. **Do not replace the identity with the handle,** for the portability and extraction-order reasons above.
4. **Do not add a roaring dependency.** Sorted slices are faster than maps at these sizes, and the sets are small.
5. **Prefer attribute-major (H64a) over owner-major (H64o).** They are the same size, but H64a gives package, visibility and kind prefix ranges for selectors and the exported-API scan. Owner lookups need an `(owner_id, kind, name)` index either way: today `owner_id = ?` has no usable index and scans `symbols`.

## Risks and costs

- **Handles are local to one database.** They must never leave it through the API, JSON output or documents. Only SHA ids are exposed.
- **Visibility is packed into the handle.** If the visibility algorithm changes, publication fails loudly on a mismatched handle, and a reindex into a fresh database is required.
- **Existing databases need a rebuild.** Their rows are not migrated to the new posting layout. Opening a pre-handle database (recognised by `modules` lacking `ordinal`) runs a cutover before migration, in one transaction:
  - It drops the index tables: postings, package coverage, documents, symbols, source deltas and revisions, heads, and snapshots.
  - It keeps the registrations in `modules`, `locations` and `primary_locations`, and numbers `modules` densely.
  - It took about 5 s on a copy of the 777 MB fixture corpus database.

  `uir reindex` on each checkout rebuilds the index. Until then `uir list` and `uir get` show the root with no snapshot, and queries that select it fail saying it is registered but not indexed. `OpenReadOnly` refuses a pre-handle database.
- **Concurrent writers.**
  - PostgreSQL: allocation conflicts (unique violation, deadlock, serialization failure) retry the whole publication up to three times. This is tested with a blocked concurrent publisher.
  - SQLite: two writer processes can still fail with a busy error that is not retried.
- **Registry headroom.** The 11-bit module field is 10 % used by five checkouts.

## Follow-ups

- **SQLite per-process floor: found and fixed in commons-db, awaiting release.**
  - **Cause:** `commons-db/migrate` opened an Atlas SQLite transaction on every `uir` invocation, even with nothing to migrate. Atlas's `OpenTx` runs `PRAGMA foreign_key_check` over the whole database when the transaction opens and again when it commits.
  - **Fix:** `reconcileSQLite` now computes pending changes read-only first, via `sqlitemigrate.PendingChanges`, and only opens the transaction when there is something to apply.
  - **Effect on main's code and database:**

    | Measure | Before | After |
    | --- | ---: | ---: |
    | `storage.UirDB` | 911 ms | 10 ms |
    | one `uir` process | 0.94 s | 0.05 s |
    | 77 SQLite query fixtures | 119.5 s | 50.1 s |

    The SQLite query comparison above predates this fix. After it, most of the SQLite gain attributed to handles disappears, because the handle design's smaller file mainly shortened this scan.
- **Lazy document content in queries.** `ActiveDocuments` now takes `ActiveDocumentOptions{Content}`, and the diff reads membership without content, but the query layer still passes `Content: true` and loads every active document's content up front (`query/module_index.go`, `query/module_documents.go`). Loading only the documents a posting selects, with `DocumentContents`, would help queries on both designs.
- **Owner lookup index.** Add `(owner_id, kind, name)`, or read `:methods` through the owner's method buckets.
- **`func init` identity.** Fix the shared canonical id (TODO `34ea4d17`).
- **Documents.** They are now 77 % of the SQLite database and 39 % of the PostgreSQL one. Both the per-document symbol table and the per-file input hash from [benchmarks](benchmarks.md) still apply.
- **Missing dotenv.** Gavel ignores a missing `setup.dotenv`, which is why the corpus and history fixtures carry a `WORKAROUND(missing-dotenv)` guard. The commons-db fix is tracked as gavel TODO `7d858863`.
