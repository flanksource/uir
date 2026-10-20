---
cwd: $GIT_ROOT_DIR
codeBlocks: [bash]
---

# Hack build-tag acceptance

These checks prove exact Boolean semantics, immutable source recovery, SQLite/PostgreSQL membership, cache behavior, unchanged metric IO, lint witnesses, API serialization, completion and the runnable CLI rule. Build-excluded sources remain snapshot members; no storage migration or reindex is introduced. The fixture module intentionally contains eight warning-level violations.

| Name | Command | Timeout | Exit Code | CEL Validation |
| --- | --- | --- | --- | --- |
| Exact Go build constraints | go test -v ./internal/buildconstraint -count=1 -ginkgo.no-color | 3m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| SQL grammar | go test -v ./query/sqlsyntax -count=1 -ginkgo.no-color | 3m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Snapshot build constraints | go test -v ./query -ginkgo.focus='[Bb]uild constraint' -ginkgo.no-color -count=1 -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Unchanged metric IO | go test -v ./query -ginkgo.focus='Metric query IO' -ginkgo.no-color -count=1 -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Hack rule and CLI witnesses | go test -v ./lint ./cmd/uir -ginkgo.focus='[Bb]uild.tag' -ginkgo.no-color -count=1 -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Query completion | pnpm --dir web exec vitest run src/sql-completion.test.ts | 3m | 0 | exitCode == 0 && stdout.contains("11 passed") |
| SQL API contract | pnpm --dir packages/api exec vitest run test/sql.spec.ts | 3m | 0 | exitCode == 0 && stdout.contains("5 passed") |
| Eight CLI warning findings | bash hack/uir-lint-smoke.sh --dsn .tmp/hack-build-acceptance.db lint --config lint/testdata/hack-build-tags/.uir.yaml --rule hack-build-tag --format json --no-progress | 5m | 0 | exitCode == 0 && json.size() == 1 && json[0].success && json[0].violations.size() == 8 && json[0].violations.all(v, v.severity == "warning") && json[0].violations.exists(v, v.file.endsWith("hack/platform_windows.go")) && json[0].violations.exists(v, v.file.endsWith("tools/hack/nested.go")) |
| Build | make build | 10m | 0 | exitCode == 0 |
| Lint | make lint | 10m | 0 | exitCode == 0 |
| Go suite | go test ./... | 15m | 0 | exitCode == 0 |
