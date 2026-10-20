---
cwd: $GIT_ROOT_DIR
codeBlocks: [bash]
---

# SQL lint acceptance

The specifications cover strict configuration, immutable snapshots, freshness, coverage warnings, architecture witnesses and uncapped SQL rows. The sample runs as an independent module, outside the enclosing development workspace's module selection.

| Name | Command | Timeout | Exit Code | CEL Validation |
| --- | --- | --- | --- | --- |
| UIR lint specifications | go test -v ./lint ./cmd/uir -ginkgo.focus='[Ll]int' -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Uncapped SQL visitor and reference scopes | go test -v ./query -ginkgo.focus='SQL r' -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Freshness checks | go test -v ./indexer -ginkgo.focus='checking an indexed checkout' -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Coverage failure classification | go test -v ./indexer -ginkgo.focus='coverage cause classification' -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Gavel adapter and severity | go test -C ../gavel -v ./linters ./linters/uir -ginkgo.no-color -timeout=2m | 5m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("no tests to run") |
| Gavel discovery scope and baseline | go test -C ../gavel -v ./lint -run TestUIRLint -ginkgo.no-color -timeout=2m | 5m | 0 | exitCode == 0 && stdout.contains("Passed") |
| Error findings JSON | bash hack/uir-lint-smoke.sh --dsn .tmp/uir-lint-errors.db lint --config lint/testdata/project/.uir.yaml --format json --no-progress | 3m | 1 | exitCode == 1 && json.size() == 1 && json[0].success && json[0].violations.size() == 2 && json[0].violations.exists(v, v.severity == "error" && v.line == 3) |
| Warning findings JSON | bash hack/uir-lint-smoke.sh --dsn .tmp/uir-lint-warnings.db lint --config lint/testdata/project/.uir.yaml --rule small-files --format json --no-progress | 3m | 0 | exitCode == 0 && json[0].success && json[0].violations.size() == 1 && json[0].violations[0].severity == "warning" |
| Include and exclude findings | bash hack/uir-lint-smoke.sh --dsn .tmp/uir-lint-filters.db lint --config lint/testdata/project/.uir.yaml --include '**/*.go' --exclude 'sample.go' --format json --no-progress | 3m | 0 | exitCode == 0 && json[0].success && json[0].violations.size() == 0 |
| Build | make build | 10m | 0 | exitCode == 0 |
| Lint | make lint | 10m | 0 | exitCode == 0 |
| Released module boundary | GOWORK=off go test ./cmd/uir -run '^$' | 5m | 0 | exitCode == 0 |
