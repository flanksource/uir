---
cwd: ..
# WORKAROUND(missing-dotenv): gavel ignores a missing setup.dotenv file, so each command checks UIR_DSN and exits 78 rather than falling back to ~/.config/uir/uir.db.
# Correct fix: commons-db shell.loadDotEnv (shell/environment.go) fails on a declared dotenv that does not exist; then drop the guards and exec .bin/uir directly.
# Ref: discussed with user 2026-09-27
exec: bash
args: ["-c", "test -n \"$UIR_DSN\" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }; exec .bin/uir \"$@\"", "uir", "--format", "json", "query", "{{.expression}}"]
timeout: 30s
setup:
  dotenv: [../.tmp/uir-corpus.env]
---

# Queries across registered Flanksource modules

Run `make fixture-corpus` from the UIR repository root before running these fixtures. These expressions omit `--root` so callers in another registered module remain in scope.

| Name | expression | CEL Validation |
| --- | --- | --- |
| commons logger called by commons-db | github.com/flanksource/commons/logger.StripSecrets < +pkg github.com/flanksource/commons-db/query | json.matches.exists(m, m.root == "github.com/flanksource/commons-db" && m.path == "query/connection_log_format.go" && m.role == "call" && m.package_path == "github.com/flanksource/commons-db/query") |
| clicky task called by captain | github.com/flanksource/clicky/task.StartTask < +pkg github.com/flanksource/captain/pkg/cli | json.matches.exists(m, m.root == "github.com/flanksource/captain" && m.path == "pkg/cli/prompt_batch_run.go" && m.role == "call" && m.package_path == "github.com/flanksource/captain/pkg/cli") |
