---
cwd: ..
# WORKAROUND(missing-dotenv): gavel ignores a missing setup.dotenv file, so each command checks UIR_DSN and exits 78 rather than falling back to ~/.config/uir/uir.db.
# Correct fix: commons-db shell.loadDotEnv (shell/environment.go) fails on a declared dotenv that does not exist; then drop the guards and exec .bin/uir directly.
# Ref: discussed with user 2026-09-27
exec: bash
args: ["-c", "test -n \"$UIR_DSN\" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }; exec .bin/uir \"$@\"", "uir", "--format", "json", "query", "--", "{{.expression}}"]
timeout: 30s
setup:
  dotenv: [../.tmp/uir-corpus.env]
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
