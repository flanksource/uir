---
cwd: $GIT_ROOT_DIR
codeBlocks: [bash]
---

# Resolved variable types acceptance

Type facts are snapshot-specific; unavailable facts and incomplete coverage must fail before filtering. The reference Ruleguard runner uses Go 1.22 because its pinned go/packages cannot read Go 1.27 export data. UIR preserves all 11 reference findings and additionally reports both names in a grouped declaration that the reference AST pattern misses.

| Name | Command | Timeout | Exit Code | CEL Validation |
| --- | --- | --- | --- | --- |
| Type extraction, publication, validation and IO | go test -v ./query ./storage -ginkgo.focus='[Vv]ariable type' -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Lint rules | go test -v ./lint -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Ruleguard parity | go test -tags ruleguardparity -v ./lint -ginkgo.focus='Ruleguard parity' -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("1 Passed") |
| Metric IO remains document-free | go test -v ./query -ginkgo.focus='Metric query IO' -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("2 Passed") |
| API types | npm --prefix packages/api test -- --run test/sql.spec.ts | 3m | 0 | exitCode == 0 && stdout.contains("4 passed") |
| Completion | npm --prefix web test -- --run src/sql-completion.test.ts | 3m | 0 | exitCode == 0 && stdout.contains("10 passed") |
| Error findings JSON | bash hack/uir-lint-smoke.sh --dsn .tmp/request-state-acceptance.db lint --config lint/testdata/request-state/.uir.yaml --format json --no-progress | 3m | 1 | exitCode == 1 && json[0].success && json[0].violations.size() == 13 && json[0].violations.exists(v, v.line == 26 && v.column == 13) |
| Build | make build | 10m | 0 | exitCode == 0 |
| Lint | make lint | 10m | 0 | exitCode == 0 |
| Go suite | go test ./... -timeout=5m | 15m | 0 | exitCode == 0 |
| Released module boundary | GOWORK=off go test ./cmd/uir -run '^$' | 5m | 0 | exitCode == 0 |
