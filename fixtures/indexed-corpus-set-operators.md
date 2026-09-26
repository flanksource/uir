---
cwd: ..
exec: bash
codeBlocks: [bash]
timeout: 30s
---

# Set operators with a literal pipe

The pipe must reach the CLI as part of one quoted expression. Markdown table cells cannot carry it through Gavel's table parser, so these three cases use command blocks.

### command: Set union combines distinct function declarations

```bash
.bin/uir --dsn .tmp/uir-corpus.db --format json query 'func:github.com/flanksource/clicky/rpc.NewOpenAPICommand | func:github.com/flanksource/clicky/rpc.NewOpenAPICommandWithConfig' --root github.com/flanksource/clicky
```

- cel: json.symbols.exists(s, s.query_name == "github.com/flanksource/clicky/rpc.NewOpenAPICommand") && json.symbols.exists(s, s.query_name == "github.com/flanksource/clicky/rpc.NewOpenAPICommandWithConfig")

### command: Parentheses preserve intersection inside union

```bash
.bin/uir --dsn .tmp/uir-corpus.db --format json query 'func:github.com/flanksource/clicky/rpc.NewOpenAPICommand | (func:github.com/flanksource/clicky/rpc.NewOpenAPICommandWithConfig & pkg:github.com/flanksource/clicky/rpc)' --root github.com/flanksource/clicky
```

- cel: size(json.symbols) == 2 && json.symbols.exists(s, s.query_name == "github.com/flanksource/clicky/rpc.NewOpenAPICommandWithConfig")

### command: Missing right side of union fails parsing

```yaml
exitCode: 1
```

```bash
.bin/uir --dsn .tmp/uir-corpus.db --format json query 'func:NewOpenAPICommand |' --root github.com/flanksource/clicky
```

- cel: stderr.contains("invalid_query")
