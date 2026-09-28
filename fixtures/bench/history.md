---
cwd: ../..
# WORKAROUND(missing-dotenv): gavel ignores a missing setup.dotenv file, so each command checks HISTORY_DSN and exits 78 rather than falling back to ~/.config/uir/uir.db.
# Correct fix: commons-db shell.loadDotEnv (shell/environment.go) fails on a declared dotenv that does not exist; then drop the guards in history-lib.sh.
# Ref: discussed with user 2026-09-27
exec: bash
codeBlocks: [bash]
timeout: 60m
setup:
  dotenv: [../../.tmp/uir-history.env]
---

# Diff a scripted commons history

This fixture benchmarks `uir diff` over a deterministic history. It uses only CLI commands and flags that every `uir` build accepts, so the same file measures the binary of this checkout and of any other: `UIR_BIN` selects the binary, `.bin/uir` when unset.

The history lives in its own database so it never touches the query corpus (`.tmp/uir-corpus.db`). `HISTORY_DSN` names it and every block refuses to run without it; the documented default is a SQLite file under `.tmp/`, written once with:

```text
printf "HISTORY_DSN='%s'\n" .tmp/uir-history.db > .tmp/uir-history.env
```

Use one `HISTORY_DSN` per binary, because two builds may not share a schema. A SQLite `.db` path must lie under `.tmp/` and is recreated on every run; a PostgreSQL DSN (with an optional `HISTORY_SCHEMA`) is used as is, so drop its schema before a run.

The first block copies `../commons` without `.git` and without Git-ignored files (an ignored `.go` file would be indexed but untracked, making every snapshot dirty) into `.tmp/history/commons`, commits it with a fixed author and date, and indexes it. It then makes 20 commits, reindexing after each: commits 5, 10, 15, and 18 rename the parameter of `BufferedLogger.Named`, add `HistoryMarker` to `logger/buffered.go`, remove `DeleteEmptyStrings` from `collections/slice.go`, and append two lines to `go.sum`; every other commit adds one `_ = <n>` line to the body of `NewBufferedLogger`, 16 lines in all. The remaining blocks time one diff each and assert its rows.

```text
gavel fixtures fixtures/bench/history.md --benchmark
```

### command: Build a 20-commit commons history

```bash
bash fixtures/bench/history.sh
```

- cel: dyn(json).exists(m, m.root_key == "github.com/flanksource/commons" && !m.unchanged && m.files > 0)

### command: Diff first..last (exported)

```bash
source fixtures/bench/history-lib.sh
uir --format json diff HEAD~20..HEAD --root "$HISTORY_ROOT_KEY"
```

- cel: dyn(json).packages.map(p, p.path) == ["github.com/flanksource/commons/collections", "github.com/flanksource/commons/logger"]
- cel: dyn(json).packages[0].files.map(f, f.path) == ["collections/slice.go"]
- cel: dyn(json).packages[0].files[0].rows.map(r, r.class + " " + r.name) == ["removed DeleteEmptyStrings"]
- cel: dyn(json).packages[1].files.map(f, f.path) == ["logger/buffered.go"]
- cel: dyn(json).packages[1].files[0].rows.map(r, r.class + " " + (has(r.owner) ? r.owner + "." : "") + r.name) == ["signature BufferedLogger.Named", "added HistoryMarker", "body NewBufferedLogger"]

### command: Diff first..last (all visibilities)

```bash
source fixtures/bench/history-lib.sh
uir --format json diff HEAD~20..HEAD --root "$HISTORY_ROOT_KEY" --visibility all
```

- cel: dyn(json).visibility == "all"
- cel: dyn(json).packages.map(p, p.path) == ["github.com/flanksource/commons/collections", "github.com/flanksource/commons/logger"]
- cel: dyn(json).packages[0].files[0].rows.map(r, r.class + " " + r.name) == ["removed DeleteEmptyStrings"]
- cel: dyn(json).packages[1].files.map(f, f.path) == ["logger/buffered.go"]
- cel: dyn(json).packages[1].files[0].rows.map(r, r.class + " " + (has(r.owner) ? r.owner + "." : "") + r.name) == ["signature BufferedLogger.Named", "added HistoryMarker", "body NewBufferedLogger"]

### command: Diff first..last with line counts

```bash
source fixtures/bench/history-lib.sh
uir --format json diff HEAD~20..HEAD --root "$HISTORY_ROOT_KEY" --stat
```

- cel: !has(dyn(json).lines_error)
- cel: dyn(json).packages.map(p, p.path) == ["github.com/flanksource/commons", "github.com/flanksource/commons/collections", "github.com/flanksource/commons/logger"]
- cel: dyn(json).packages[0].files.map(f, f.path) == ["go.sum"]
- cel: dyn(json).packages[0].files[0].coverage == ["manifest"] && dyn(json).packages[0].files[0].lines.added == 2 && dyn(json).packages[0].files[0].lines.removed == 0
- cel: dyn(json).packages[2].files[0].rows.exists(r, r.name == "NewBufferedLogger" && r.class == "body" && r.lines.added == 16 && r.lines.removed == 0)
- cel: dyn(json).packages.all(p, p.files.all(f, !has(f.lines_error)))

### command: Diff one adjacent commit

```bash
source fixtures/bench/history-lib.sh
uir --format json diff HEAD~1..HEAD --root "$HISTORY_ROOT_KEY"
```

- cel: dyn(json).packages.map(p, p.path) == ["github.com/flanksource/commons/logger"]
- cel: dyn(json).packages[0].files.map(f, f.path) == ["logger/buffered.go"]
- cel: dyn(json).packages[0].files[0].rows.map(r, r.class + " " + r.name) == ["body NewBufferedLogger"]
