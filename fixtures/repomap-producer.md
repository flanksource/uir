---
cwd: ..
codeBlocks: [bash]
build: bash fixtures/repomap-producer/setup.sh
timeout: 15m
exec: bash
args: ["-c", ".bin/uir --dsn .tmp/repomap-producer/index.db query \"{{.sql}}\" --location .tmp/repomap-producer/shop --format json"]
---

# repomap producer plugin

`uir import --plugin repomap <checkout>` finds the `repomap` binary, runs `repomap uir` as a stdio JSON-RPC producer (`uir-producer/1`), and publishes what it returns as its own `repomap:<checkout>` root. Its dependencies, GitHub Actions, container images and Kubernetes resources then become UIR SQL symbols. Each symbol is positioned at the manifest line that declares it, so `path`, `line` and `column` are real declaration sites.

Run from the uir repository root, with a sibling `../repomap` checkout (override with `REPOMAP_DIR`) and Go, make, git and jq on PATH. The build step ([`setup.sh`](repomap-producer/setup.sh)) builds both CLIs and copies the [`shop`](repomap-producer/shop) demo into a clean git checkout under `.tmp/repomap-producer`. It then imports the demo into a fresh SQLite index. Each table row is one UIR SQL query, run against that checkout with `uir query --location .tmp/repomap-producer/shop --format json`.

The demo declares these:

| File | Declares |
| --- | --- |
| `go.mod` | `github.com/google/uuid` (line 6), `golang.org/x/net` (line 7), indirect `golang.org/x/text` (line 10) |
| `web/package.json` | `left-pad` (line 5), dev dependency `vitest` (line 8) |
| `.github/workflows/ci.yml` | `actions/checkout@v4` (line 7), `actions/setup-go@v5` (line 8) |
| `deploy/shop.yaml` | Deployment `store/shop` (line 1) with containers `api` → `ghcr.io/example/shop-api:1.2.3` (line 11) and `proxy` → `nginx:1.27.0` (line 13) |

## Import

### command: Import publishes every manifest

```bash
jq . .tmp/repomap-producer/import.json
```

- cel: exitCode == 0 && json.root_key.startsWith("repomap:") && json.files == 4 && json.parsed_files == 4

## Dependencies at their declaration lines

`dep.package` carries `manager`, `version`, `scope`, `direct`, `dev` and `project` properties, plus an `open_advisories` metric. Package names contain `/`, so select them with `**`.

| Name | sql | CEL Validation |
| --- | --- | --- |
| Go and npm packages with declaration sites | SELECT symbol.name, d.manager, d.version, d.direct, d.dev, path, line FROM dep.package:** AS d ORDER BY path, line | exitCode == 0 && json.total == 5 && json.rows[0]["symbol.name"] == "github.com/google/uuid" && json.rows[0]["d.version"] == "v1.6.0" && json.rows[0].path == "go.mod" && json.rows[0].line == 6 && json.rows[3]["symbol.name"] == "left-pad" && json.rows[3].path == "web/package.json" && json.rows[3].line == 5 |
| Indirect requires are located but not direct | SELECT symbol.name, line FROM dep.package:** AS d WHERE d.direct = FALSE | exitCode == 0 && json.total == 1 && json.rows[0]["symbol.name"] == "golang.org/x/text" && json.rows[0].line == 10 |
| Dev dependencies filter by property | SELECT symbol.name, d.version FROM dep.package:** AS d WHERE d.dev = TRUE | exitCode == 0 && json.total == 1 && json.rows[0]["symbol.name"] == "vitest" && json.rows[0]["d.version"] == "1.6.0" |
| Open advisories is a queryable metric | SELECT symbol.name, symbol.metrics.open_advisories FROM dep.package:** WHERE symbol.metrics.open_advisories = 0 | exitCode == 0 && json.total == 5 |
| Actions point at the uses reference | SELECT symbol.name, path, line, column FROM dep.action:** ORDER BY line | exitCode == 0 && json.total == 2 && json.rows[0]["symbol.name"] == "actions/checkout" && json.rows[0].path == ".github/workflows/ci.yml" && json.rows[0].line == 7 && json.rows[0].column == 15 |
| Images point at the image value | SELECT symbol.name, i.version, path, line, column FROM dep.image:** AS i ORDER BY line | exitCode == 0 && json.total == 2 && json.rows[1]["symbol.name"] == "nginx" && json.rows[1]["i.version"] == "1.27.0" && json.rows[1].line == 13 && json.rows[1].column == 18 |

## Kubernetes resources and images

`k8s.resource` spans its whole YAML document. `k8s.contains` links each resource to its containers, and `k8s.uses_image` links each container to its image dependency.

| Name | sql | CEL Validation |
| --- | --- | --- |
| Resources carry their identity | SELECT symbol.name, r.kind, r.namespace, path, line FROM k8s.resource:** AS r | exitCode == 0 && json.total == 1 && json.rows[0]["symbol.name"] == "shop" && json.rows[0]["r.kind"] == "Deployment" && json.rows[0]["r.namespace"] == "store" && json.rows[0].line == 1 |
| Resource contains its containers | SELECT symbol.name FROM outgoing(k8s.resource:shop, via => 'k8s.contains') ORDER BY symbol.name | exitCode == 0 && json.total == 2 && json.rows[0]["symbol.name"] == "api" && json.rows[1]["symbol.name"] == "proxy" |
| Container uses its image | SELECT symbol.name, path, line FROM outgoing(k8s.container:api, via => 'k8s.uses_image') | exitCode == 0 && json.total == 1 && json.rows[0]["symbol.name"] == "ghcr.io/example/shop-api" && json.rows[0].line == 11 |
| Image finds the containers running it | SELECT symbol.name, path, line FROM incoming(dep.image:nginx, via => 'k8s.uses_image') | exitCode == 0 && json.total == 1 && json.rows[0]["symbol.name"] == "proxy" && json.rows[0].path == "deploy/shop.yaml" && json.rows[0].line == 13 |

## Go module references

Each located Go dependency also publishes a `dep.provides` link to its module path. uir does not yet resolve it to the module's package symbols (tracked as gavel todo `ffde4e5d`), so it reads as an unresolved native relationship.

| Name | sql | CEL Validation |
| --- | --- | --- |
| Module reference stays unresolved | SELECT relationship.kind, relationship.resolution FROM outgoing(dep.package:\"golang.org/x/net\", via => 'dep.provides') | exitCode == 0 && json.total == 1 && json.rows[0]["relationship.kind"] == "dep.provides" && json.rows[0]["relationship.resolution"] == "unresolved" |

## Plugin protocol and failures

### command: Plugin handshake over stdio JSON-RPC

```bash
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize"}' '{"jsonrpc":"2.0","id":2,"method":"shutdown"}' | "${REPOMAP_DIR:-../repomap}/.bin/repomap" uir 2>/dev/null | head -1
```

- cel: exitCode == 0 && json.result.name == "repomap" && json.result.protocol == "uir-producer/1" && "publication" in json.result.methods

### command: Unknown method is a JSON-RPC error and the session continues

```bash
printf '%s\n' '{"jsonrpc":"2.0","id":7,"method":"nope"}' '{"jsonrpc":"2.0","id":8,"method":"shutdown"}' | "${REPOMAP_DIR:-../repomap}/.bin/repomap" uir 2>/dev/null | jq -s '{error: .[0].error.code, shutdown: .[1].id}'
```

- cel: exitCode == 0 && json.error == -32601.0 && json.shutdown == 8.0

### command: Re-importing an unchanged checkout publishes nothing

```bash
.bin/uir --dsn .tmp/repomap-producer/index.db import --plugin repomap --plugin-path "${REPOMAP_DIR:-../repomap}/.bin/repomap" .tmp/repomap-producer/shop --format json
```

- cel: exitCode == 0 && json.unchanged == true

### command: A missing plugin reports where it searched

```yaml
exitCode: 1
```

```bash
.bin/uir --dsn .tmp/repomap-producer/missing.db import --plugin nosuchplugin .tmp/repomap-producer/shop
```

- cel: exitCode == 1 && stderr.contains("producer \"nosuchplugin\" not found") && stderr.contains("UIR_PRODUCER_PATH")
