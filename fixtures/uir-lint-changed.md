---
cwd: $GIT_ROOT_DIR
---

# Git-scoped lint acceptance

| Name | Command | Exit Code | CEL Validation |
|------|---------|-----------|----------------|
| Immutable Git source capture | go test github.com/flanksource/commons-db/git -count=1 -ginkgo.focus='Prepared sources' -ginkgo.fail-on-empty -ginkgo.no-color | 0 | stdout.contains("ok") |
| Git-scoped lint and CLI | go test ./lint ./cmd/uir -count=1 -ginkgo.focus='Git-scoped lint' -ginkgo.fail-on-empty -ginkgo.no-color | 0 | stdout.contains("ok") |
| Pinned source indexing | go test ./indexer -count=1 -ginkgo.focus='Pinned source indexing' -ginkgo.fail-on-empty -ginkgo.no-color | 0 | stdout.contains("ok") |
