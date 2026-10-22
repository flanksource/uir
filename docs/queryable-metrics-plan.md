# Queryable file and callable metrics

Status: implemented on 2026-10-19 with focused verification passing; full acceptance is blocked by the failures recorded below. Tracked TODO: `0dff6e7d-a439-4ea6-893e-883fa327068e`. Acceptance fixture: `fixtures/queryable-metrics.md`.

## Public queries

```sh
uir query 'SELECT path, file.lines, file.definitions FROM files() WHERE file.lines < 100 AND file.definitions < 3 ORDER BY path LIMIT 1000'
uir query 'SELECT symbol.name, path, line, symbol.metrics.param_count, symbol.metrics.body_lines FROM func:* WHERE symbol.metrics.param_count > 3 OR symbol.metrics.body_lines > 50 ORDER BY path, line'
```

## Semantics

- File lines count physical lines including blanks/comments. Empty content has zero lines; a final newline adds no phantom line; CRLF counts once.
- Definitions use the active document's symbol count, including fields, package variables and constants, excluding references and function-local bindings.
- Parameter counts expand grouped names, count a variadic argument once, and exclude receivers and type parameters.
- Body lines count the inclusive physical span of the body's delimiters, excluding documentation and signature-only lines. A one-line body counts as one. A declaration without a body has NULL body lines.
- Excluded files do not appear in `files()`. Syntax/partial coverage remains visible through the existing warnings.
- Complexity, nesting, saved rules, a lint runner, and historical backfill are outside this version.

## Storage and extraction

- Store immutable, versioned source metrics by source revision; store typed callable metrics on document declarations and in a compact relational projection by document ordinal and declaration key, with canonical handles and name positions where available.
- Extract Go metrics from the existing syntax/type extraction, including interface signatures. External producers can supply typed callable metrics; compute their file lines from published source.
- Publish projections atomically, verify immutable reuse, and cascade deletion when source revisions or documents are pruned. Preserve semantic fingerprints.
- Add portable SQLite/PostgreSQL migrations and schema assertions. Bump document and indexer/producer versions so reindexing records metrics.
- Do not backfill during migration or query execution. Existing snapshots remain readable. A requested unavailable metric fails with snapshot/path/declaration context and reindex/republish guidance.

## Query and presentation

- Add standalone zero-argument `files()` with existing root/location/snapshot selection. Reuse SQL filtering, ordering, limits, pre-limit totals and projected response envelopes.
- Register the four integer metric fields, use source-specific binding and wildcard expansion, and reject symbol navigation involving `files()`.
- Resolve callable metrics on the same canonical declaration as other SQL symbol properties, retaining ambiguity checks.
- The two example projections read neither source bytes nor document JSON. Other projections/navigation load content only when their requested fields require it.
- Update catalog completion, API documentation, runnable examples and source witnesses. Browser result rows open the selected source.

## Acceptance

- Failing Ginkgo tests first for extraction and queries, including thresholds, grouped/unnamed/variadic parameters, receivers, multiline signatures, bodyless declarations, empty bodies and newline variants.
- Both database engines: old snapshots, unchanged reindex, layout-only changes, reuse, pruning, external publications, missing metrics and immutable conflicts.
- Prove zero source reads/document loads for metric-only projections; verify totals, ordering, numeric comparisons and witness alignment.
- Extend CLI/API/frontend checks and verify completion/results/source navigation with agent-browser.
- Run the full Go suite, frontend/API tests, make build and make lint. Measure cold/warm latency, allocations, SQL count and decoded bytes on the real indexed corpus; do not promise a latency target before measurement.

## Reproducible benchmark

Index the corpus into an isolated development database, then run the metric-only benchmark against it without migrations or source reads. Set `UIR_METRICS_BENCH_DSN` to that database and `UIR_METRICS_BENCH_ROOT` to the indexed module key. Cold samples create a fresh pipeline; warm samples prime one query first. Database and filesystem caches are shared, so cold means pipeline-cold rather than machine-cold.

```sh
make binary
mkdir -p .tmp/metrics-live
.bin/uir --dsn .tmp/metrics-live/index.db add .
UIR_METRICS_BENCH_DSN="$PWD/.tmp/metrics-live/index.db" UIR_METRICS_BENCH_ROOT=github.com/flanksource/uir go test -tags metricbench ./query -run '^$' -bench BenchmarkStoredMetrics -benchtime=1x -count=20 -benchmem
```

Each sample reports wall time, allocations, SQL statements, source reads and document-content reads. Both I/O counters must be zero. Reindex if metrics were not recorded; the benchmark fails rather than calculating them during queries.

## Measured result and verification

The isolated SQLite database `.tmp/metrics-0dff6e7d/index.db` contains UIR snapshot `3821c16b-e057-47de-a884-bceb73fb89be`: 334 indexed documents and 7,253 declared symbols. Its two example queries return 36 small files and 263 callables over either threshold. The benchmark ran on an Apple M2 Pro with Go 1.27.1, with 20 one-operation samples per case after this task's fixture run stopped. Cold means a fresh pipeline over the same database connection and shared operating-system caches. The p95 is the nearest-rank 95th percentile.

| Query | Pipeline | Median | p95 | SQL/op | Median allocated bytes/op | Median allocations/op |
| --- | --- | --- | --- | --- | --- | --- |
| Small files | Cold | 18.69 ms | 19.88 ms | 25 | 3,206,528 | 45,013 |
| Small files | Warm | 1.72 ms | 1.94 ms | 10 | 1,349,168 | 9,994 |
| Callable thresholds | Cold | 162.11 ms | 196.90 ms | 90 | 60,194,368 | 707,118 |
| Callable thresholds | Warm | 134.41 ms | 194.13 ms | 61 | 53,180,080 | 620,211 |

Every sample recorded zero source-blob reads and zero document-content reads, hence zero document-payload bytes decoded. The first file-cold sample was 193.38 ms; the percentile table does not hide that one-time outlier. Callable queries still materialize selected candidates and filter/sort in Go; these results establish the present cost, not a latency guarantee or an optimizer claim. Raw samples and summaries are in `.tmp/metrics-0dff6e7d/bench.log` and `bench-summary.json`.

Focused metrics and cutover tests pass in `storage`, `indexer/syntax`, `indexer`, `query`, `query/sqlsyntax`, and `cmd/uir`, including SQLite and PostgreSQL integration coverage. All nine SQL-completion tests and all three SQL API tests pass. Focused `go vet` and browser type checking pass. The CLI binary and production browser assets build. Agent-browser verified catalog completion, numeric result columns, pre-limit totals and navigation to the selected snapshot's source; Chrome was required because Lightpanda failed loading the editor with `EvaluationError`. The task's browser sessions and local server were closed after verification.

The full fixture ran all six gates and passed its metric specification gate. Its other gates retain these unrelated failures: `hack/postdeploymentdiff` references unavailable `sqlglot.ParseDiagnostic` and `sqlglot.ParseScript`; three graph assertions expect nil unreadable-source slices while the unchanged `guardReader.unreadablePaths` allocates empty slices; the navigation icon test omits the existing Connections destination; API tests reference missing `listConnectionSchemas` and expect the previous suggestion envelope; the pinned linter cannot read Go 1.27 export data. The installed newer linter also fails because it was built with Go 1.26. No suppression or unrelated source change was made. Full output is `.tmp/metrics-fixture.log`. `gavel todos check 0dff6e7d --timeout 20m --max-concurrent 1 --max-retries 0 --no-progress --no-color` repeated the six gates and recorded **UNVERIFIED**, with only the metric specification gate passing; its output is `.tmp/metrics-todo-check.log`. The fixture now declares timeouts per row because the TODO verifier does not accept a document-level timeout.
