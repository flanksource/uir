---
cwd: ..
exec: .bin/uir
args: ["--dsn", ".tmp/uir-corpus.db", "--format", "json", "query", "{{.expression}}", "--root", "{{.root}}"]
timeout: 30s
---

# Compact PEG grammar over indexed source

Run `make fixture-corpus` first. The assertions use stable symbol identities and source paths from the five indexed sibling checkouts. Together with the scoped, cross-root, and glob fixtures, these cases exercise every selector kind, relation, filter, set operator, grouping, path operator, and modifier in `query/grammar.peg`.

## Symbols, relations, and filters

| Name | root | expression | CEL Validation |
| --- | --- | --- | --- |
| bare Go symbol | github.com/flanksource/clicky | rpc.NewOpenAPICommand | json.symbols.exists(s, s.query_name == "github.com/flanksource/clicky/rpc.NewOpenAPICommand") |
| direct package children | github.com/flanksource/clicky | github.com/flanksource/clicky/rpc.* | json.symbols.exists(s, s.query_name == "github.com/flanksource/clicky/rpc.NewOpenAPICommand") |
| unary outgoing relation | github.com/flanksource/clicky | extensions.CobraExtension.OpenAPICommand > | json.matches.exists(m, m.path == "extensions/cobra.go" && m.role == "call" && m.symbol.contains("NewOpenAPICommand")) |
| outgoing binary relation | github.com/flanksource/clicky | extensions.CobraExtension.OpenAPICommand > func:github.com/flanksource/clicky/rpc.NewOpenAPICommand | json.matches.exists(m, m.path == "extensions/cobra.go" && m.role == "call") |
| incoming binary relation | github.com/flanksource/clicky | rpc.NewOpenAPICommand < func:github.com/flanksource/clicky/extensions.CobraExtension.OpenAPICommand | json.matches.exists(m, m.path == "extensions/cobra.go" && m.role == "call") |
| bounded transitive callers | github.com/flanksource/clicky | rpc.NewOpenAPICommand <<2 | json.matches.exists(m, m.path == "extensions/cobra.go" && m.role == "definition" && m.depth <= 2) |
| default transitive depth | github.com/flanksource/clicky | rpc.NewOpenAPICommand << | json.matches.exists(m, m.path == "extensions/cobra.go" && m.depth <= 3) |
| interface implementers | github.com/flanksource/captain | agent.WorkspaceIsolator :impl struct:github.com/flanksource/captain/pkg/ai/agent/worktree.Plugin | json.matches.exists(m, m.path == "pkg/ai/agent/worktree/worktree.go" && m.kind == "implementation" && m.symbol.contains("Plugin")) |
| owned method | github.com/flanksource/captain | struct:github.com/flanksource/captain/pkg/ai/agent.Runner :methods func:Run | json.matches.exists(m, m.path == "pkg/ai/agent/runner.go" && m.symbol.contains("agent.Runner:Run#")) |
| field writes | github.com/flanksource/gavel | git.DefaultGitRepositoryManager.cacheDir ~w | json.matches.exists(m, m.path == "git/manager.go" && m.role == "write") |
| incoming write filter | github.com/flanksource/gavel | git.DefaultGitRepositoryManager.cacheDir < ~w | json.matches.exists(m, m.path == "git/manager.go" && m.role == "write") |
| file suffix filter | github.com/flanksource/clicky | rpc.NewOpenAPICommand < -f _test.go | json.matches.exists(m, m.path == "extensions/cobra.go" && m.role == "call") && json.matches.all(m, !m.path.endsWith("_test.go")) |
| package include filter | github.com/flanksource/clicky | rpc.NewOpenAPICommand < +pkg github.com/flanksource/clicky/extensions | json.matches.exists(m, m.package_path == "github.com/flanksource/clicky/extensions" && m.path == "extensions/cobra.go") && json.matches.all(m, m.package_path == "github.com/flanksource/clicky/extensions") |
| package recursive include filter | github.com/flanksource/clicky | rpc.NewOpenAPICommand < +pkg github.com/flanksource/clicky/... | json.matches.exists(m, m.path == "extensions/cobra.go" && m.role == "call") |
| package exclude filter | github.com/flanksource/clicky | rpc.NewOpenAPICommand < -pkg github.com/flanksource/clicky/rpc | json.matches.exists(m, m.path == "extensions/cobra.go" && m.role == "call") && json.matches.all(m, m.package_path != "github.com/flanksource/clicky/rpc") |
| combined location filters | github.com/flanksource/clicky | rpc.NewOpenAPICommand < -f _test.go +pkg github.com/flanksource/clicky/extensions | json.matches.exists(m, m.path == "extensions/cobra.go") && json.matches.all(m, m.package_path == "github.com/flanksource/clicky/extensions" && !m.path.endsWith("_test.go")) |
| modified binary right operand | github.com/flanksource/clicky | rpc.NewOpenAPICommand < func:OpenAPICommand +pkg:github.com/flanksource/clicky/extensions | json.matches.exists(m, m.path == "extensions/cobra.go" && m.role == "call") |
| grouped binary right operand | github.com/flanksource/clicky | rpc.NewOpenAPICommand < (func:OpenAPICommand & pkg:github.com/flanksource/clicky/extensions) | json.matches.exists(m, m.path == "extensions/cobra.go" && m.role == "call") |
| chained relations | github.com/flanksource/clicky | rpc.NewOpenAPICommand < > func:NewOpenAPICommand | json.matches.exists(m, m.path == "extensions/cobra.go" && m.role == "call") |

## Sets, modifiers, and call paths

| Name | root | expression | CEL Validation |
| --- | --- | --- | --- |
| set intersection | github.com/flanksource/clicky | pkg:github.com/flanksource/clicky/rpc & func:NewOpenAPICommand | json.symbols.exists(s, s.query_name == "github.com/flanksource/clicky/rpc.NewOpenAPICommand") && json.symbols.all(s, s.package_path == "github.com/flanksource/clicky/rpc") |
| include and exclude modifiers | github.com/flanksource/clicky | func:NewOpenAPI* +pkg:github.com/flanksource/clicky/rpc -func:*WithConfig | json.symbols.exists(s, s.query_name == "github.com/flanksource/clicky/rpc.NewOpenAPICommand") && json.symbols.all(s, !s.query_name.endsWith("WithConfig")) |
| modified unary relation | github.com/flanksource/clicky | (rpc.NewOpenAPICommand <) +pkg:github.com/flanksource/clicky/extensions | json.matches.exists(m, m.path == "extensions/cobra.go" && m.enclosing_key.contains("CobraExtension")) && json.matches.all(m, m.package_path == "github.com/flanksource/clicky/extensions") |
| bounded direct path | github.com/flanksource/clicky | extensions.CobraExtension.OpenAPICommand >>2 rpc.NewOpenAPICommand | json.total == 1 && size(json.path.calls) == 1 && json.path.calls[0].path == "extensions/cobra.go" |
