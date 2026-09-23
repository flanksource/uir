# UIR query language

`uir project query` runs structured symbol and call queries against the relational UIR store. The command is generated from the Clicky `project` entity and uses the PEG grammar in `query/grammar.peg`; the parser feeds a resolution pipeline rather than translating user input directly into SQL.

## Command

Run against SQLite with either a plain `.db` path or a `sqlite://` URL:

```sh
go run ./cmd/uir --dsn ./state/uir.db project query billing --expression 'nodes where type = "InvoiceService" and method = "Approve"'
```

Run against PostgreSQL, optionally selecting a schema:

```sh
go run ./cmd/uir --dsn 'postgres://localhost/uir?sslmode=disable' --schema analysis project query billing --expression 'callers of node where root = "app" and symbol_key = "method:billing.invoices.InvoiceService:Approve"' --format json
```

The action returns Clicky table rows with the operation, owning root, node kind, symbol components, language, and primary source location. Clicky's shared format flags select pretty tables, JSON, NDJSON, YAML, CSV, Markdown, HTML, or file sinks. The reusable Go pipeline still returns the full `query.Result`, including resolution stages and an optional graph target.

| Flag | Meaning |
| --- | --- |
| `--dsn` | Required PostgreSQL DSN, `sqlite://` URL, or plain `.db` path. |
| `--schema` | PostgreSQL schema. SQLite rejects schema-scoped initialization. |
| positional `project` | Project key. Without `--snapshot`, its published head is queried. |
| `--expression` | Required quoted PEG query expression. |
| `--snapshot` | Explicit snapshot UUID. When combined with `--project`, the snapshot must belong to that project. |
| `--root` | Default root key when the expression has no `root` predicate. |
| `--limit` | Maximum result count, from 1 through 1000. The default is 100. |
| `--format`, `--json`, `--yaml`, `--csv`, `--markdown`, `--html` | Shared Clicky output selection. |

The positional project key always supplies the logical scope. Passing `--snapshot` selects a retained snapshot and validates that it belongs to that project; otherwise the published head is used. The expression is one flag value and therefore normally needs quotes. The command opens the database through `storage.UirDB`, so it also applies the embedded additive schema migrations before querying.

## Grammar

Keywords and field names are lowercase. The supported commands are:

```text
nodes [where predicate [and predicate ...]]
callers of node where predicate [and predicate ...]
callees of node where predicate [and predicate ...]
unresolved calls [where root = "value"]
```

A predicate is an exact comparison with a double-quoted string:

```text
field = "value"
```

Supported node fields are `node_type`, `module`, `package`, `type`, `method`, `field`, `signature`, `language`, `symbol_key`, `identity_key`, and `root`. The `type` field maps to `uir_nodes.type_name`; `root` resolves `uir_roots.root_key` inside the selected snapshot. An unresolved-call expression accepts only the `root` predicate because its target is a locator rather than a resolved node. Escapes such as `\"`, `\\`, `\n`, and Unicode escapes follow Go quoted-string rules.

Examples:

```text
nodes
nodes where language = "go" and node_type = "method"
nodes where root = "vendor" and package = "example/client"
callers of node where root = "app" and identity_key = "v1:[\"method\",\"billing\",\"invoices\",\"InvoiceService\",\"Approve\",\"\",\"\"]"
callees of node where root = "app" and module = "billing" and type = "InvoiceService" and method = "Approve"
unresolved calls where root = "app"
```

Predicates use exact equality. `symbol_key` is a discovery projection, not a unique identity, so graph queries fail when their selector resolves zero or multiple nodes. Add `root`, `identity_key`, or enough structured identifier fields to select exactly one target.

## Resolution pipeline

Every query runs these stages in order:

1. `parse` applies the generated PEG parser and produces a typed operation plus predicates. Trailing input, unknown fields, unquoted values, and incomplete graph expressions fail here.
2. `project` resolves the requested project. When only a snapshot UUID is supplied, its owning project is loaded; a supplied project key is validated against it.
3. `snapshot` follows `uir_project_heads` when the caller supplied a project, or loads the explicit snapshot UUID.
4. `root` reconciles the `--root` flag with any `root` predicate and resolves that key inside the selected snapshot. Conflicting roots fail.
5. `target` runs only for `callers` and `callees`. It applies structured predicates inside the resolved scope and requires exactly one node before graph traversal.
6. `execute` returns deterministic, bounded results. Node queries are ordered by root and identity; call queries return unique nodes; unresolved-call queries return edge occurrences.

Root scope identifies the graph target for `callers` and `callees`; it does not restrict returned neighbours. A caller or callee in another root of the same snapshot is therefore retained. Resolved call traversal uses node IDs and `relationship_type = "call"`, never string inference.

## Go API

The command is a thin wrapper over the reusable pipeline:

```go
database, err := storage.UirDB(ctx, storage.DBOptions{DSN: dsn})
if err != nil {
    return err
}
pipeline, err := query.NewPipeline(database)
if err != nil {
    return err
}
result, err := pipeline.Run(ctx,
    `callers of node where root = "app" and package = "invoices" and type = "InvoiceService" and method = "Approve"`,
    query.ScopeOptions{ProjectKey: "billing", Limit: 100},
)
```

`Result.Stages` makes the successful resolution decisions observable. Failures are stage-qualified and stop immediately; the pipeline does not broaden scope or fall back from structured identity to a display string.

## Regenerating the parser

The generated parser is checked in so consumers do not need the generator to compile. After changing `query/grammar.peg`, regenerate it with:

```sh
make query-parser
```

The generator is pinned as a Go tool dependency, disables the unused syntax tree, and normalizes its generated-code notice so regeneration is deterministic across workspaces.
