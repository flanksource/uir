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

SQL supports `SELECT fields FROM source [AS alias] [WHERE predicate] [ORDER BY fields] [LIMIT integer]`, with an optional final semicolon. Keywords are case insensitive. A source is a typed selector, nested navigation function, or standalone `files()`. Joins, aggregation, subqueries, CTEs, and writes are outside this grammar.

`symbol` is the canonical symbol projected by navigation: the enclosing caller for `callers`, the callee for `callees`, the referenced entity for `references`, and the source or target for native incoming or outgoing witnesses. Symbol fields include `id`, `kind`, `name`, `query_name`, `module`, `package`, `owner`, `visibility`, and `props`. Witness fields include `path`, `line`, `column`, `package`, `module`, `role`, `dispatch`, `depth`, and `snapshot`. Native witnesses expose `relationship.kind`, `relationship.resolution`, and `relationship.props`.

Predicates support field-to-literal comparisons, `symbol [NOT] IN selector`, `IS [NOT] NULL`, `[NOT] MATCH`, `[NOT] IMATCH`, `file.build_constraint [NOT] REQUIRES 'tag'`, parentheses, `NOT`, `AND`, and `OR`. `NOT` binds before `AND`, which binds before `OR`. Missing optional values are `NULL`; comparisons with `NULL` are unknown and only true predicates retain rows.

Globs are anchored. `*` matches inside one slash segment, `?` matches one character, `**` matches whole segments, `/...` abbreviates `/**`, and backslashes escape glob characters. `IMATCH` uses Unicode simple case folding; it does not expand characters such as `ß` into `ss`. Selector patterns remain case sensitive, and matching never changes canonical identity.

Registered kind names provide selector prefixes. `func:` retains all callable kinds; `type:` retains the type category. Builtin selectors keep their compact semantics. Namespaced prefixes such as `http.route:` match the registered kind's entity name; `api:` aliases `endpoint`. Property references bind to declared JSON Schema types, including nested object fields. Unknown or ambiguously typed properties fail. `FROM http.route:* AS r` makes `r.method` shorthand for `symbol.props.method`.

Rows default to 100 and accept limits from 1 to 1,000. Navigation finishes before `WHERE`, ordering, and limiting, so a predicate does not remove intermediate traversal nodes. `total` counts matching rows before the limit. `SELECT *` includes canonical symbols, properties, and witness fields.

## Functions and bounds

| Function | Result and options |
| --- | --- |
| `files()` | Non-excluded indexed files, including files with zero definitions. Standalone source; `include_excluded => TRUE` also includes build-excluded snapshot members. |
| `definitions(s)` | Declaration locations. |
| `callers(s)`, `callees(s)` | Call-site witnesses; `depth` defaults to 1. |
| `references(s)`, `reads(s)`, `writes(s)` | Recorded use locations. A direct selector matches identities referenced in the selected snapshots, including external dependencies without local declarations. |
| `methods(s)`, `implementers(s)`, `inheritors(s)` | Existing declaration witnesses. |
| `incoming(s)`, `outgoing(s)` | Native witnesses; optional `via` lists exact registered relationship names. |
| `traverse(s)` | Reachable symbols with minimum hop distances; `direction` defaults to `outgoing`, `depth` to 3, optional `via`. |
| `path(a, b)` | One shortest recorded path; `depth` defaults to 8, optional `via`. |
| `graph(s)` | Nodes, native kinds, sites, properties, and omissions; `direction` defaults to `both`, `depth` to 2, `nodes` to 150; optional `via`, `exclude`, or `access`. |

Nested functions consume canonical symbol projections. Paths and graphs are terminal sources and require `SELECT *` without `WHERE`, `ORDER BY`, or `LIMIT`. Depth accepts 1–8. Traversal fails beyond 10,000 symbols; graph bounds accept at most 1,000 nodes and report omissions. `direction` accepts `incoming` or `outgoing`; graph-only `both` unions independent directional walks. Without `via`, traversal and paths follow calls and known dispatch. `via` is a comma-separated string of exact native names; unknown names fail. Native graph `via` and call/data graph `access` are mutually exclusive. Existing exclusions apply; `exclude => 'none'` disables them.

Traversal describes recorded possible reachability. It does not guarantee execution. Ambiguous candidates and unresolved references remain visible without inferred links between similar names.

## Resolved variable types

`symbol.type` is the resolved type of a package-level variable, available in projection, predicates and ordering. For example:

```sql
SELECT symbol.name, path, line, symbol.type
FROM var:*
WHERE symbol.type = 'context.Context' OR symbol.type = '*gorm.io/gorm.DB'
```

Types use canonical import paths and preserve pointers/composites. Import aliases and type aliases resolve to their targets; distinct named types keep their identity. Local variables and fields are outside this initial type field; non-variable symbols return `NULL`. Request the field explicitly; `SELECT *` is unchanged. Types come from immutable document declarations in format 8, never from current source files. Queries requesting types require complete typed coverage and fail before filtering if a variable's type was not recorded; reindex or republish with type facts. Historical snapshots retain their original availability. External producers may supply a nonempty `declared_type` string on a proven `var` declaration in format 8. Metric-only queries retain their document-free path; type queries use the existing decoded document cache.

## Build constraints

Inspect the explicit modern Go build constraint, including files excluded from the indexed build:

```sh
uir query "SELECT path, file.build_constraint FROM files(include_excluded => TRUE) WHERE path MATCH '**/hack/**/*.go' AND file.build_constraint NOT REQUIRES 'hack' ORDER BY path"
```

`include_excluded` accepts `TRUE` or `FALSE` and defaults to false. The option exposes source revisions and excluded documents already in the selected snapshot. Gitignore, module scope and test-file discovery still apply. Excluded documents have no extracted symbols; their zero `file.definitions` is an extraction count. The option does not type-check excluded code.

`file.build_constraint` is a string containing the normalized expression without `//go:build`. A Go source without a recognized modern header returns an empty string; a non-Go source returns `NULL`. File aliases support `f.build_constraint`. Request the field explicitly: `SELECT *` retains its six existing file columns.

`REQUIRES 'hack'` accepts a single case-sensitive positive Go tag and performs exact Boolean analysis. The expression must be satisfiable and impossible to satisfy without the tag. `hack`, `hack && linux`, and `(hack || linux) && !linux` require `hack`; `ignore`, `!hack`, `hack || linux`, and `hack && !hack` do not. Tags are Boolean variables independent of the host environment. Filename suffixes and legacy-only `// +build` directives do not satisfy this explicit modern-header rule. `NOT REQUIRES` negates the result; non-Go `NULL` remains unknown under either operator.

The field reads hash-verified source bytes belonging to the immutable snapshot through the existing checkout/stored-blob/pinned-Git recovery path. No schema migration or reindex is required when those bytes remain available. Unavailable or corrupt source bytes and malformed or duplicate recognized directives are execution errors with source and snapshot context. Exact analysis fails explicitly beyond 65,536 decision nodes or on cancellation.

Ordinary file and metric queries perform no additional source reads. Constraint queries recover source bytes on cache misses and reuse a bounded, per-pipeline cache of parsed headers keyed by content hash. Cold queries can read all selected Go source candidates before WHERE filtering; LIMIT limits returned rows, not this work. Querying historical snapshots never silently substitutes current checkout content.

## Stored metrics

Find small files with few definitions, or callables with many parameters or long bodies:

```sh
uir query 'SELECT path, file.lines, file.definitions FROM files() WHERE file.lines < 100 AND file.definitions < 3 ORDER BY path LIMIT 1000'
uir query 'SELECT symbol.name, path, line, symbol.metrics.param_count, symbol.metrics.body_lines FROM func:* WHERE symbol.metrics.param_count > 3 OR symbol.metrics.body_lines > 50 ORDER BY path, line'
```

| Field | Meaning |
| --- | --- |
| `file.lines` | Physical source lines, including blanks and comments. Empty content has zero lines; a terminating newline adds no extra line; CRLF counts once. |
| `file.definitions` | The active document's indexed declaration count, including fields, package variables and constants, excluding references and function-local bindings. |
| `file.functions` | Number of declared functions and concrete methods; excludes interface signatures, function literals and function-valued variables. |
| `file.structs` | Number of declared struct types; excludes fields, interfaces and type aliases. |
| `symbol.metrics.param_count` | Value parameters, expanding grouped names and counting a variadic parameter once. Receivers and type parameters do not count. |
| `symbol.metrics.body_lines` | Inclusive physical span of the body's delimiters. Documentation and signature-only lines do not count. A one-line body counts as one; a declaration without a body returns `NULL`. |

All four fields have catalog type `integer`. `files()` also exposes `path`, `module`, `package`, and `snapshot`; its `SELECT *` projects these six fields. `FROM files() AS f` supports `f.path`, `f.lines`, and `f.definitions`. Symbol navigation cannot consume a file source. Symbol `SELECT *` retains its existing fields; request callable metrics explicitly. Standard root, location, snapshot and limit options apply. Test files participate when the snapshot indexed them. Existing coverage warnings describe syntax/partial extraction.

Source and declaration metrics are recorded during indexing/publication, with immutable source revisions and document membership deciding which values a snapshot serves. The example projections read neither source bytes nor full document JSON. Navigation and requested symbol properties can require document content. Filtering and ordering still inspect matching candidates before limiting; `LIMIT` caps returned rows rather than scanning work.

Existing snapshots remain readable. Metrics are not backfilled during migration or query execution. When a requested metric was not recorded, the query fails with its snapshot/path/declaration and reindex or republish guidance; it never substitutes zero. Reindexing a historical revision provides a metric-bearing snapshot while leaving earlier documents intact. Ordinary SQL `NULL` rules apply to a known bodyless declaration and to metric fields on non-callable symbols.

External producers may include `metrics` on a callable `DocumentSymbol` in document format 7:

```json
{"version": 1, "param_count": 2, "body_lines": 4}
```

Here `version` is the metric semantics version, `param_count` is nonnegative, and `body_lines` is positive or `null` for a declaration without a body. Omit the entire metric record when it is unavailable; queries requesting it then fail with republish guidance. File lines are derived from the publication's verified source. Go indexing records function/method parameters and body spans, including bodyless interface signatures. Metrics do not change canonical symbol identities or semantic fingerprints.

Run the focused and repository acceptance checks with `gavel fixtures fixtures/queryable-metrics.md`. Real-corpus benchmark instructions are in [the metric plan](queryable-metrics-plan.md).

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

`POST /api/v1/modules/query` accepts `args: [expression]`, `language: auto|compact|sql`, and existing root, location, snapshot, and limit options. Row responses add `columns` and `rows` beside the existing `matches`, `symbols`, `coverage`, and `stages`; paths use `path`, and graphs use `graph`. Native graph sites carry their own snapshot and source location so evidence opens the correct file.

SQL CLI JSON and YAML retain this result envelope, including pre-limit totals and source witnesses. Human-readable output renders projected values, paths, or graphs. Compact CLI JSON retains its existing row format.

`GET /api/v1/modules/query-catalog` supplies functions, registered kinds, fields, and property schemas for completion. `GET /api/v1/modules/suggest-selectors?...&language=sql` completes registered selector prefixes. The Query page persists language in its URL, renders projected columns, paths, and graphs, and opens witnesses in Source. The Query page and Cmd+K share SQL completion and positioned diagnostics. Ctrl/Cmd+Space requests suggestions; Ctrl/Cmd+Enter runs the query.

Run the executable verification fixture with `gavel fixtures fixtures/sql-query.md`. It checks parser and query behavior, API integration, frontend models, and the runnable CLI publication.
