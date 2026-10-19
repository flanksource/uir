# Incremental Git snapshot profiling

## SQLite change-impact measurements

`impact` measures `graph --since` and history pruning against an existing, checkpointed Captain SQLite corpus. It copies the corpus before opening it, saves the running executable and records unprofiled query/transaction latency, allocated bytes and serialized response size. Setup, migrations and effective-state checks are outside the timers. The corpus must contain the two commits selected by `--from` and `--to`, with the later snapshot based on the earlier one. Defaults use the two Captain commits in `captain_run.go`.

Build the same harness against the before and after production sources, using identical sibling dependencies. Run the binaries serially in alternating order, using new output directories each time:

```bash
go run ./hack/snapshotbench impact --db .tmp/captain-profile/wave3/uir.db --out "$PWD/.tmp/impact/after-1" --label after --samples 10
```

`--only graph` or `--only prune` selects one operation. Graphs use depth 4 and a 150-node limit by default (`--depth`, `--limit`). Each selector is measured with fresh pipelines and with a reused pipeline; both have warm OS caches. Each prune uses a fresh database copy, with one survivor rebased, and verifies document/source bindings, symbol fingerprints and implements facts afterward. One warmup per cohort is excluded. These are operation measurements, not indexing or ordinary-commit acceptance measurements. PostgreSQL is excluded.

Compare completed runs from both binaries, requiring identical corpus hashes, snapshot identities, runtime, bounds and current graph digests:

```bash
go run ./hack/snapshotbench impact-report --before "$PWD/.tmp/impact/before-1,$PWD/.tmp/impact/before-2" --after "$PWD/.tmp/impact/after-1,$PWD/.tmp/impact/after-2" --out "$PWD/.tmp/impact/report"
```

Each run retains raw samples in `results.json`, its executable in `runner`, and the copied corpus in `seed.db`. The report includes median latency, allocations and response sizes; `summary.json` also records min, nearest-rank p95 and maximum.

## Historical indexing profiles

This runner profiles committed sources in isolated clones. It uses the native UIR Go indexer for mission-control-oipa and the offline OIPA filesystem lowerer for rules-dev-baseline. Original repositories are read only. Each corpus and indexing path has a separate SQLite database; each operation runs in a separate Gavel fixture invocation to preserve commit order.

Build the producer with `make build` in the OIPA checkout before starting. `go run` compiles and copies the runner before any measurement. The runner also copies the OIPA and Gavel executables into the new run directory and records their SHA-256 fingerprints and Go build information.

```bash
go run ./hack/snapshotbench run --out .tmp/snapshot-benchmark/my-run --oipa /Users/moshe/work/om/oma/oipa-cli/.bin/oipa-cli
```

The default matrix has 92 observations: 22 head operations and 24 historical operations per corpus. Head replay starts at the oldest baseline, advances through 20 first-parent transitions, and repeats the final head. Historical replay initializes the pinned tip as the current head, requests all 21 commits without moving heads, reindexes the unchanged current head, and repeats the last historical request. Release, merge and no-source-change commits remain in the sequence.

| Option | Purpose |
| --- | --- |
| `--out` | New directory beneath `.tmp/snapshot-benchmark`; an existing directory is rejected. Defaults to a timestamp. |
| `--oipa` | OIPA executable built with the directory publication support; defaults to the local OIPA checkout's `.bin/oipa-cli`. |
| `--corpus` | `all`, `go`, or `rules`; defaults to both real corpora. |
| `--path` | `all`, `head`, or `historical`; defaults to both indexing paths. |
| `--transitions` | Between 1 and 20 for smoke runs; the complete verification requires 20. |
| `--reuse-go` | Retain the complete Go observations from an existing identical pinned run while measuring rules with a rebuilt producer. Requires `all` corpus/path selections and matching toolchain/cache settings; original descriptors, artifact paths and Go binary provenance remain in the final manifest. |

The pinned tips are `2c34bc95bc95e4a75fc26cec4ea8b0e70aba6362` for mission-control-oipa and `0d8fd63cf62dcc3b0bb9bb5e9ef065d74b783932` for rules-dev-baseline. Local source paths and the chronological commit list are recorded in `manifest.json`. Source checksum files describe producer working trees at launch; binary fingerprints identify the executed code even if other agents later edit or rebuild those checkouts. Benchmark Go clones use `GOWORK=off`, with the normal shared Go caches.

Each case retains `case.json`, `case.md`, `result.json`, `gavel.json`, the Gavel log, CPU and heap profiles, execution trace, SQL recording, trace phase totals and pprof top tables. The runner emits `observations.json`, `observations.csv` and `report.md`. Failed cases and their artifacts remain available; later commits are not run after a failure.

The worker checks resolved commit identity, persisted snapshot IDs, head versions, unchanged reuse without structural writes, and source SHA-256 round-trips after reopening the database. CEL independently checks the commit, unchanged rows and historical head preservation. Focused producer tests verify query round-trips, producer invalidation, historical-only locations, rollback and pruning. Validate the completed matrix with the current verifier:

Final verification independently reopens all four databases and verifies a source sample from every retained snapshot, preferring real source files over generated `generation` and `manifest.json` metadata. It validates every effective source path as portable. Current catalogs that were automatically pruned are covered by their operation's assertions and the corresponding retained historical replay.

```bash
go run ./hack/snapshotbench verify .tmp/snapshot-benchmark/my-run
```

Run the replay evidence check together with the focused integration, build and lint gates through the reusable Gavel definition of done:

```bash
SNAPSHOT_RUN="$PWD/.tmp/snapshot-benchmark/my-run" gavel fixtures fixtures/bench/snapshot-verification.md --max-concurrent 1 --max-retries 0
```

Use one observation per case as a profiling sample. There are no timing thresholds. Native Go indexing excludes the separately timed database open; OIPA timing includes command startup, committed archive preparation, its own database open, lowering, publication and cleanup. Trace regions separate these phases. SQL recordings include the worker's storage and source verification queries. Resource profiles sample each fixture process tree; zero samples mean unmeasured. Profiling overhead, shared warm caches and concurrent machine activity limit direct timing comparisons.

Retention also differs between producers: native Go checkout replay retains all published snapshots, while the OIPA publisher automatically prunes superseded current snapshots and preserves explicitly historical snapshots. Compare row growth within each producer and path; database sizes across producers reflect both their corpus and retention policy. Row deltas are net counts after pruning: a zero delta can include publication followed by deletion. Use unchanged verdicts and SQL recordings to distinguish reuse from that work.

Parsed and reused file counts are producer-reported publication counters. Native Go historical indexing can typecheck and extract before matching a retained snapshot, then return `Unchanged` with zero file counters. Use trace regions to identify that work; zero reported counters do not establish zero extraction cost.

OIPA filesystem coverage includes transactions, business rules and copybooks, screen fields, product/plan segments and filesystem schema declarations. Database-only code rows, file definitions and exposed computations are omitted and reported in the producer output. Historical catalogs retain a manifest containing all root-to-snapshot mappings and the commit/producer fingerprint. Automatic pruning retains historical snapshots; explicit `uir prune` remains the operator-controlled retention surface.

Native Go uses the indexer's default policy with test files excluded. Committed type errors retain partial coverage in head indexing and syntax coverage for affected historical packages. Snapshot coverage is recorded in the JSON observations; exact package diagnostics remain in the databases and captured SQL. These measurements do not assert full typed coverage for every historical commit.
