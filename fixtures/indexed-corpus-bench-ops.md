---
cwd: ..
# WORKAROUND(missing-dotenv): gavel ignores a missing setup.dotenv file, so each command checks UIR_DSN and exits 78 rather than falling back to ~/.config/uir/uir.db.
# Correct fix: commons-db shell.loadDotEnv (shell/environment.go) fails on a declared dotenv that does not exist; then drop the guards and exec .bin/uir directly.
# Ref: discussed with user 2026-09-27
exec: bash
args: ["-c", "test -n \"$UIR_DSN\" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }; exec .bin/uir \"$@\"", "uir", "--format", "json", "query", "{{.expression}}"]
codeBlocks: [bash]
timeout: 30s
setup:
  dotenv: [../.tmp/uir-corpus.env]
---

# Name search and lookups across every indexed head

Run `make fixture-corpus` first. The other corpus fixtures cover globs, `:methods`, and single-root lookups; these cases cover name search (`suggest`, `suggest-selectors`) and definitions and references that span several module heads, so benchmarks also time the queries that fan out over the whole corpus.

## Definitions and references across heads

| Name | expression | CEL Validation |
| --- | --- | --- |
| one name defined in several heads | func:Shutdown = | json.matches.all(m, m.role == "definition") && json.matches.exists(m, m.root == "github.com/flanksource/clicky" && m.path == "shutdown/shutdown.go") && json.matches.exists(m, m.root == "github.com/flanksource/commons-db" && m.path == "query/session_managed.go") && json.matches.exists(m, m.root == "github.com/flanksource/gavel" && m.path == "procfile/supervisor.go") |
| qualified definition stays in its own head | github.com/flanksource/clicky/task.StartTask = | json.matches.exists(m, m.path == "task/manager.go" && m.role == "definition") && json.matches.all(m, m.root == "github.com/flanksource/clicky") |
| references from several heads | github.com/flanksource/commons/logger.StripSecrets < | json.matches.all(m, m.role == "call") && json.matches.exists(m, m.root == "github.com/flanksource/commons" && m.path == "logger/http.go") && json.matches.exists(m, m.root == "github.com/flanksource/clicky" && m.path == "entity/error_sanitize.go") && json.matches.exists(m, m.root == "github.com/flanksource/commons-db" && m.path == "query/connection_log_format.go") |

## Name search

### command: Symbol search within one head

```bash
test -n "$UIR_DSN" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }
.bin/uir --format json suggest --prefix rpc.NewOpenAPI --root github.com/flanksource/clicky
```

- cel: dyn(json).exists(s, s.query_name == "github.com/flanksource/clicky/rpc.NewOpenAPICommand" && s.kind == "func") && dyn(json).all(s, s.module_key == "github.com/flanksource/clicky")

### command: Symbol search across every head

```bash
test -n "$UIR_DSN" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }
.bin/uir --format json suggest --prefix Shutdown --limit 100
```

- cel: dyn(json).exists(s, s.query_name == "github.com/flanksource/clicky/shutdown.Shutdown") && dyn(json).exists(s, s.query_name == "github.com/flanksource/commons-db/shutdown.ShutdownAndExit") && dyn(json).exists(s, s.query_name == "github.com/flanksource/gavel/procfile.Supervisor.Shutdown")

### command: Selector search completes a function

```bash
test -n "$UIR_DSN" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }
.bin/uir --format json suggest-selectors --prefix func:github.com/flanksource/clicky/rpc.NewOpenAPIC --root github.com/flanksource/clicky
```

- cel: dyn(json).exists(s, s == "func:github.com/flanksource/clicky/rpc.NewOpenAPICommand") && dyn(json).all(s, s.startsWith("func:github.com/flanksource/clicky/rpc.NewOpenAPIC"))

### command: Selector search completes a relative package

```bash
test -n "$UIR_DSN" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }
.bin/uir --format json suggest-selectors --prefix pkg:github.com/flanksource/clicky:rp --root github.com/flanksource/clicky
```

- cel: dyn(json).exists(s, s == "pkg:github.com/flanksource/clicky:rpc")

### command: Selector search completes modules across heads

```bash
test -n "$UIR_DSN" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }
.bin/uir --format json suggest-selectors --prefix mod:github.com/flanksource/c
```

- cel: dyn(json).exists(s, s == "mod:github.com/flanksource/captain") && dyn(json).exists(s, s == "mod:github.com/flanksource/clicky") && dyn(json).exists(s, s == "mod:github.com/flanksource/commons") && dyn(json).exists(s, s == "mod:github.com/flanksource/commons-db")
