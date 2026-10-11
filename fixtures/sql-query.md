---
cwd: ..
codeBlocks: [bash]
timeout: 15m
---

# SQL navigation with Participle

Run from the repository root with Go, pnpm, jq, and the normal development toolchain on PATH. The focused tests cover grammar, predicates, snapshot properties, native postings, dispatch, cycles, graph omissions, and API envelopes. The full repository gates also run; the CLI example uses an isolated temporary database.

| Name | Command | CEL Validation |
| --- | --- | --- |
| Build CLI and embedded browser | make build | exitCode == 0 |
| Full Go suite | go test ./... -timeout=10m | exitCode == 0 |
| SQL syntax and execution | go test -v ./query/sqlsyntax ./query ./cmd/uir -ginkgo.focus='SQL ' -ginkgo.no-color -timeout=2m | exitCode == 0 && stdout.contains("TestSQLSyntax") && stdout.contains("Passed") |
| Graph regressions | go test ./graph -count=1 | exitCode == 0 |
| Browser unit tests | pnpm --dir web run test | exitCode == 0 && stdout.contains("passed") |
| API integration tests | pnpm --dir packages/api run test | exitCode == 0 && stdout.contains("passed") |
| Go and browser lint | make lint | exitCode == 0 && stdout.contains("0 issues.") |

### command: Import and query explicit domain links

```yaml
timeout: 120
```

```bash
set -euo pipefail
mkdir -p .tmp
SQL_FIXTURE_DIR=$(mktemp -d .tmp/sql-query.XXXXXX)
trap 'rm -rf "$SQL_FIXTURE_DIR"' EXIT
go run ./hack/sql-domains > "$SQL_FIXTURE_DIR/publication.json"
go run ./cmd/uir --dsn "$SQL_FIXTURE_DIR/index.db" import "$SQL_FIXTURE_DIR/publication.json" > "$SQL_FIXTURE_DIR/import.json"
go run ./cmd/uir --dsn "$SQL_FIXTURE_DIR/index.db" query "SELECT r.method, r.path, r.description FROM http.route:* AS r WHERE r.path IMATCH '/orders/**' AND r.description IS NULL" --format json | jq -e '.total == 1 and .rows[0]["r.method"] == "POST" and .rows[0]["r.description"] == null' > /dev/null
go run ./cmd/uir --dsn "$SQL_FIXTURE_DIR/index.db" query "SELECT * FROM path(http.route:Orders, messaging.consumer:Consume, via => 'http.handled_by,messaging.publishes_to,messaging.delivers_to')" --format json | jq -e '.total == 1 and (.path.symbols | length) == 4 and (.path.calls | length) == 3' > /dev/null
go run ./cmd/uir --dsn "$SQL_FIXTURE_DIR/index.db" query "SELECT * FROM graph(k8s.service:checkout, via => 'k8s.selects,k8s.uses_config', direction => 'outgoing')" --format json
```

- cel: exitCode == 0 && size(json.graph.nodes) == 2 && size(json.graph.edges) == 2 && json.graph.edges[0].kind != json.graph.edges[1].kind && json.graph.edges.all(e, size(e.sites) == 1 && e.sites[0].resolution == "static" && e.sites[0].location.snapshot_id != "")
