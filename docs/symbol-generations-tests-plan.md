# Symbol generations, test coverage and test flakiness

Status: plan, not started. Read-only investigation of `storage/`, `indexer/`, `query/`, `cmd/uir` and gavel's `testrunner/parsers`; no code changed.

Three facts this plan adds, each anchored to an existing seam rather than a parallel store:

1. **Symbol generation** — how many distinct implementations a symbol has had along a snapshot's history. Rides on `symbol_deltas`.
2. **Test coverage** — which symbols a test run executed, keyed to the exact immutable document it executed. Rides on `documents.ordinal` and symbol handles.
3. **Test flakiness** — differing outcomes for the same test over the same code. "Same code" is defined by (1) and (2), which is the reason to build them in one design.

Vocabulary collisions to avoid: "generation" already names schema eras in `storage/handle_cutover.go` (`indexGeneration`), and "coverage" already names index coverage (`indexed`/`partial`/`syntax`/`excluded`) on `documents`, `package_coverage` and `snapshots`. New names below are always qualified: `content_generation`, `test_coverage`, `test_*`.

## 1. Symbol generations

### Definition

`content_generation(S, h)` is 1 at the snapshot where handle `h` is first defined along `S`'s base chain, and increases by one at every later link whose `set` delta changes `content_fp`. A `set` that changes only `public_fp`/`private_fp` (a parameter rename) keeps the generation, matching the diff's `signature`-without-`body` class. It is branch-aware for free: two locations whose chains diverge count independently, because the value is carried by the chain, not by the symbol row.

`symbols` is global and identity-only; a counter there would be wrong as soon as two branches or two roots define the same id, so it does not go there.

### Seam: carry the counter on the delta row

The counter cannot be derived at query time by counting `set` rows along the chain, because `PruneHistory` rebases a survivor into one `set` per symbol (`storage/prune_apply.go:96`, `createRebasedDeltas`) and the count would collapse to 1. Storing the running value on each delta row makes it survive rebasing by copying.

| Change | Where |
| --- | --- |
| `symbol_deltas.content_generation bigint` NOT NULL, check `content_generation >= 1` | new `storage/migrations/22_symbol_generations.hcl`, added to the `//go:embed` list in `storage/database.go` |
| `SymbolDelta.ContentGeneration`, `EffectiveSymbol.ContentGeneration` | `storage/symbol_models.go`, `storage/effective_symbols.go` |
| `effectiveSymbolsSQL` and the `DefinedSymbols` CTE select the column | `storage/effective_symbols.go`, `storage/query_support.go` |
| `storage.SymbolGenerations(ctx, db, baseSnapshotID, handles)` — the `DefinedSymbols` CTE restricted to ≤256 handles, but keeping tombstones so a re-added symbol continues its count | `storage/query_support.go` |
| `deltaRows` takes the base's generations for the handles it writes and sets `prior+1` when `content_fp` differs or the symbol is new/re-added, else `prior` | `indexer/symbol_deltas.go:183` |
| Rebase copies `ContentGeneration` | `storage/prune_apply.go:96` |

`createSymbolDeltas` is the only publication writer (`indexer/publication.go:227`; external publication reaches it through `publishSnapshotRows`), so Go and external producers get generations from one change. Its cost stays proportional to changed packages: the extra read is one handle-restricted CTE over the handles that already get a row.

Tombstones carry the generation they ended at, so `delete` rows also get the column. The current check on `symbol_deltas` (fingerprints NULL iff `delete`) is unchanged; `content_generation` is NOT NULL on both. Rebase writes no tombstones, so a symbol deleted before a pruned boundary restarts at 1 if it reappears; this is documented, not hidden.

### Existing databases

Every value is derivable from the stored deltas, so the cutover backfills instead of forcing `uir reindex` (which cannot recreate retained historical snapshots). It follows the `document_membership` pattern (`18_document_membership.hcl`, `storage/membership_cutover.go`): a one-row progress table `symbol_generation_backfill(id, through_ordinal, complete)`, bounded resumable transactions over snapshots in ordinal order (a base always has a lower ordinal than its dependant, so its generations are final when read), and `storage.OpenReadOnly` refusing an incomplete backfill with a named error. The column is added nullable by 22, filled, and tightened to NOT NULL by a follow-up migration in the same release, because SQLite cannot add a NOT NULL column without a default and a default of 1 would be silently wrong.

### Surfaces

- SQL: `symbol.generation` (integer) next to `symbol.metrics.*` in `query/sql_metric_fields.go` / `query/sql_metrics.go`, read from `EffectiveSymbol` without decoding documents. `uir query 'SELECT symbol.name, path, symbol.generation FROM func:* WHERE symbol.generation > 10 ORDER BY symbol.generation DESC'`.
- Since queries (`query/graph_since.go`): `generation delta` between the two sides is a subtraction of two effective rows.
- Timeline: `uir history symbol <selector>` lists the snapshots (and their commits) where the generation advanced, read from the existing `symbol_deltas_handle_idx (symbol_handle, snapshot_ordinal)` walked along the selected chain. It lives under the existing `history` command in `cmd/uir/history_command.go`.
- JSON payloads that already carry a symbol's fingerprints carry `content_generation`.

### Caveat that needs a decision

Historical publication records its delta "from the current head when one exists" (`docs/historical-import.md`). Importing older commits on top of a newer head would therefore count generations in import order, not commit order. Generations are only meaningful when historical snapshots are based on their Git parent's snapshot. See open decision D2.

## 2. Test identity and runs

### Test identity

A Go `TestX` is a canonical `func` symbol only in snapshots indexed with `IncludeTests` (`indexer/discovery.go:95`, part of `configurationHash`). A Ginkgo spec is not a symbol at all: it is an `It` call inside `var _ = Describe(...)`. Gavel already normalizes every framework into `parsers.Test` (`gavel/testrunner/parsers/types.go:65`): `Framework`, `Package`, `Suite []string`, `Name`, `File`, `Line`, `EndLine`, `Attempts`. Test identity is therefore its own table, keyed by name, and optionally linked to a symbol:

| Table | Columns | Keys |
| --- | --- | --- |
| `test_cases` | `id uuid`, `root_id uuid`, `framework text`, `package_path text`, `name_path text` (suite segments + name, length-delimited canonical encoding), `symbol_handle bigint?` | PK `id`; UQ `(root_id, framework, package_path, name_path)`; `root_id → modules.id` CASCADE; `symbol_handle → symbols.handle` NO ACTION |

`symbol_handle` is set when the run's snapshot defines the test function (Go `TestX`, or the declaration whose extent encloses a Ginkgo spec's `File:Line` when it is proven). Pruning's orphan-symbol scan must count it as a reference (`storage/prune_symbols.go`, `snapshotHandles`).

### Runs and results

| Table | Columns | Keys |
| --- | --- | --- |
| `test_runs` | `id uuid`, `root_id uuid`, `snapshot_id uuid?`, `producer text`, `environment_hash text` (GOOS/GOARCH, toolchain, `-race`, tags, `-count`), `started_at`, `finished_at`, `task_run_id text?` | PK `id`; `(root_id, snapshot_id) → snapshots` ON DELETE SET NULL |
| `test_results` | `run_id uuid`, `test_case_id uuid`, `attempt integer`, `outcome text` (`pass`,`fail`,`skip`,`timeout`,`aborted`), `duration_ms bigint`, `failure_digest text?`, `closure_fingerprint text?`, `closure_source text?` | PK `(run_id, test_case_id, attempt)`; index `(test_case_id, closure_fingerprint)` |

A run binds to the snapshot whose bytes the tests executed. The import fails unless `indexer.CheckCurrent` (`indexer/check_current.go`) proves that the checkout's current content set equals a stored snapshot, with the same `IncludeTests` configuration. There is no "nearest snapshot" fallback. Results outlive pruning (`snapshot_id` becomes NULL); the closure fingerprint they carry keeps them usable for flakiness.

### Ingestion seam

uir defines a producer-neutral JSON contract (`test_runs` + nested results + optional coverage blocks), validated like external publication JSON, and imports it with `uir tests import <file>`, exposed through the same clicky operation as an API route. Gavel gets an exporter that writes that contract from `parsers.Test`, `Attempts` included. uir does not import gavel and does not parse `go test` output itself: gavel's parsers are the proven implementation. The gavel exporter is a cross-repo change (D4).

## 3. Test coverage

### What is stored

Coverage is a fact about one test run executing one immutable document, so it is keyed by document ordinal and symbol handle, never by path and line alone:

| Table | Columns | Keys |
| --- | --- | --- |
| `test_coverage` | `run_id uuid`, `test_case_id uuid?` (NULL = the run's aggregate profile), `document_ordinal bigint`, `symbol_handle bigint?` (NULL = file scope), `statements integer`, `covered integer` | PK `(run_id, test_case_id, document_ordinal, symbol_handle)` with portable NULL handling (sentinel 0 for file scope, as `symbol_modules` reserves 0); index `(symbol_handle, document_ordinal)`; `document_ordinal → documents.ordinal` CASCADE |

### Mapping blocks to symbols

The import seam receives normalized blocks `(path_key, start_line, start_col, end_line, end_col, statements, count)`. Adapters produce them: Go `-coverprofile` first (lines and columns are already in this shape), lcov later for vitest/external roots. At import, the run's active documents for the covered paths are loaded with `storage.DocumentContents`. Each block is attributed to the innermost proven declaration whose `extent` contains it, and otherwise to file scope. This is the same segmenting the diff's line attribution uses (`symboldiff`, see `docs/symbol-index-storage.md` "Line attribution"), so the partition logic is shared, not re-implemented. A block that crosses an extent boundary, or names a path the snapshot does not serve, fails the import with path and position.

Queries never decode documents for coverage. They read `test_coverage` by handle.

### Reuse across snapshots via generations

A coverage row stays valid for a later snapshot exactly while the symbol's `content_generation` there equals the generation of the covered document's snapshot. `symbol.test_coverage` and `symbol.tests` in SQL therefore read the newest coverage whose generation matches, and report the generation they read (`symbol.test_coverage_generation`), so stale coverage is visible, not silently reused. This is the first consumer that makes generations load-bearing.

### Granularity

`go test -coverprofile` is per package process. Per-test attribution needs one profile per test (`-run '^TestX$'`). For Ginkgo, per-spec attribution needs a focus run per spec. Aggregate coverage (`test_case_id` NULL) is cheap and comes first. Per-test coverage is opt-in and runs through gavel (D3).

## 4. Test flakiness

### Definition

A test case is flaky over a closure when results with the same `(test_case_id, closure_fingerprint, environment_hash)` include both `pass` and `fail`/`timeout`. A fail-then-pass within one run's `Attempts` is direct evidence and needs no closure.

`closure_fingerprint` is a digest (versioned, length-delimited, `internal/canonicalhash`) of the sorted `(symbol handle → canonical id, content_fp)` pairs the test depends on. `closure_source` records how that set was obtained, and nothing falls back silently:

| `closure_source` | Set of symbols | When |
| --- | --- | --- |
| `coverage` | symbols with `covered > 0` in this test's per-test coverage | per-test coverage was imported |
| `static` | the bounded call graph from the test symbol (`graph.FromRelationships` / `query/graph_source.go`) | the test case has a `symbol_handle` and the graph is complete for it |
| `snapshot` | the snapshot's `context_hash` + `content_set_hash` | neither of the above; only matches identical code |

Comparisons only group results with the same `closure_source`. The fingerprint uses `content_fp`, not `content_generation`, because generations are chain-local and two branches can reach the same content.

### Surfaces

- `uir tests flaky [--since <dur>] [--min-runs n]`: per test case, runs, failures, flake rate, and the closures that flaked.
- SQL source `tests()` with `test.name`, `test.framework`, `test.runs`, `test.failures`, `test.flake_rate`, `test.last_outcome`; and symbol fields `symbol.tests` / `symbol.flaky_tests` (tests whose closure contains the symbol) once coverage closures exist.
- Flakiness is computed by query over `test_results`; no stored rate to keep in sync.

## Phases

Each phase is TDD (Ginkgo, both SQLite and PostgreSQL), ends with `make build`, `make lint` (0 issues) and `go test ./...`, and ships its docs section (`docs/symbol-index-storage.md` for 1, a new `docs/tests.md` for 2–4) plus a gavel fixture under `fixtures/`.

1. **Generations.** Migration, models, `SymbolGenerations`, `deltaRows`, rebase copy, backfill cutover, SQL field, `history symbol`. Tests: body edit +1; parameter rename +0; comment-only +0; move between files +0; delete then re-add continues; two branches from one base count independently; prune keeps values; backfill equals fresh publication for a replayed history; external publication gets generations. Benchmark publication cost on the fixture corpus (`make fixture-corpus`, `gavel fixtures --benchmark --profile`): the extra CTE must not move single-file publication materially.
2. **Test identity and runs.** Tables, the JSON contract, `uir tests import`, `CheckCurrent` binding, prune SET NULL and orphan-symbol counting. Tests: import binds to the exact snapshot; dirty, unindexed checkout fails; Ginkgo spec links to its enclosing declaration only when proven; re-import is idempotent. Gavel exporter (separate repo, D4).
3. **Coverage.** Block adapter for coverprofile, extent attribution shared with `symboldiff`, `test_coverage`, SQL fields with generation-gated reuse. Tests: blocks inside nested funcs/closures attribute to the outermost proven declaration; file-scope blocks; boundary-crossing block fails; coverage reused after an unrelated edit and dropped after a body edit; document prune cascades coverage.
4. **Flakiness.** Closure fingerprints for all three sources, `tests flaky`, `tests()` SQL source. Tests: pass/fail on the same closure is flaky; pass/fail across a changed closure is not; in-run retry is flaky; different `environment_hash` never groups.

## Open decisions

- **D1 — layers.** Count only content changes (as requested), or also `public_fp`/`private_fp` generations? Recommendation: content only now. The column pattern extends trivially if signature churn becomes a question.
- **D2 — historical ordering.** Should historical publication base each commit on its Git parent's snapshot, so generations follow commit order? Recommendation: yes for imports that walk a range; it changes `docs/historical-import.md` behavior, so it needs approval.
- **D3 — per-test coverage cost.** Is per-test (one process per test/spec) coverage wanted, or is aggregate coverage plus static closures enough for flakiness? Recommendation: aggregate first, per-test behind a gavel flag.
- **D4 — gavel ownership.** The exporter lives in gavel, and gavel would call `uir tests import` (or its API) after `gavel test`. Confirm that cross-repo direction, rather than uir reading `.gavel/last.json` directly.
