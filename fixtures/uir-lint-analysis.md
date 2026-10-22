---
cwd: $GIT_ROOT_DIR
codeBlocks: [bash]
---

# Module-scoped lint analysis acceptance

Run `make build` first to build the CLI. These checks cover syntax-only extraction, current-module typed facts, explicit multi-module traversal, reuse, isolated analysis heads, SQLite/PostgreSQL storage, Git sources, grouping, SQL requirements and the public API response shape. The CLI smoke uses its own database and disables module downloads.

| Name | Command | Timeout | Exit Code | CEL Validation |
| --- | --- | --- | --- | --- |
| Analysis extraction and metric parity | go test -v ./indexer ./indexer/syntax -ginkgo.focus='Project analysis' -ginkgo.focus='Syntax analysis build selection' -ginkgo.focus='[Mm]etrics' -ginkgo.focus='module index run memo' -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Analysis migration cutover | go test -v ./storage -ginkgo.focus='two-layer fingerprint cutover' -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Lint scope and source modes | go test -v ./lint ./cmd/uir -ginkgo.focus='[Ll]int' -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| SQL planning and witnesses | go test -v ./query -run TestQuery -ginkgo.focus='SQL analysis requirements' -ginkgo.focus='SQL row visitor' -ginkgo.focus='SQL syntax analysis' -ginkgo.focus='SQL metrics' -ginkgo.focus='the call graph of one file' -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| API analysis response | pnpm --dir packages/api test test/sql.spec.ts | 2m | 0 | exitCode == 0 && stdout.contains("test/sql.spec.ts") && stdout.contains("passed") |
| Syntax CLI without downloads | GOWORK=off GOPROXY=off .bin/uir --dsn .tmp/uir-lint-analysis-smoke.db lint --config lint/testdata/project/.uir.yaml --rule small-files --format json --no-progress | 2m | 0 | exitCode == 0 && json[0].success && json[0].violations.size() == 1 && json[0].violations[0].severity == "warning" |
