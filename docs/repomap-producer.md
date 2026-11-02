# Producer plugins and `uir import --plugin repomap`

`uir import` normally reads a publication JSON file. A producer plugin lets uir ask an external executable for the publication of a checkout instead. repomap uses this to publish dependencies, container images, Kubernetes resources and security advisories as queryable symbols (`dep.package`, `security.advisory`, `k8s.container`, ...).

```bash
uir import --plugin repomap .
uir import --plugin repomap --security ../service
uir import --plugin repomap --plugin-path ./.bin/repomap .
```

With `--plugin`, the positional argument is the checkout path (the working directory when omitted). `--security` asks the plugin to include security advisories and fails if the plugin cannot reach its advisory source. `--producer`, `--historical` and `--git-commit` apply to the returned publication as they do to a file.

The import is a command-line operation only. `POST /api/v1/modules/import` refuses `plugin` with 400, because it would execute a binary and read a checkout on the server; send the publication in the body instead.

## Discovery

The executable named after the plugin is resolved in this order, and a miss lists every location searched:

1. `--plugin-path`
2. `$UIR_PRODUCER_PATH` (a path list)
3. `./.bin`
4. the directory of the `uir` executable
5. `$PATH`

## Protocol `uir-producer/1`

uir runs `<bin> uir` and speaks newline-delimited JSON-RPC 2.0 over its stdin and stdout. The plugin's stderr is for logs; uir includes its tail in any error.

| Method | Params | Result |
| --- | --- | --- |
| `initialize` | none | `{"name":"repomap","version":"...","protocol":"uir-producer/1","methods":["initialize","publication","shutdown"]}` |
| `publication` | `{"path":"<checkout dir>","security":false}` | an `indexer.Publication` object, the format `uir import` reads |
| `shutdown` | none | `null`, after which the process exits |

uir fails the import, with upgrade guidance, when `protocol` differs or `publication` is not listed. The publication is decoded strictly (unknown fields are rejected). A JSON-RPC error object, non-JSON on stdout, or the child exiting early also fail the import; the plugin's `error.data` and stderr tail are included. Cancelling uir kills the child.

When the publication does not set `options.producer`, uir sets it to `<name>/<version>+sha256:<binary digest>`.

## Related queries

```sql
SELECT symbol.name, r.severity, r.target_version, path, line FROM security.advisory:* AS r WHERE r.severity = 'high'
SELECT path, line FROM references(traverse(dep.package:"golang.org/x/net", via => 'dep.provides'))
```

`references(traverse(...))` composes: a navigation function accepts another navigation function as its source.
