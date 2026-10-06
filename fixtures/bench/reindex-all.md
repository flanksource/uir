---
cwd: ../..
exec: bash
codeBlocks: [bash]
timeout: 60m
goProfiles:
  cpu:
    file: cpu.pprof
    env: GAVEL_CPU_PROFILE
  mem:
    file: mem.pprof
    env: GAVEL_MEM_PROFILE
---

# Reindex every checkout of a real database

This fixture benchmarks `uir reindex --all` against copies of a real uir database, so it measures the database a user actually has rather than a scripted corpus. `UIR_PROFILE_BASE` names that database, `~/.config/uir/uir.db` when unset, and `UIR_BIN` selects the binary, `.bin/uir` when unset (`make binary` links it).

The base database is only ever read. The first block refuses to copy it while its `-wal` file holds pending writes, or when it changed during the copy, and otherwise copies it once to `.tmp/profile/base.db`. Each cold run copies `base.db` to `.tmp/profile/run-<n>.db` and runs `uir --dsn .tmp/profile/run-<n>.db reindex --all` with `--cpuprofile`, `--memprofile` and `--trace` writing `run-<n>.cpu.pprof`, `run-<n>.mem.pprof` and `run-<n>.trace` beside it. The no-op run repeats `--all` on `run-1.db` into `run-1-noop.*`: every checkout already has a head, so it isolates the fixed cost of opening, migrating and backfilling the database.

`fixtures/bench/reindex-all.sh` prints one JSON object per run with the row counts of `snapshots`, `documents`, `source_deltas`, `symbol_deltas`, `package_coverage`, `locations`, `location_heads` and, once a migration adds it, `snapshot_stats` (null while absent), before and after the reindex, their delta, the wall time and the uir result. `missing_heads` counts the non-external locations without a `location_heads` row: the database this fixture was written against has two, and a cold run must leave none. Under `--profile`, gavel also records each run's CPU and heap profile in its report.

```text
gavel fixtures fixtures/bench/reindex-all.md --benchmark --profile
go tool pprof -top .tmp/profile/run-1.cpu.pprof
go tool trace .tmp/profile/run-1.trace
```

### command: Copy the base database

```bash
bash fixtures/bench/reindex-all.sh base
```

- cel: exitCode == 0
- cel: dyn(json).counts.missing_heads == 2

### command: Reindex all, cold run 1

```bash
bash fixtures/bench/reindex-all.sh cold 1
```

- cel: exitCode == 0
- cel: dyn(json).case == "cold-all"
- cel: dyn(json).before.missing_heads == 2 && dyn(json).after.missing_heads == 0

### command: Reindex all, cold run 2

```bash
bash fixtures/bench/reindex-all.sh cold 2
```

- cel: exitCode == 0
- cel: dyn(json).case == "cold-all"
- cel: dyn(json).before.missing_heads == 2 && dyn(json).after.missing_heads == 0

### command: Reindex all, cold run 3

```bash
bash fixtures/bench/reindex-all.sh cold 3
```

- cel: exitCode == 0
- cel: dyn(json).case == "cold-all"
- cel: dyn(json).before.missing_heads == 2 && dyn(json).after.missing_heads == 0

### command: Reindex all again, no-op on run 1

```bash
bash fixtures/bench/reindex-all.sh noop
```

- cel: exitCode == 0
- cel: dyn(json).case == "noop-all"
- cel: dyn(json).before.missing_heads == 0 && dyn(json).after.missing_heads == 0
