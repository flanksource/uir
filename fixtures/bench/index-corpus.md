---
cwd: ../..
# WORKAROUND(missing-dotenv): gavel ignores a missing setup.dotenv file, so each command checks UIR_DSN and exits 78 rather than falling back to ~/.config/uir/uir.db.
# Correct fix: commons-db shell.loadDotEnv (shell/environment.go) fails on a declared dotenv that does not exist; then drop the guards and exec .bin/uir directly.
# Ref: discussed with user 2026-09-27
exec: bash
codeBlocks: [bash]
timeout: 30m
setup:
  dotenv: [../../.tmp/uir-corpus.env]
---

# Index the five local Flanksource checkouts

This file is the single definition of the query fixture corpus. `make fixture-corpus` writes `.tmp/uir-corpus.env` from `UIR_CORPUS_DSN` and `UIR_CORPUS_SCHEMA`, then runs it; `gavel fixtures fixtures/bench/index-corpus.md --benchmark --profile` measures indexing on its own. Gavel runs the blocks in order, one at a time, so every checkout is indexed into the same database the way the previous Makefile loop did.

Gavel ignores a missing dotenv file, and an unset `UIR_DSN` would silently index into `~/.config/uir/uir.db`. Each block therefore refuses to run without `UIR_DSN`.

### command: Index commons

```bash
test -n "$UIR_DSN" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }
env -u GOWORK .bin/uir --format json add ../commons --no-workspace-uses
```

- cel: dyn(json).exists(m, m.root_key == "github.com/flanksource/commons" && m.files > 0)

### command: Index clicky

```bash
test -n "$UIR_DSN" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }
env -u GOWORK .bin/uir --format json add ../clicky --no-workspace-uses
```

- cel: dyn(json).exists(m, m.root_key == "github.com/flanksource/clicky" && m.files > 0)

### command: Index commons-db

```bash
test -n "$UIR_DSN" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }
env -u GOWORK .bin/uir --format json add ../commons-db --no-workspace-uses
```

- cel: dyn(json).exists(m, m.root_key == "github.com/flanksource/commons-db" && m.files > 0)

### command: Index gavel

```bash
test -n "$UIR_DSN" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }
env -u GOWORK .bin/uir --format json add ../gavel --no-workspace-uses
```

- cel: dyn(json).exists(m, m.root_key == "github.com/flanksource/gavel" && m.files > 0)

### command: Index captain

```bash
test -n "$UIR_DSN" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }
env -u GOWORK .bin/uir --format json add ../captain --no-workspace-uses
```

- cel: dyn(json).exists(m, m.root_key == "github.com/flanksource/captain" && m.files > 0)
