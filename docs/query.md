# Querying indexed modules

`uir query` uses the PEG grammar in `query/grammar.peg` and a resolution pipeline over immutable module snapshots. It reads structured node and call projections from source revisions; it does not translate the expression directly into SQL or use the removed project tables.

## CLI and scope

```sh
uir query 'nodes where method = "Run"' --root example.org/service
uir query 'callers of node where package = "service" and method = "Run"' --root example.org/service --location ../service-feature
uir query 'unresolved calls' --snapshot 6f8b2c6e-79af-4b59-8736-e45696f0c546 --format json
```

`--dsn` on the root command accepts a PostgreSQL DSN, `sqlite://` URL, or plain `.db` path. Without it, the CLI uses its configured local SQLite database. `--root` is the full `go.mod` module path; `--location` identifies a registered canonical checkout; `--snapshot` selects one historical ready snapshot by UUID. Location and snapshot are mutually exclusive. A snapshot plus root must agree. `--limit` defaults to 100 and accepts 1–1000. Clicky's shared format flags select table, JSON, YAML, and other supported renderers.

Without an explicit location or snapshot, `nodes` and `unresolved calls` read the primary head of each selected root. `callers` and `callees` examine all registered checkout heads for graph target selection. If more than one declaration matches, the result contains `candidate` rows with root, location, source, and snapshot; select a location or snapshot instead of assuming one target. A selected historical snapshot never silently falls back to a newer head.

## Grammar

```text
nodes [where predicate [and predicate ...]]
callers of node where predicate [and predicate ...]
callees of node where predicate [and predicate ...]
unresolved calls [where root = "module/path"]
predicate := field = "value"
```

Keywords and fields are lowercase; values use double-quoted Go-style strings. Node predicates support `node_type`, `module`, `package`, `type`, `method`, `field`, `signature`, `language`, `symbol_key`, `identity_key`, and `root`. `root` must agree with `--root` when both are present. The unresolved-call expression accepts only `root`; graph queries require at least one non-root symbol predicate. Comparisons are exact equality, not substring or regex matching. An incomplete expression, unknown field, unquoted value, or trailing input fails at parse time.

```text
nodes where language = "go" and node_type = "method"
nodes where root = "example.org/service" and package = "example.org/service/api"
callers of node where identity_key = "v1:[\"method\",\"service\",\"api\",\"Handler\",\"Run\",\"\",\"\"]"
callees of node where type = "Handler" and method = "Run"
unresolved calls where root = "example.org/service"
```

The `identity_key` example is illustrative: use the indexed declaration's actual structured identifier, because its module and package fields depend on extraction. `symbol_key` is a display/search projection and need not be unique. For the exact formats and reference semantics, see [symbols](symbols.md).

## Resolution

`query.Pipeline.RunModules(ctx, expression, query.ModuleScopeOptions{RootKey: root, Location: location, SnapshotID: snapshot, Limit: limit})` parses the expression, reconciles the root selector, resolves ready snapshot scope, reconstructs effective sources, then filters or traverses call projections. It returns `ModuleQueryResult` with `operation`, `matches`, and successful `stages`. A match includes kind, root, checkout, snapshot, module-relative source, position, and full `uir.Identifier`. Results are deterministic and bounded by the requested limit.

Calls retain a structured target identifier. A signature-free call locator may match a declaration with a signature; ambiguous or dynamic targets are not compiler-proven edges. `unresolved calls` means a target could not be found in the selected index scope or syntax extraction could not resolve its expression. The extractor does not use `go/types`, evaluate build tags, or index external dependencies automatically.

The generated parser is checked in. After changing `query/grammar.peg`, run `make query-parser`; tests check parser behavior.
