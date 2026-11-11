---
cwd: ..
timeout: 5m
---

# Selective Git patch output

| Name | Command | Exit Code | CEL Validation |
| --- | --- | --- | --- |
| Original hunks, rename references, mixed-hunk rejection and temporary-index staging | go test ./symboldiff -ginkgo.focus='filtered patch output' -count=1 | 0 | exitCode == 0 |
| CLI patch bytes and conflicting formats | go test ./cmd/uir -ginkgo.focus='uir diff --patch' -count=1 | 0 | exitCode == 0 |
| Patch package lint | .bin/golangci-lint run ./symboldiff | 0 | exitCode == 0 |
| Rename API response | pnpm --dir packages/api exec vitest run test/diff.spec.ts | 0 | exitCode == 0 |
| History rename rendering | pnpm --dir web exec vitest run src/HistoryDiff.test.ts | 0 | exitCode == 0 |
