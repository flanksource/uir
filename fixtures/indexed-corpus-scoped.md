---
cwd: ..
# WORKAROUND(missing-dotenv): gavel ignores a missing setup.dotenv file, so each command checks UIR_DSN and exits 78 rather than falling back to ~/.config/uir/uir.db.
# Correct fix: commons-db shell.loadDotEnv (shell/environment.go) fails on a declared dotenv that does not exist; then drop the guards and exec .bin/uir directly.
# Ref: discussed with user 2026-09-27
exec: bash
args: ["-c", "test -n \"$UIR_DSN\" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }; exec .bin/uir \"$@\"", "uir", "--format", "json", "query", "{{.expression}}", "--root", "{{.root}}"]
timeout: 30s
setup:
  dotenv: [../.tmp/uir-corpus.env]
---

# Queries over the five local Flanksource modules

Run `make fixture-corpus` from the UIR repository root before running these fixtures. The corpus follows the current sibling checkouts, so assertions use symbol identities and source paths rather than counts, lines, or snapshot IDs.

## Definitions

| Name | root | expression | CEL Validation |
| --- | --- | --- | --- |
| commons logger definition | github.com/flanksource/commons | github.com/flanksource/commons/logger.StripSecrets = | json.matches.exists(m, m.root == "github.com/flanksource/commons" && m.path == "logger/sanitize.go" && m.role == "definition" && m.line > 0) |
| clicky RPC definition | github.com/flanksource/clicky | github.com/flanksource/clicky/rpc.NewOpenAPICommand = | json.matches.exists(m, m.root == "github.com/flanksource/clicky" && m.path == "rpc/commands.go" && m.role == "definition" && m.line > 0) |
| commons-db test DB definition | github.com/flanksource/commons-db | github.com/flanksource/commons-db/dbtest.Open = | json.matches.exists(m, m.root == "github.com/flanksource/commons-db" && m.path == "dbtest/dbtest.go" && m.role == "definition" && m.line > 0) |
| gavel Git manager definition | github.com/flanksource/gavel | github.com/flanksource/gavel/git.NewGitRepositoryManager = | json.matches.exists(m, m.root == "github.com/flanksource/gavel" && m.path == "git/manager.go" && m.role == "definition" && m.line > 0) |
| captain runner definition | github.com/flanksource/captain | github.com/flanksource/captain/pkg/ai/agent.Runner.Run = | json.matches.exists(m, m.root == "github.com/flanksource/captain" && m.path == "pkg/ai/agent/runner.go" && m.role == "definition" && m.line > 0) |

## Relationships and selectors

| Name | root | expression | CEL Validation |
| --- | --- | --- | --- |
| clicky RPC caller | github.com/flanksource/clicky | github.com/flanksource/clicky/rpc.NewOpenAPICommand < +pkg github.com/flanksource/clicky/extensions | json.matches.exists(m, m.root == "github.com/flanksource/clicky" && m.path == "extensions/cobra.go" && m.role == "call" && m.package_path == "github.com/flanksource/clicky/extensions") |
| clicky direct call path | github.com/flanksource/clicky | extensions.CobraExtension.OpenAPICommand >> rpc.NewOpenAPICommand | json.total == 1 && size(json.path.calls) == 1 && json.path.calls[0].path == "extensions/cobra.go" |
| captain runner method | github.com/flanksource/captain | struct:github.com/flanksource/captain/pkg/ai/agent.Runner :methods func:Run | json.matches.exists(m, m.path == "pkg/ai/agent/runner.go" && m.role == "definition" && m.symbol.contains("agent.Runner:Run#")) |
| gavel typed function selector | github.com/flanksource/gavel | func:NewGitRepositoryManager +pkg:github.com/flanksource/gavel:git | json.symbols.exists(s, s.query_name == "github.com/flanksource/gavel/git.NewGitRepositoryManager" && s.kind == "func") |
| root excludes clicky symbol | github.com/flanksource/captain | func:github.com/flanksource/clicky/rpc.NewOpenAPICommand | json.total == 0 && size(json.matches) == 0 |
