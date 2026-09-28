---
cwd: ..
# WORKAROUND(missing-dotenv): gavel ignores a missing setup.dotenv file, so each command checks UIR_DSN and exits 78 rather than falling back to ~/.config/uir/uir.db.
# Correct fix: commons-db shell.loadDotEnv (shell/environment.go) fails on a declared dotenv that does not exist; then drop the guards and exec .bin/uir directly.
# Ref: discussed with user 2026-09-27
exec: bash
codeBlocks: [bash]
timeout: 30s
setup:
  dotenv: [../.tmp/uir-corpus.env]
---

# Set operators with a literal pipe

The pipe must reach the CLI as part of one quoted expression. Markdown table cells cannot carry it through Gavel's table parser, so these three cases use command blocks.

### command: Set union combines distinct function declarations

```bash
test -n "$UIR_DSN" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }
.bin/uir --format json query 'func:github.com/flanksource/clicky/rpc.NewOpenAPICommand | func:github.com/flanksource/clicky/rpc.NewOpenAPICommandWithConfig' --root github.com/flanksource/clicky
```

- cel: json.symbols.exists(s, s.query_name == "github.com/flanksource/clicky/rpc.NewOpenAPICommand") && json.symbols.exists(s, s.query_name == "github.com/flanksource/clicky/rpc.NewOpenAPICommandWithConfig")

### command: Parentheses preserve intersection inside union

```bash
test -n "$UIR_DSN" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }
.bin/uir --format json query 'func:github.com/flanksource/clicky/rpc.NewOpenAPICommand | (func:github.com/flanksource/clicky/rpc.NewOpenAPICommandWithConfig & pkg:github.com/flanksource/clicky/rpc)' --root github.com/flanksource/clicky
```

- cel: size(json.symbols) == 2 && json.symbols.exists(s, s.query_name == "github.com/flanksource/clicky/rpc.NewOpenAPICommandWithConfig")

### command: Missing right side of union fails parsing

```yaml
exitCode: 1
```

```bash
test -n "$UIR_DSN" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }
.bin/uir --format json query 'func:NewOpenAPICommand |' --root github.com/flanksource/clicky
```

- cel: stderr.contains("invalid_query")
