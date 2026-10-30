---
cwd: $GIT_ROOT_DIR
codeBlocks: [bash]
---

# Queryable metrics acceptance

The focused specifications verify physical line and parameter semantics, source/document persistence, immutable snapshot selection, missing-metric errors, projection I/O, API witnesses and completion. Run from the repository root with the normal development toolchain.

| Name | Command | Timeout | CEL Validation |
| --- | --- | --- | --- |
| Metric specifications | go test -v ./indexer/syntax ./storage ./indexer ./query ./query/sqlsyntax ./cmd/uir -ginkgo.focus='[Mm]etric' -ginkgo.no-color -timeout=5m | 10m | exitCode == 0 && stdout.contains("Passed") |
| Full Go suite | go test ./... -timeout=10m | 15m | exitCode == 0 |
| Browser unit tests | pnpm --dir web run test | 5m | exitCode == 0 && stdout.contains("passed") |
| API integration tests | pnpm --dir packages/api run test | 5m | exitCode == 0 && stdout.contains("passed") |
| Build CLI and embedded browser | make build | 10m | exitCode == 0 |
| Go and browser lint | make lint | 10m | exitCode == 0 && stdout.contains("0 issues.") |
