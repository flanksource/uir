# Symbol index benchmarks

Step 8 of [the symbol index design](symbol-index-storage.md): wall time, row counts and database size on two real workspaces, the plans of the posting intersection and the base-chain CTE on both engines, the one optimization this step implemented, and a finding about document size.

## Setup

- Workspaces: this repository (`github.com/flanksource/uir`, 111 non-test files, 14 packages) and `github.com/flanksource/commons` (two roots: `commons` and the nested `commons/cmd/hx`, 108 non-test files, 39 packages). Each is copied from its working tree (without `.git`, `node_modules`, `.tmp`, `.bin`, `web/dist`, `go.work`) into a fresh Git repository under `$TMPDIR/s9bench/` and committed with a fixed author, so `worktree_state` is `clean` for every snapshot. Every commit script checks `git rev-parse --show-toplevel` against `$TMPDIR/s9bench/` first.
- Binary: `make build`, then `.bin/uir` copied to `.tmp/bench/uir-baseline` before the optimization and to `.tmp/bench/uir-optimized` after it. The per-edit tables and the SQLite deep history use the baseline binary. The PostgreSQL deep history uses the optimized one, which behaves identically whenever sources change. Every run uses `GOWORK=off` (as the Makefile does) and `--no-progress`, without `--force` and without `--include-tests`.
- Engines: SQLite at `.tmp/bench/<name>.db`, and PostgreSQL 16 on the shared embedded server the integration tests use (`dbtest`, `localhost:7432`), database `uir_bench_s9`, schema `uir_bench`.
- Host: Apple Silicon laptop shared with several other agents; the load average stayed between 26 and 30 during every run, so wall times carry roughly ±30 % noise. Row counts and sizes are exact.
- Scripts: `.tmp/bench/lib.sh`, `scenario.sh`, `deep.sh`, `queries.sh`, `repeat.sh`, `compare.sh`, `explain.sh` (scratch, not committed). The exact invocations are listed under [Commands](#commands).

## Headline

- An unchanged re-run no longer type-checks: 3.3 to 4.5 s before, 0.14 to 0.67 s after, on both workspaces and both engines. That is the optimization under [Unchanged runs skip type-checking](#unchanged-runs-skip-type-checking).
- Every other run costs about the same as an unchanged run did before the optimization: 4 to 15 s depending on host load, whatever the edit. That means the `go/packages` load dominates, and publication was not measured separately.
- Documents are 66 to 72 % of the SQLite file, at about 145 KB per document: 1.4 KB per declared symbol and 235 B per occurrence. After 200 single-file edits of one 8-file package the commons database grew from 17.5 MB to 350 MB, or 1.66 MB per commit.
- The base-chain CTE over a 200-link chain takes about 1 ms on SQLite and 0.9 ms on PostgreSQL. The posting lookup is a covering index range scan on both engines. What each head derivation actually spends is in reading every active document's `content` (about 15 MB for commons), not in the CTE, so the phase-2 head table is not warranted (see [Phase-2 decision](#phase-2-decision)).

## Runs per edit

Each row is one `uir add` (initial) or `uir reindex` over the fixture after the stated commit, using the baseline binary. Counts are totals after the run. Size is the SQLite file (`stat`) or `pg_database_size`.

### github.com/flanksource/uir, SQLite

| Run | Wall s | Snapshots | Documents | Postings | Symbols | package_coverage | Size |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| initial | 14.7 | 1 | 111 | 12,644 | 4,311 | 14 | 27.3 MB |
| unchanged | 11.5 | 1 | 111 | 12,644 | 4,311 | 14 | 27.3 MB |
| body-only edit, `storage/active_documents.go` | 10.3 | 2 | 121 | 13,434 | 4,311 | 28 | 28.7 MB |
| exported signature, `storage.EffectiveSources` gains `_ ...string` | 10.6 | 3 | 182 | 19,430 | 4,312 | 42 | 40.4 MB |
| `go.sum` bump | 10.7 | 4 | 182 | 19,430 | 4,312 | 56 | 40.4 MB |
| unchanged again | 9.0 | 4 | 182 | 19,430 | 4,312 | 56 | 40.4 MB |

### github.com/flanksource/uir, PostgreSQL

| Run | Wall s | Snapshots | Documents | Postings | Symbols | package_coverage | Size |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| initial | 5.8 | 1 | 111 | 12,644 | 4,311 | 14 | 25.6 MB |
| unchanged | 4.1 | 1 | 111 | 12,644 | 4,311 | 14 | 25.8 MB |
| body-only edit | 4.6 | 2 | 121 | 13,434 | 4,311 | 28 | 26.6 MB |
| exported signature | 5.7 | 3 | 182 | 19,430 | 4,312 | 42 | 32.4 MB |
| `go.sum` bump | 5.9 | 4 | 182 | 19,430 | 4,312 | 56 | 32.3 MB |
| unchanged again | 4.5 | 4 | 182 | 19,430 | 4,312 | 56 | 32.3 MB |

### github.com/flanksource/commons (two roots), SQLite

| Run | Wall s | Snapshots | Documents | Postings | Symbols | package_coverage | Size |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| initial | 7.4 | 2 | 108 | 7,368 | 3,091 | 39 | 17.5 MB |
| unchanged | 5.3 | 2 | 108 | 7,368 | 3,091 | 39 | 17.5 MB |
| body-only edit, `logger/buffered.go` | 6.5 | 4 | 116 | 8,244 | 3,091 | 78 | 19.1 MB |
| exported signature, `logger.IsSensitiveKey` gains `_ ...string` | 5.9 | 6 | 168 | 12,315 | 3,092 | 117 | 26.8 MB |
| `go.sum` bump | 4.7 | 8 | 168 | 12,315 | 3,092 | 156 | 26.9 MB |
| unchanged again | 4.6 | 8 | 168 | 12,315 | 3,092 | 156 | 26.9 MB |

What the rows show:

- The identical workload ran two to three times faster in the PostgreSQL series than in the first SQLite series. The difference is host load, not the engine: an unchanged run writes nothing, and the later alternating comparison (below) puts both engines at 3.3 to 4.5 s.
- Body-only edit: exactly the edited package is re-stored (uir: 10 documents for the 10-file `storage` package, commons: 8 for `logger`), with identical postings under new document ids. No symbol row is added.
- Exported signature change: the package and every transitive importer get new documents (uir: 61, commons: 52). One symbol is added, the new identity, because a parameter-type change is a new identity and the old row is retained. This run adds 11.7 MB (uir) and 7.7 MB (commons).
- `go.sum` bump: one new snapshot per root with zero source deltas, zero new documents and zero new postings, plus `P` package_coverage rows. It costs 12 to 20 KB.
- Every commit also makes a new snapshot for every other root of the checkout, because `revision` is part of the unchanged check. commons' nested `cmd/hx` root gets one zero-delta snapshot per commit, which is why the snapshot count grows by two.

## Deep history

`deep.sh`: 200 sequential commits to a fresh commons fixture, each inserting one `_ = N` statement into the first function of `logger/buffered.go`, each followed by `uir reindex` without `--force`.

| Measure | SQLite | PostgreSQL |
| --- | ---: | ---: |
| 200 edit runs, total | 1,212.6 s | 911.7 s |
| edit run p50 / p90 / max | 4.78 / 9.55 / 23.6 s | 4.03 / 6.00 / 12.5 s |
| snapshots after | 402 | 402 |
| documents after | 1,708 | 1,708 |
| postings after | 182,568 | 182,568 |
| symbols after | 3,091 | 3,091 |
| package_coverage after | 7,839 | 7,839 |
| size after | 350.3 MB (from 17.5 MB) | 190.1 MB (from 20.3 MB; jsonb is TOAST-compressed) |

Each commit adds 8 documents (the whole `logger` package), 876 postings, 39 package_coverage rows (both roots) and 2 snapshots. On SQLite that is 1.66 MB, 97 % of it in `documents.content` and the posting table with its two indexes. After the history the SQLite file holds `documents` at 251.5 MB, `symbol_postings` at 30.0 MB, and the two posting indexes at 34.1 and 26.1 MB. Symbols did not grow at all, because body edits add no identities.

### Queries over the deep history

After the history, a second checkout of the same commit (`git clone` of the fixture, registered with `uir add`) became a second head of `github.com/flanksource/commons`. Registering it took 12.4 s on SQLite and 6.6 s on PostgreSQL. It is a full type-check, but every document it needs already exists under its key, so the document and posting counts did not change. Each query ran three times, and the times below are those three runs.

| Query | SQLite runs, s | PostgreSQL runs, s |
| --- | --- | --- |
| `references of node where package = "…/logger" and kind = "func" and name = "GetLogger"`, one head (`--location`) | 1.92, 0.65, 0.67 | 0.26, 0.30, 0.26 |
| the same, both heads (`--root`) | 0.58, 1.17, 1.33 | 0.33, 0.32, 0.31 |
| `definitions …` same selector, both heads | 0.58, 0.63, 3.83 | 0.35, 0.31, 0.30 |
| `search "get"`, both heads | 0.64, 0.63, 1.35 | 0.38, 0.34, 0.35 |
| `uir diff <first>..<last>` (exported rows) | 0.58, 0.58, 0.83 | 0.32, 0.32, 0.32 |
| `uir diff <first>..<last> --stat --visibility all` | 0.78, 0.73, 1.01 | 0.58, 0.51, 0.50 |

References return 10 rows with one head and 20 with two, with 1 and 2 declarations. Rows are not multiplied by declaration locations. Before the second head existed, the diff across 200 commits reported one row: `NewBufferedLogger body +200 -0`, under `logger/buffered.go +200 -0` and `(file scope) +0 -0`.

After the second head was added, the same diff also lists `hack/check_uuid_hash.go` and `hack/repeat_test_flake.go` as removed. `<last>` now resolves to the clone's snapshot, the newest clean snapshot at that revision. commons lists `hack/` in `.gitignore`, so the rsync'd fixture has those files on disk and the clone does not. Both checkouts report `worktree_state = clean`, because `git status --porcelain` omits ignored files, but discovery indexes them. This is a real defect: `clean` does not currently guarantee that a snapshot's bytes are the commit's bytes (see [Follow-ups](#follow-ups)).

The spread between runs of one query is host noise; the floor for every operation is about 0.6 s, of which process start, opening the database and one membership derivation per head are the bulk.

## Query plans

The plans below come from the deep-history database: 200-link base chain, 1,708 documents, 182,568 postings, target `logger.GetLogger` (205 reference postings across all documents of the root; the probe uses the first 256 document ids in id order, which hold 32 of them on SQLite and 28 on PostgreSQL).

### SQLite (`EXPLAIN QUERY PLAN`)

Posting count and range read (`rootPostings`, reading the posting side):

```text
`--SEARCH symbol_postings USING COVERING INDEX symbol_postings_symbol_idx (symbol_id=? AND role=? AND root_id=?)
```

Posting probe with a batch of active document ids (`rootPostings`, probing the active side):

```text
`--SEARCH symbol_postings USING COVERING INDEX symbol_postings_symbol_idx (symbol_id=? AND role=? AND root_id=? AND document_id=?)
```

Recursive CTE (`storage.effectiveDeltasSQL`):

```text
|--CO-ROUTINE tail
|  |--MATERIALIZE chain
|  |  |--SETUP
|  |  |  `--SEARCH snapshots USING INDEX sqlite_autoindex_snapshots_1 (id=?)
|  |  `--RECURSIVE STEP
|  |     |--SCAN c
|  |     `--SEARCH s USING INDEX sqlite_autoindex_snapshots_1 (id=?)
|  |--SCAN chain
|  `--USE TEMP B-TREE FOR ORDER BY
|--MATERIALIZE roots
|  |--USE TEMP B-TREE FOR count(DISTINCT)
|  `--SCAN chain
|--MATERIALIZE ranked
|  |--CO-ROUTINE (subquery-7)
|  |  |--SCAN c
|  |  |--SEARCH d USING INDEX sqlite_autoindex_source_deltas_1 (snapshot_id=?)
|  |  `--USE TEMP B-TREE FOR ORDER BY
|  `--SCAN (subquery-7)
|--SCAN t
|--SCAN r
`--SCAN e LEFT-JOIN
```

Each recursion step is one primary-key probe of `snapshots`. The delta join probes the `(snapshot_id, path_key)` primary key per chain link. The only scans are over the materialized chain (201 rows) and the ranked deltas (310 rows). `sqlite3 -cmd .timer on` measures the CTE at 1 ms and the posting range at under 1 ms. By contrast, reading the `content` of the head's 108 active documents (15.1 MB) takes 128 ms. That read is what `storage.ActiveDocuments` does on every derivation, because it selects whole `documents` rows.

### PostgreSQL (`EXPLAIN (ANALYZE, BUFFERS)`)

Same deep history, loaded into PostgreSQL 16 (`uir_bench` schema). The plans are verbatim except for three things: the 64-hex symbol id and the 256-id array are abbreviated with `…`, and the `Planning: Buffers` lines are dropped.

Posting count and range read:

```text
 Index Only Scan using symbol_postings_symbol_idx on symbol_postings  (cost=0.42..61.93 rows=67 width=81) (actual time=0.054..0.180 rows=205 loops=1)
   Index Cond: ((symbol_id = '6deb1f88…7515'::text) AND (role = 'reference'::text) AND (root_id = 'b1ab6675-89fb-447a-a1c7-00fd3042bef4'::uuid))
   Heap Fetches: 39
   Buffers: shared hit=48
 Planning Time: 0.296 ms
 Execution Time: 0.202 ms
```

(The `count(*)` form is the same index-only scan under an `Aggregate`, with an execution time of 0.267 ms.)

Posting probe with 256 document ids:

```text
 Index Only Scan using symbol_postings_symbol_idx on symbol_postings  (cost=1.06..62.90 rows=10 width=81) (actual time=0.071..0.204 rows=28 loops=1)
   Index Cond: ((symbol_id = '6deb1f88…7515'::text) AND (role = 'reference'::text) AND (root_id = 'b1ab6675-89fb-447a-a1c7-00fd3042bef4'::uuid))
   Filter: (document_id = ANY ('{…256 ids…}'::uuid[]))
   Rows Removed by Filter: 177
   Heap Fetches: 39
   Buffers: shared hit=48
 Planning Time: 0.656 ms
 Execution Time: 0.217 ms
```

Recursive CTE:

```text
 Nested Loop Left Join  (cost=272.93..274.69 rows=1 width=82) (actual time=0.725..0.795 rows=103 loops=1)
   Buffers: shared hit=616
   CTE chain
     ->  Recursive Union  (cost=0.27..260.01 rows=31 width=52) (actual time=0.009..0.225 rows=201 loops=1)
           Buffers: shared hit=603
           ->  Index Scan using snapshots_pkey on snapshots  (cost=0.27..8.29 rows=1 width=52) (actual time=0.008..0.008 rows=1 loops=1)
                 Index Cond: (id = '3bb90566-cc82-425e-b170-4eb066b107a8'::uuid)
                 Buffers: shared hit=3
           ->  Nested Loop  (cost=0.27..25.14 rows=3 width=52) (actual time=0.001..0.001 rows=1 loops=201)
                 Buffers: shared hit=600
                 ->  WorkTable Scan on chain c_1  (cost=0.00..0.22 rows=3 width=20) (actual time=0.000..0.000 rows=1 loops=201)
                       Filter: (depth < 100000)
                 ->  Index Scan using snapshots_pkey on snapshots s  (cost=0.27..8.29 rows=1 width=48) (actual time=0.001..0.001 rows=1 loops=201)
                       Index Cond: (id = c_1.base_id)
                       Buffers: shared hit=600
   ->  Nested Loop  (cost=2.32..2.34 rows=1 width=28) (actual time=0.313..0.314 rows=1 loops=1)
         Buffers: shared hit=609
         ->  Limit  (cost=0.78..0.78 rows=1 width=20) (actual time=0.276..0.276 rows=1 loops=1)
               Buffers: shared hit=606
               ->  Sort  (cost=0.78..0.85 rows=31 width=20) (actual time=0.276..0.276 rows=1 loops=1)
                     Sort Key: chain.depth DESC
                     Sort Method: top-N heapsort  Memory: 25kB
                     Buffers: shared hit=606
                     ->  CTE Scan on chain  (cost=0.00..0.62 rows=31 width=20) (actual time=0.010..0.259 rows=201 loops=1)
                           Buffers: shared hit=603
         ->  Aggregate  (cost=1.54..1.55 rows=1 width=8) (actual time=0.036..0.037 rows=1 loops=1)
               Buffers: shared hit=3
               ->  Sort  (cost=1.39..1.47 rows=31 width=16) (actual time=0.024..0.029 rows=201 loops=1)
                     Sort Key: chain_1.root_id
                     Sort Method: quicksort  Memory: 29kB
                     Buffers: shared hit=3
                     ->  CTE Scan on chain chain_1  (cost=0.00..0.62 rows=31 width=16) (actual time=0.000..0.008 rows=201 loops=1)
   ->  Subquery Scan on e  (cost=10.60..12.33 rows=1 width=54) (actual time=0.410..0.474 rows=103 loops=1)
         Filter: (e.path_rank = 1)
         Buffers: shared hit=7
         ->  WindowAgg  (cost=10.60..11.66 rows=53 width=66) (actual time=0.410..0.468 rows=103 loops=1)
               Run Condition: (row_number() OVER (?) <= 1)
               Buffers: shared hit=7
               ->  Sort  (cost=10.60..10.74 rows=53 width=58) (actual time=0.404..0.412 rows=303 loops=1)
                     Sort Key: d.path_key, c.depth
                     Sort Method: quicksort  Memory: 50kB
                     Buffers: shared hit=7
                     ->  Hash Join  (cost=1.01..9.08 rows=53 width=58) (actual time=0.037..0.102 rows=303 loops=1)
                           Hash Cond: (d.snapshot_id = c.id)
                           Buffers: shared hit=4
                           ->  Seq Scan on source_deltas d  (cost=0.00..6.58 rows=258 width=70) (actual time=0.003..0.034 rows=310 loops=1)
                                 Buffers: shared hit=4
                           ->  Hash  (cost=0.62..0.62 rows=31 width=20) (actual time=0.029..0.029 rows=201 loops=1)
                                 Buckets: 1024  Batches: 1  Memory Usage: 19kB
                                 ->  CTE Scan on chain c  (cost=0.00..0.62 rows=31 width=20) (actual time=0.000..0.011 rows=201 loops=1)
 Planning Time: 0.589 ms
 Execution Time: 0.892 ms
```

Both engines walk the chain with one `snapshots` primary-key probe per link: 201 loops on PostgreSQL, with the planner's estimate of 31 rows well below the actual 201. Both answer the posting side from `symbol_postings_symbol_idx` alone. PostgreSQL applies the probe's `document_id` list as a filter on the `(symbol, role, root)` range rather than as an index condition. That is the right choice while one symbol's posting range is a few hundred rows.

One plan difference will matter at scale. With only 310 delta rows in the whole table, PostgreSQL hash-joins a sequential scan of `source_deltas` against the chain. Once `source_deltas` grows beyond a few pages per chain link, it should switch to probing the `(snapshot_id, path_key)` primary key, as SQLite already does. The single-location history here is too small to show that switch.

## Unchanged runs skip type-checking

**Change.** `IndexModules` now asks, before extracting each root, whether its head snapshot can be reused: `indexer/unchanged.go`, `reusableHead`. It can when all of these hold:

- the run is not `--force`;
- the location has a head whose `revision`, `content_set_hash` (sources plus `go.mod`, `go.sum`, `go.work`, `go.work.sum`) and `configuration_hash` equal the discovered root's;
- no file of the root imports a package of another module that the toolchain loads from disk. Such modules are a `go.work` `use`, or the target of a local-path `replace` in `go.work` or `go.mod`. They are matched by longest module-path prefix, so a nested workspace module is not mistaken for the root's own package.

Then every package's input hash is unchanged by construction. The sources, dependency versions and toolchain are fixed. Standard-library and module-cache export shapes are functions of those alone. The root's own packages' shapes recurse only through them. So the root is reported `Unchanged` without a `go/packages` load. Inside the publication transaction the head must still be that snapshot, or the run fails with `location … head moved from snapshot … during indexing`. A root that imports a workspace sibling is always type-checked and compared by `context_hash` as before. The loud paths stay: an unreadable or module-less `go.work` use directory, a malformed `go.mod` or `go.work`, and a file whose imports no longer parse all fail the run.

**Why siblings are not compared against `package_coverage`.** The slice brief asked to compare each imported sibling's export shape with the shapes recorded in `package_coverage`. That comparison is unsound with the current schema. A root's base snapshot records its own packages' shapes, not the sibling shapes it consumed. A sibling's own snapshots record only the states that were indexed, while a root's typed load reads the sibling from disk. Take this sequence: edit the sibling, index only the root, revert the sibling, index both. The sibling's head then equals its disk, but the root's base consumed the edited shape, so a head comparison would wrongly skip the root. Making the sibling case skippable needs the consumed shapes persisted per snapshot, for example `(snapshot_id, import_path, export_shape_hash)` rows for workspace-sibling imports only. That is a storage change and is proposed rather than made here.

**Measured.** `compare.sh` alternates the two binaries on unchanged re-runs of the scenario fixtures:

| Fixture | Baseline runs, s | Optimized runs, s |
| --- | --- | --- |
| uir, SQLite | 4.52, 3.62, 3.59 | 0.44, 0.67, 0.14 |
| uir, PostgreSQL | 4.38, 3.84, 4.42 | 0.32, 0.32, 0.25 |
| commons (2 roots), SQLite | 3.42, 3.26, 3.53 | 0.16, 0.15, 0.15 |

The optimized result row is identical: `parsed_files 0`, `reused_files 111`, `unchanged true`, same snapshot id. The real uir checkout with its `go.work` active (it `use`s `../clicky`, `../commons` and others, which uir imports) still type-checks on an unchanged run (3.2 s, 14 s of CPU). That is by design.

**Tests.** `indexer/unchanged_skip_integration_test.go`, on SQLite and PostgreSQL, counts loads through `Indexer.loadPackages`, the `go/packages` seam `extractModule` now takes:

- An unchanged re-run performs no load and returns the head. A body edit loads once, and `--force` always loads.
- In a `go.work` of `pricing` and `shop`, where shop imports pricing: an unchanged run loads only shop. A pricing body change leaves shop unchanged by context hash. A pricing export change republishes shop with zero source deltas, the same content set and configuration hash, and a new context hash.
- A missing `go.mod` in a `go.work` use directory fails the run loudly.

The existing context-hash tamper test in `module_trigger_integration_test.go` now uses the sibling workspace, because a root without siblings no longer recomputes its context hash when nothing changed.

## Document size

On the uir database (182 documents, 26.3 MB of `content`, 66 % of the file):

| Part | Bytes | Share | Per item |
| --- | ---: | ---: | ---: |
| `occurrences` (81,886) | 19.2 MB | 73 % | 235 B per occurrence |
| of which `enclosing` ids (78,021 × 64 hex plus key) | 6.0 MB | | |
| of which `symbol` ids (47,136 × 64 hex plus key) | 3.5 MB | | |
| of which `enclosing_key` on call occurrences (9,088) | 1.2 MB | | 133 B per call |
| of which `target` on call occurrences | 0.7 MB | | |
| `symbols` (4,945) | 7.0 MB | 27 % | 1,426 B per symbol |
| of which `payload` (UIR node JSON) | 1.7 MB | | |
| of which `id`, `shape_hash`, `body_hash`, `semantic_hash` | 1.2 MB | | |
| of which `key` plus `parent_key` | 0.7 MB | | |
| of which `identifier` | 0.6 MB | | |
| of which `shape` | 0.3 MB | | |

The average document is 145 KB and the largest is 597 KB. On the commons deep history, documents are 72 % of 350 MB.

**Proposal.** Give each document a symbol table: a `symbols_used` array listing each distinct canonical id the document mentions once. Occurrences then carry `symbol` and `enclosing` as integer indexes into it, and into `symbols` for enclosing declarations. That turns about 150 B of hex ids per occurrence into a few bytes. For uir, 9.5 MB of ids become about 1.3 MB of indexes, plus about 1.3 MB of table (roughly `k` 67-byte entries per document, which the posting count bounds).

Two more cuts take roughly a third off `content` with no loss of information:

- Drop `enclosing_key`, which is the enclosing entry's `key`, and `statement_path`, which is the call's ordinal within its enclosing declaration. Both can be derived when reading.
- Stop repeating a declaration's identity three times (`key`, `identifier`, and inside `payload`).

It is a document-format change, so it needs a new `indexer_version`. Postings, and the publisher's check that every posting symbol appears in the document, read the table directly.

A larger lever on history growth is the package-granular input hash the design already flags. Every one of the 200 edits re-stored all 8 `logger` documents for one changed file. The per-file input hash refinement would store one, cutting history growth about 8× for this package.

## Phase-2 decision

Do not add `head_documents`. The measured derivation per head is 1 ms of CTE plus one batched document lookup, and a two-head query costs about what a one-head query does within host noise. The per-head cost that does exist is `storage.ActiveDocuments` selecting `documents.*`, content included, for every active path: 15 MB for commons, and for uir about 111 × 145 KB ≈ 16 MB per head. Queries then decode only the few documents a posting selected. The proposed fix is a storage change: select everything but `content` in the membership lookup and load `content` only for the documents a posting or diff selects. It helps `query` and `symboldiff` alike. The head table would not remove that read, because it too would have to fetch `content` to answer anything.

## Follow-ups

These are proposed, not implemented; each lies outside this step's scope.

- **Content-free membership** (`storage/active_documents.go`). Select `documents` without `content` in `ActiveDocuments`, and load `content` for the selected ids only, in `query/module_index.go` (`indexContext.document`) and `symboldiff`.
- **Consumed sibling shapes** (storage schema). Persist `(snapshot_id, import_path, export_shape_hash)` for each workspace-sibling import. `reusableHead` can then skip roots that import siblings, too.
- **Ignored files and `clean`** (`indexer/discovery.go`). Either skip Git-ignored `.go` files in discovery, or count them as making the worktree `dirty`, so that `clean` implies commit bytes.
- **Per-document symbol table** and the **per-file input hash**, from [Document size](#document-size).

## Commands

```sh
make build
cp .bin/uir .tmp/bench/uir-baseline            # before the optimization
psql "postgres://postgres:postgres@localhost:7432/postgres?sslmode=disable" -c "CREATE DATABASE uir_bench_s9"
bash .tmp/bench/scenario.sh uir ~/go/src/github.com/flanksource/uir sqlite storage/active_documents.go storage/effective_sources.go \
  'func EffectiveSources(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID)' \
  'func EffectiveSources(ctx context.Context, database *gorm.DB, snapshotID uuid.UUID, _ ...string)'
bash .tmp/bench/scenario.sh uir ~/go/src/github.com/flanksource/uir postgres …same arguments…
bash .tmp/bench/scenario.sh commons ~/go/src/github.com/flanksource/commons sqlite logger/buffered.go logger/sanitize.go \
  'func IsSensitiveKey(v string)' 'func IsSensitiveKey(v string, _ ...string)'
bash .tmp/bench/deep.sh commons ~/go/src/github.com/flanksource/commons logger/buffered.go 200 sqlite
bash .tmp/bench/queries.sh commons sqlite github.com/flanksource/commons <first> <last>
bash .tmp/bench/repeat.sh commons sqlite github.com/flanksource/commons <first> <last>
bash .tmp/bench/explain.sh sqlite github.com/flanksource/commons github.com/flanksource/commons/logger GetLogger
make build
cp .bin/uir .tmp/bench/uir-optimized          # after the optimization
bash .tmp/bench/compare.sh uir sqlite
bash .tmp/bench/compare.sh commons sqlite
bash .tmp/bench/compare.sh uir postgres
UIR=$PWD/.tmp/bench/uir-optimized bash .tmp/bench/deep.sh commons ~/go/src/github.com/flanksource/commons logger/buffered.go 200 postgres
UIR=$PWD/.tmp/bench/uir-optimized bash .tmp/bench/queries.sh commons postgres github.com/flanksource/commons <first> <last>
UIR=$PWD/.tmp/bench/uir-optimized bash .tmp/bench/repeat.sh commons postgres github.com/flanksource/commons <first> <last>
bash .tmp/bench/explain.sh postgres github.com/flanksource/commons github.com/flanksource/commons/logger GetLogger
.tmp/bench/uir-optimized add ~/go/src/github.com/flanksource/uir --no-workspace-uses --dsn .tmp/bench/uir-gowork.db   # go.work active
.tmp/bench/uir-optimized reindex ~/go/src/github.com/flanksource/uir --dsn .tmp/bench/uir-gowork.db
```

`lib.sh` exports `GOWORK=off` and a fixed Git author and date. It refuses any commit whose `git rev-parse --show-toplevel` is not the fixture itself under `$TMPDIR/s9bench/`.
