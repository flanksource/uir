---
cwd: ..
exec: .bin/uir
args: ["--dsn", ".tmp/uir-corpus.db", "--format", "json", "query", "--", "{{.expression}}"]
timeout: 30s
---

# Invalid compact expressions fail at the grammar boundary

| Name | expression | expected | Exit Code | CEL Validation |
| --- | --- | --- | --- | --- |
| empty selector value | pkg: | query | 1 | stderr.contains(expected) |
| malformed double star segment | pkg:github.com/flanksource/***/clicky | ** must occupy a complete path segment | 1 | stderr.contains(expected) |
| invalid relative package segment | pkg:github.com/flanksource/clicky:rpc/. | . is only valid as the whole relative package pattern | 1 | stderr.contains(expected) |
| leading exclusion | -func:NewOpenAPICommand | invalid_query | 1 | stderr.contains(expected) |
| definition with right operand | func:NewOpenAPICommand = func:NewOpenAPICommandWithConfig | definition relation has no right operand | 1 | stderr.contains(expected) |
| transitive depth outside bound | func:NewOpenAPICommand <<9 | depth must be between 1 and 8 | 1 | stderr.contains(expected) |
| path depth outside bound | func:NewOpenAPICommand >>0 func:NewOpenAPICommandWithConfig | depth must be between 1 and 8 | 1 | stderr.contains(expected) |
