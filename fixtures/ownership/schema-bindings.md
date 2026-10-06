---
cwd: /Users/moshe/go/src/github.com/flanksource/uir-schema-bindings
---

# Canonical schema and generator-input checkpoint

| Name | Command | Exit Code | CEL Validation |
|---|---|---|---|
| Worktree identity | pwd | 0 | stdout.contains('/flanksource/uir-schema-bindings') |
| Taskfile entrypoints | task --list | 0 | stdout.contains('bindings:typescript:check') && stdout.contains('schema:check') |
| Schema contract, projection and artifact drift | task schema:check | 0 | exitCode == 0 |
| Existing Go and Python model wire tests | go test . | 0 | exitCode == 0 |
| Owner package lint | task lint:schema | 0 | exitCode == 0 |

This checkpoint verifies the schema foundation. Full phase 3 acceptance additionally requires generated SDK import/compile and cross-language round-trip conformance, plus the host/plugin JSON cutover.
