---
cwd: $GIT_ROOT_DIR
codeBlocks: [bash]
---

# Package import budget lint acceptance

`package_imports()` charges each package for the closure of its own direct third-party imports. The sample workspace in `query/testdata/imports` has module `a` importing local-replace modules `b` and `c`: `p1` imports in-module `p2` and `b/bpkg`, whose closure is four packages, while `p2` carries `c/cother` itself and so is not charged to `p1`. The lint run indexes `a` as an independent module, outside the enclosing development workspace's module selection.

| Name | Command | Timeout | Exit Code | CEL Validation |
| --- | --- | --- | --- | --- |
| Exact per-package counts | go test -v ./query -ginkgo.focus='package_imports' -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Documented SQL examples | go test -v ./query -ginkgo.focus='SQL documentation examples' -ginkgo.no-color -timeout=5m | 10m | 0 | exitCode == 0 && stdout.contains("Passed") && !stdout.contains("Ran 0") |
| Budget findings JSON | bash hack/uir-lint-smoke.sh --dsn .tmp/uir-lint-imports.db lint --config query/testdata/imports/a/.uir.yaml --format json --no-progress | 3m | 1 | exitCode == 1 && json.size() == 1 && json[0].violations.size() == 3 && json[0].violations.exists(v, v.rule.method == "dep-budget-package" && v.severity == "error" && v.file.endsWith("/a/p1/p1.go") && v.message.contains("imports 4 third-party packages") && v.message.contains("largest: example.org/imports/b/bpkg (4)")) && json[0].violations.exists(v, v.rule.method == "dep-budget-package" && v.file.endsWith("/a/p4/doc.go") && v.message.contains("example.org/imports/b/bpkg (4), example.org/imports/c/cpkg (2)")) && json[0].violations.exists(v, v.rule.method == "dep-budget-package-warning" && v.severity == "warning" && v.file.endsWith("/a/p2/p2.go")) |
| Package without third-party imports is not reported | bash hack/uir-lint-smoke.sh --dsn .tmp/uir-lint-imports.db lint --config query/testdata/imports/a/.uir.yaml --format json --no-progress | 3m | 1 | exitCode == 1 && !json[0].violations.exists(v, v.file.endsWith("/a/p3/p3.go")) && !json[0].violations.exists(v, v.file.endsWith("/a/a.go")) |
| Build | make build | 10m | 0 | exitCode == 0 |
| Lint | make lint | 10m | 0 | exitCode == 0 |
