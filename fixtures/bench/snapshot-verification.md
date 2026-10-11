---
cwd: ../..
timeout: 20m
codeBlocks: [bash]
---

# Incremental snapshot definition of done

Set `SNAPSHOT_RUN` to the absolute directory of the completed 92-case replay. Every observation must have a passing fixture verdict, valid source round-trip and nonempty CPU, heap, trace and SQL artifacts. The focused integration suites independently verify publication, query round-trips, retention and rollback.

### command: Complete committed-corpus replay evidence

```bash
go run ./hack/snapshotbench verify "${SNAPSHOT_RUN:?Set SNAPSHOT_RUN}"
```

- cel: dyn(json).ok == true && dyn(json).cases == 92 && dyn(json).transitions == 20 && dyn(json).retained_source_samples > 0

### command: Historical publication and replay regression tests

```bash
go test ./indexer ./storage ./query ./symboldiff ./cmd/uir ./hack/snapshotbench -ginkgo.focus='historical|external publication|module import|snapshot replay evidence' -ginkgo.no-color
```

- cel: exitCode == 0

### command: Import and historical-location API integration

```bash
pnpm --dir packages/api test
```

- cel: exitCode == 0

### command: UIR build

```bash
make build
```

- cel: exitCode == 0

### command: UIR lint

```bash
make lint
```

- cel: exitCode == 0

### command: OIPA catalog transactions and offline command

```yaml
cwd: /Users/moshe/work/om/oma/oipa-cli
```

```bash
go test ./cmd ./plugins/oipa/ast ./plugins/oipa/uirindex ./internal/rbac ./internal/symbols -run 'TestCmd$|TestAST$|TestUIRIndex$|TestRBAC$|TestSymbols$' -ginkgo.focus='explicit source tracking roots|directory revision publication|directory catalog transactions|offline symbols|UIR SQLite output|symbol publication' -ginkgo.no-color
```

- cel: exitCode == 0

### command: OIPA producer lint

```yaml
cwd: /Users/moshe/work/om/oma/oipa-cli
```

```bash
golangci-lint run --timeout=5m ./plugins/oipa/uirindex/... ./internal/symbols/... ./internal/rbac/...
```

- cel: exitCode == 0
