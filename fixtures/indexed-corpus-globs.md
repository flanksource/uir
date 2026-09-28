---
cwd: ..
# WORKAROUND(missing-dotenv): gavel ignores a missing setup.dotenv file, so each command checks UIR_DSN and exits 78 rather than falling back to ~/.config/uir/uir.db.
# Correct fix: commons-db shell.loadDotEnv (shell/environment.go) fails on a declared dotenv that does not exist; then drop the guards and exec .bin/uir directly.
# Ref: discussed with user 2026-09-27
exec: bash
args: ["-c", "test -n \"$UIR_DSN\" || { echo 'UIR_DSN is unset: run make fixture-corpus to write .tmp/uir-corpus.env' >&2; exit 78; }; exec .bin/uir \"$@\"", "uir", "--format", "json", "query", "{{.kind}}:{{.module}}{{.suffix}}", "--root", "{{.root}}"]
timeout: 30s
suffix: ""
setup:
  dotenv: [../.tmp/uir-corpus.env]
---

# Typed selector globs over the indexed corpus

`root` selects the indexed module; `module` and `suffix` form the selector value. Run `make fixture-corpus` first. Each row names a symbol that the pattern must include.

## Module selectors

| Name | root | module | kind | suffix | witness | CEL Validation |
| --- | --- | --- | --- | --- | --- | --- |
| exact module | github.com/flanksource/clicky | github.com/flanksource/clicky | mod | | github.com/flanksource/clicky/rpc.NewOpenAPICommand | json.symbols.exists(s, s.query_name == witness) |
| single star module | github.com/flanksource/clicky | github.com/flanksource/click | mod | * | github.com/flanksource/clicky/rpc.NewOpenAPICommand | json.symbols.exists(s, s.query_name == witness) |
| double star module | github.com/flanksource/captain | github.com | mod | /**/captain | github.com/flanksource/captain/pkg/ai/agent.Runner.Run | json.symbols.exists(s, s.query_name == witness) |
| recursive module suffix | github.com/flanksource/gavel | github.com/flanksource | mod | /... | github.com/flanksource/gavel/git.NewGitRepositoryManager | json.symbols.exists(s, s.query_name == witness) |
| recursive module suffix includes root | github.com/flanksource/captain | github.com/flanksource/captain | mod | /... | github.com/flanksource/captain/pkg/ai/agent.Runner.Run | json.symbols.exists(s, s.query_name == witness) |

## Package selectors

| Name | root | module | kind | suffix | witness | CEL Validation |
| --- | --- | --- | --- | --- | --- | --- |
| single star package | github.com/flanksource/clicky | github.com/flanksource/clicky | pkg | /r* | github.com/flanksource/clicky/rpc.NewOpenAPICommand | json.symbols.exists(s, s.query_name == witness) |
| double star package | github.com/flanksource/captain | github.com/flanksource/captain | pkg | /**/agent | github.com/flanksource/captain/pkg/ai/agent.Runner.Run | json.symbols.exists(s, s.query_name == witness) |
| recursive package suffix | github.com/flanksource/captain | github.com/flanksource/captain | pkg | /... | github.com/flanksource/captain/pkg/ai/agent.Runner.Run | json.symbols.exists(s, s.query_name == witness) |
| recursive package suffix includes root | github.com/flanksource/clicky | github.com/flanksource/clicky | pkg | /rpc/... | github.com/flanksource/clicky/rpc.NewOpenAPICommand | json.symbols.exists(s, s.query_name == witness) |
| exact relative package | github.com/flanksource/captain | github.com/flanksource/captain | pkg | :pkg/ai/agent | github.com/flanksource/captain/pkg/ai/agent.Runner.Run | json.symbols.exists(s, s.query_name == witness) |
| double star relative package | github.com/flanksource/captain | github.com/flanksource/captain | pkg | :pkg/**/agent | github.com/flanksource/captain/pkg/ai/agent.Runner.Run | json.symbols.exists(s, s.query_name == witness) |
| recursive relative package suffix | github.com/flanksource/captain | github.com/flanksource/captain | pkg | :pkg/... | github.com/flanksource/captain/pkg/ai/agent.Runner.Run | json.symbols.exists(s, s.query_name == witness) |
| recursive module half of package | github.com/flanksource/captain | github.com/flanksource | pkg | /...:pkg/ai/agent | github.com/flanksource/captain/pkg/ai/agent.Runner.Run | json.symbols.exists(s, s.query_name == witness) |

## Function selectors

| Name | root | module | kind | suffix | witness | CEL Validation |
| --- | --- | --- | --- | --- | --- | --- |
| single star function | github.com/flanksource/clicky | github.com/flanksource/clicky | func | /rpc.NewOpenAPI* | github.com/flanksource/clicky/rpc.NewOpenAPICommand | json.symbols.exists(s, s.query_name == witness) |
| single character function glob | github.com/flanksource/clicky | github.com/flanksource/clicky | func | /rpc.NewOpenAPIComman? | github.com/flanksource/clicky/rpc.NewOpenAPICommand | json.symbols.exists(s, s.query_name == witness) |
| double star function | github.com/flanksource/captain | github.com/flanksource/captain | func | /**/agent.Runner.Run | github.com/flanksource/captain/pkg/ai/agent.Runner.Run | json.symbols.exists(s, s.query_name == witness) |
| recursive function suffix | github.com/flanksource/captain | github.com/flanksource/captain | func | /... | github.com/flanksource/captain/pkg/ai/agent.Runner.Run | json.symbols.exists(s, s.query_name == witness) |

## Struct selectors

| Name | root | module | kind | suffix | witness | CEL Validation |
| --- | --- | --- | --- | --- | --- | --- |
| single star struct | github.com/flanksource/captain | github.com/flanksource/captain | struct | /pkg/ai/agent.Run* | github.com/flanksource/captain/pkg/ai/agent.Runner | json.symbols.exists(s, s.query_name == witness) |
| double star struct | github.com/flanksource/captain | github.com/flanksource/captain | struct | /**/agent.Runner | github.com/flanksource/captain/pkg/ai/agent.Runner | json.symbols.exists(s, s.query_name == witness) |
| recursive struct suffix | github.com/flanksource/captain | github.com/flanksource/captain | struct | /... | github.com/flanksource/captain/pkg/ai/agent.Runner | json.symbols.exists(s, s.query_name == witness) |

## Field selectors

| Name | root | module | kind | suffix | witness | CEL Validation |
| --- | --- | --- | --- | --- | --- | --- |
| single star field | github.com/flanksource/captain | github.com/flanksource/captain | field | /pkg/ai/agent.Runner.Requ* | github.com/flanksource/captain/pkg/ai/agent.Runner.Request | json.symbols.exists(s, s.query_name == witness) |
| double star field | github.com/flanksource/captain | github.com/flanksource/captain | field | /**/agent.Runner.Request | github.com/flanksource/captain/pkg/ai/agent.Runner.Request | json.symbols.exists(s, s.query_name == witness) |
| recursive field suffix | github.com/flanksource/captain | github.com/flanksource/captain | field | /... | github.com/flanksource/captain/pkg/ai/agent.Runner.Request | json.symbols.exists(s, s.query_name == witness) |

## Escaped selector character

| Name | root | module | kind | suffix | CEL Validation |
| --- | --- | --- | --- | --- | --- |
| escaped star is literal | github.com/flanksource/clicky | github.com/flanksource/clicky | func | /rpc.NewOpenAPICommand\* | json.total == 0 && size(json.symbols) == 0 |
