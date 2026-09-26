---
cwd: ..
exec: .bin/uir
args: ["--dsn", ".tmp/uir-corpus.db", "--format", "json", "query", "{{.expression}}"]
timeout: 30s
---

# Queries across registered Flanksource modules

Run `make fixture-corpus` from the UIR repository root before running these fixtures. These expressions omit `--root` so callers in another registered module remain in scope.

| Name | expression | CEL Validation |
| --- | --- | --- |
| commons logger called by commons-db | github.com/flanksource/commons/logger.StripSecrets < +pkg github.com/flanksource/commons-db/query | json.matches.exists(m, m.root == "github.com/flanksource/commons-db" && m.path == "query/connection_log_format.go" && m.role == "call" && m.package_path == "github.com/flanksource/commons-db/query") |
| clicky task called by captain | github.com/flanksource/clicky/task.StartTask < +pkg github.com/flanksource/captain/pkg/cli | json.matches.exists(m, m.root == "github.com/flanksource/captain" && m.path == "pkg/cli/prompt_batch_run.go" && m.role == "call" && m.package_path == "github.com/flanksource/captain/pkg/cli") |
