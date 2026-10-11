# SQL navigation

SQL queries read the same immutable snapshots as compact queries. The Participle parser builds a typed syntax tree, binds selectors and fields against the registered catalog, then executes through the existing query and graph pipeline.

```sh
uir query 'SELECT symbol.name, path, line FROM callers(method:Store.Save) WHERE symbol IN func:* ORDER BY symbol.name LIMIT 20'
```

Ordinary SQL strings use single quotes. When passing an expression containing those strings to a shell, enclose the whole expression in double quotes:

```sh
uir query "SELECT symbol.name, depth FROM traverse(func:Entry, depth => 3) WHERE symbol.name IMATCH '*handle*'"
```

The first example above uses no string literals; double quotes inside selectors delimit names containing spaces or punctuation, such as `http.route:"Create order"`.

## Grammar and fields

Version one supports `SELECT fields FROM source [AS alias] [WHERE predicate] [ORDER BY fields] [LIMIT integer]`, with an optional final semicolon. Keywords are case insensitive. A source is a typed selector or nested navigation function. Joins, aggregation, subqueries, CTEs, and writes are outside this grammar.

`symbol` is the canonical symbol projected by navigation: the enclosing caller for `callers`, the callee for `callees`, the referenced entity for `references`, and the source or target for native incoming or outgoing witnesses. Symbol fields include `id`, `kind`, `name`, `query_name`, `module`, `package`, `owner`, `visibility`, and `props`. Witness fields include `path`, `line`, `column`, `package`, `module`, `role`, `dispatch`, `depth`, and `snapshot`. Native witnesses expose `relationship.kind`, `relationship.resolution`, and `relationship.props`.

Predicates support field-to-literal comparisons, `symbol [NOT] IN selector`, `IS [NOT] NULL`, `[NOT] MATCH`, `[NOT] IMATCH`, parentheses, `NOT`, `AND`, and `OR`. `NOT` binds before `AND`, which binds before `OR`. Missing optional values are `NULL`; comparisons with `NULL` are unknown and only true predicates retain rows.

Globs are anchored. `*` matches inside one slash segment, `?` matches one character, `**` matches whole segments, `/...` abbreviates `/**`, and backslashes escape glob characters. `IMATCH` uses Unicode simple case folding; it does not expand characters such as `ß` into `ss`. Selector patterns remain case sensitive, and matching never changes canonical identity.

Registered kind names provide selector prefixes. `func:` retains all callable kinds; `type:` retains the type category. Builtin selectors keep their compact semantics. Namespaced prefixes such as `http.route:` match the registered kind's entity name; `api:` aliases `endpoint`. Property references bind to declared JSON Schema types, including nested object fields. Unknown or ambiguously typed properties fail. `FROM http.route:* AS r` makes `r.method` shorthand for `symbol.props.method`.

Rows default to 100 and accept limits from 1 to 1,000. Navigation finishes before `WHERE`, ordering, and limiting, so a predicate does not remove intermediate traversal nodes. `total` counts matching rows before the limit. `SELECT *` includes canonical symbols, properties, and witness fields.

## Functions and bounds

| Function | Result and options |
| --- | --- |
| `definitions(s)` | Declaration locations. |
| `callers(s)`, `callees(s)` | Call-site witnesses; `depth` defaults to 1. |
| `references(s)`, `reads(s)`, `writes(s)` | Recorded use locations. |
| `methods(s)`, `implementers(s)`, `inheritors(s)` | Existing declaration witnesses. |
| `incoming(s)`, `outgoing(s)` | Native witnesses; optional `via` lists exact registered relationship names. |
| `traverse(s)` | Reachable symbols with minimum hop distances; `direction` defaults to `outgoing`, `depth` to 3, optional `via`. |
| `path(a, b)` | One shortest recorded path; `depth` defaults to 8, optional `via`. |
| `graph(s)` | Nodes, native kinds, sites, properties, and omissions; `direction` defaults to `both`, `depth` to 2, `nodes` to 150; optional `via`, `exclude`, or `access`. |

Nested functions consume canonical symbol projections. Paths and graphs are terminal sources and require `SELECT *` without `WHERE`, `ORDER BY`, or `LIMIT`. Depth accepts 1–8. Traversal fails beyond 10,000 symbols; graph bounds accept at most 1,000 nodes and report omissions. `direction` accepts `incoming` or `outgoing`; graph-only `both` unions independent directional walks. Without `via`, traversal and paths follow calls and known dispatch. `via` is a comma-separated string of exact native names; unknown names fail. Native graph `via` and call/data graph `access` are mutually exclusive. Existing exclusions apply; `exclude => 'none'` disables them.

Traversal describes recorded possible reachability. It does not guarantee execution. Ambiguous candidates and unresolved references remain visible without inferred links between similar names.

## Runnable domain publication

From the repository root, build the CLI and generate a publication containing HTTP → handler → topic → consumer links plus parallel Kubernetes relationships:

```sh
make build
mkdir -p .tmp
go run ./hack/sql-domains > .tmp/sql-domains.json
.bin/uir --dsn .tmp/sql-example.db import .tmp/sql-domains.json
.bin/uir --dsn .tmp/sql-example.db query "SELECT r.method, r.path, r.description FROM http.route:* AS r WHERE r.path IMATCH '/orders/**'"
.bin/uir --dsn .tmp/sql-example.db query "SELECT * FROM path(http.route:Orders, messaging.consumer:Consume, via => 'http.handled_by,messaging.publishes_to,messaging.delivers_to')" --format json
.bin/uir --dsn .tmp/sql-example.db query "SELECT * FROM graph(k8s.service:checkout, via => 'k8s.selects,k8s.uses_config', direction => 'outgoing')" --format json
.bin/uir --dsn .tmp/sql-example.db serve --host localhost --port 8080
```

The generator in [`hack/sql-domains/main.go`](../hack/sql-domains/main.go) is a complete producer example. `kinds` registers each custom name, handle category, and optional object JSON Schema. `relationships` registers namespaced native names, coarse UIR types, and optional property schemas. Immutable declarations store queryable `props` and preserve the producer's full `payload`. Schemas are immutable for a registered name and use local references; use a new kind name to change its schema.

Document format 4 adds `relationships`: source canonical ID, target IDs or reference, native kind, typed properties, resolution (`static`, `dynamic`, `ambiguous`, or `unresolved`), and source range/byte/text evidence. Publication validates schemas and all identities before updating the head. Source and target postings use the existing posting index; relationship details stay in snapshot-selected documents. Include distinguishing tenant, service, broker, cluster, or namespace context in canonical identities' module, package, and owner fields.

Concrete HTTP, messaging, and Kubernetes extractors are separate producers. This implementation consumes their explicit links. Older snapshots still answer queries over their existing facts; native navigation on documents before format 4 fails with reindex/reimport guidance.

## API and browser

`POST /api/v1/modules/query` accepts `args: [expression]`, `language: auto|compact|sql`, and existing root, location, snapshot, and limit options. Row responses add `columns` and `rows` beside the existing `matches`, `symbols`, `coverage`, `warnings`, and `stages`; paths use `path`, and graphs use `graph`. Native graph sites carry their own snapshot and source location so evidence opens the correct file.

SQL CLI JSON and YAML retain this result envelope, including pre-limit totals and source witnesses. Human-readable output renders projected values, paths, or graphs. Compact CLI JSON retains its existing row format.

`GET /api/v1/modules/query-catalog` supplies functions, registered kinds, fields, and property schemas for completion. `GET /api/v1/modules/suggest-selectors?...&language=sql` completes registered selector prefixes. The Query page persists language in its URL, renders projected columns, paths, and graphs, and opens witnesses in Source. The Query page and Cmd+K share SQL completion and positioned diagnostics. Ctrl/Cmd+Space requests suggestions; Ctrl/Cmd+Enter runs the query.

Run the executable verification fixture with `gavel fixtures fixtures/sql-query.md`. It checks parser and query behavior, API integration, frontend models, and the runnable CLI publication.
