# SQL language reference

UIR SQL selects symbols, source files, and recorded relationship witnesses from immutable snapshots. It reads the same facts as [compact queries](query.md), independently of whether UIR stores them in SQLite or PostgreSQL. This page describes the implemented language; the [SQL examples](sql-examples.md) show every source function and common investigations.

```sh
uir query 'SELECT symbol.name, path, line FROM callers(method:Store.Save) WHERE symbol IN func:* ORDER BY symbol.name LIMIT 20'
```

Ordinary SQL strings use single quotes. When passing an expression containing those strings to a shell, enclose the whole expression in double quotes:

```sh
uir query "SELECT symbol.name, depth FROM traverse(func:Entry, depth => 3) WHERE symbol.name IMATCH '*handle*'"
```

The first example above uses no string literals; double quotes inside selectors delimit names containing spaces or punctuation, such as `http.route:"Create order"`. A leading unquoted `SELECT` selects SQL automatically. Use `--language sql` to force SQL, including when the query begins with a comment. Parsing and binding errors retain their diagnostics; they do not fall back to compact parsing.

## Scope and execution

Queries read saved snapshots and do not index or modify source. Index a Go module with `uir add /path/to/module`, or import the [domain example](#runnable-domain-publication), before querying it.

```sh
uir query 'SELECT symbol.name FROM func:*' --root example.org/service
uir query 'SELECT path FROM files()' --location /path/to/service-worktree
uir query 'SELECT symbol.name FROM func:*' --snapshot SNAPSHOT_UUID
uir query 'SELECT symbol.name FROM func:*' --all --format json
```

`--root` selects a registered module path, `--location` a checkout, and `--snapshot` one immutable published snapshot. Location and snapshot cannot be combined. Inside a registered Go checkout, the CLI defaults to that checkout; outside one it searches available primary heads. `--all` searches every registered root and cannot combine with root, location, or snapshot. HTTP requests never infer scope from the server's working directory. [Query scope](query.md#quick-start) describes missing heads and incomplete coverage.

Execution resolves the source and finishes navigation before applying `WHERE`, ordering, projection, and the row limit. A filter therefore does not prune intermediate traversal nodes. Witness rows are not a distinct-symbol list: two uses of the same symbol can return two rows.

## Grammar and fields

### Statement grammar

The following notation uses brackets for optional parts and braces for repetition; they are not query characters.

```text
query       = SELECT projection FROM source [AS alias]
              [WHERE predicate] [ORDER BY ordering {, ordering}]
              [LIMIT integer] [;]
projection  = * | field [AS name] {, field [AS name]}
source      = selector | function([arguments])
arguments   = source {, source} [, options] | options
options     = option {, option}
option      = name => literal
ordering    = field_or_projection_alias [ASC | DESC]
predicate   = conjunction {OR conjunction}
conjunction = condition {AND condition}
condition   = NOT condition | (predicate) | comparison
comparison  = field (= | != | < | <= | > | >=) literal
            | symbol [NOT] IN selector
            | field IS [NOT] NULL
            | field [NOT] (MATCH | IMATCH) 'glob'
            | file.build_constraint [NOT] REQUIRES 'tag'
literal     = 'string' | signed_decimal | TRUE | FALSE | NULL
```

Exactly one statement is accepted. Clauses must appear in the order shown. `*` is the entire projection; it cannot be mixed with explicit columns. Projections accept fields, not expressions or literals. `AS` is required for column and source aliases. Column aliases name output keys and can be used in `ORDER BY`, but not `WHERE`. Duplicate output names are errors.

```sql
SELECT symbol.name AS name, path, line
FROM callers(method:Store.Save)
WHERE symbol.visibility = 'exported'
ORDER BY name ASC, path ASC, line DESC
LIMIT 20;
```

Keywords, function names, and option names are case insensitive. Field names, aliases, selector prefixes/patterns, relationship names, and literal values retain their spelling. `--` line comments and `/* ... */` block comments are accepted. Strings escape a single quote by doubling it (`'Reader''s guide'`); backslashes in SQL strings remain literal and can escape glob characters. Numbers are signed integers or decimals, without exponent notation. Double quotes belong to selector values, not SQL string literals or field identifiers.

Rows default to 100. SQL `LIMIT` overrides the CLI/API `limit`; either must be an integer from 1 to 1,000. `total` counts matching rows before the limit. `ASC` is the default, sorting uses scalar values, and `NULL` sorts last in ascending order and first in descending order. There is no `OFFSET` clause.

### Selectors

Every symbol source is a typed selector or a navigation function consuming one. An empty selector result returns no rows. Short selectors can match several canonical identities; qualify the module/package or narrow the scope when that matters.

| Selector | Selects | Example |
| --- | --- | --- |
| `func:pattern` | The callable category: functions, methods, constructors, endpoints, and custom callable kinds. | `func:Store.Save` |
| `method:pattern` | Method symbols. | `method:Store.*` |
| `field:pattern` | Field symbols. | `field:Order.Total` |
| `var:pattern` | Package variables. | `var:Current` |
| `type:pattern` | The type category, including custom type kinds. | `type:Repository` |
| `struct:pattern` | Defined, non-alias Go types whose underlying form is a struct. | `struct:Order` |
| `pkg:pattern` | Symbols declared in matching package paths. | `pkg:example.org/service/internal/**` |
| `pkg:module:relative_package` | Symbols in matching modules and module-relative packages; `.` means the root package. | `pkg:example.org/service:internal/**` |
| `mod:pattern` | Symbols declared in matching registered roots. | `mod:example.org/service` |
| `kind:name[:pattern]` | One exact registered kind, optionally restricted by name. | `kind:sql.procedure:Load*` |
| `registered_kind:pattern` | The entity name within one registered kind. | `table:orders`, `column:total`, `http.route:Orders` |
| `api:pattern` | Alias for the registered `endpoint` kind. | `api:CreateOrder` |

Registered prefixes include builtin kinds such as `const`, `table`, `column`, `record`, and `interface`, plus producer kinds such as `http.route`. An unregistered kind fails. `pkg:` and `mod:` return declarations within containers, rather than creating container rows. SQL does not accept bare compact spellings such as `Store.Save <`, compact set operators, or a free-form compact expression inside `FROM`.

Selector values can use double quotes, for example `func:"Premium Calc (Annual)"` or `pkg:"Regional Plan":"Group Life/**"`. Quoting permits spaces and punctuation; glob characters still describe patterns and must be escaped to match literally. See [typed selectors](query.md#grammar) for contextual name matching and identity rules.

### Fields

`symbol` is the canonical symbol projected by the source. `path`, `line`, `package`, and `module` describe the witness site, which can be in a different package or root from that symbol. For example, `references(field:Order.Total)` projects the field while its witness is each use; `callers(method:Store.Save)` projects the enclosing caller while preserving the call site.

| Field | Catalog type | Meaning |
| --- | --- | --- |
| `symbol` | symbol | The full projected symbol object; supports identity membership with `IN`. |
| `symbol.id` | string | Canonical identity, independent of the witness path. |
| `symbol.kind`, `symbol.name`, `symbol.query_name` | string | Registered kind, simple name, and qualified query name. |
| `symbol.module`, `symbol.package` | string | The projected symbol's module key and package path. |
| `symbol.owner`, `symbol.visibility` | string | Owning declaration's name and `exported` or `internal` visibility. |
| `symbol.props`, `symbol.props.<field>` | object or schema type | Producer properties, including declared nested fields. |
| `symbol.type` | string | Resolved package-variable type; see [variable types](#resolved-variable-types). |
| `symbol.metrics.param_count`, `symbol.metrics.body_lines` | integer | Callable measures; see [metrics](#stored-metrics). |
| `symbol.metrics.<name>` | integer | A [producer metric](#producer-metrics) declared by a selected kind. |
| `path`, `package`, `module`, `snapshot` | string | Witness source path, package, root key, and snapshot ID. |
| `line`, `column` | number | One-based witness line and UTF-16 column when positioned. |
| `role` | string | Recorded occurrence role or native relationship name. |
| `dispatch` | boolean | Whether a witness was reached through known interface dispatch. |
| `depth` | number | Recorded traversal hop distance. |
| `relationship.kind`, `relationship.resolution` | string | Native kind and `static`, `dynamic`, `ambiguous`, or `unresolved` resolution. |
| `relationship.props`, `relationship.props.<field>` | object or schema type | Native relationship properties, when present. |

Symbol `SELECT *` includes the symbol, its identity/name/ownership/property fields, witness fields, and relationship fields. It does not automatically include variable types or metrics; request them explicitly. Missing optional facts are `NULL`; unavailable required type/metric capabilities fail as described below.

File sources have a separate field set: `path`, `module`, `package`, `snapshot`, `file.lines`, `file.definitions`, `file.functions`, `file.structs`, and `file.build_constraint`. Their wildcard includes the first eight fields, omitting `file.build_constraint`. Symbol sources do not expose file metrics, and file sources do not expose `symbol` fields.

A source alias on a symbol source abbreviates **properties**: `FROM http.route:* AS r` makes `r.method` mean `symbol.props.method`, not `symbol.method`. Continue to write `symbol.name` for the symbol name. A file alias supports both site fields (`f.path`) and file fields (`f.lines`, `f.functions`, `f.build_constraint`). Alias names must match exactly.

### Predicates and globs

| Form | Example predicate | Behavior |
| --- | --- | --- |
| `=`, `!=`, `<`, `<=`, `>`, `>=` | `symbol.metrics.param_count >= 4` | Compares a scalar field with a type-compatible literal. |
| `symbol IN selector` | `symbol IN pkg:example.org/service:api/**` | Retains the projected canonical identities selected on the right. |
| `symbol NOT IN selector` | `symbol NOT IN method:Store.*` | Excludes those projected identities. |
| `IS NULL`, `IS NOT NULL` | `relationship.kind IS NOT NULL` | Tests absence/presence explicitly. |
| `MATCH`, `NOT MATCH` | `path MATCH 'internal/**'` | Anchored, case-sensitive glob matching on a string field. |
| `IMATCH`, `NOT IMATCH` | `symbol.name IMATCH '*save*'` | Anchored glob matching with Unicode simple case folding. |
| `REQUIRES`, `NOT REQUIRES` | `file.build_constraint REQUIRES 'hack'` | Exact Boolean analysis of a modern Go header; file sources only. |
| `NOT`, `AND`, `OR`, parentheses | `NOT (role = 'write' OR line IS NULL)` | Combines predicates, with precedence `NOT`, then `AND`, then `OR`. |

Comparisons take literal values on the right, not another field. `IN` accepts a selector, not a literal list, and its left operand must be `symbol`. Objects, arrays, and the whole `symbol` object cannot be scalar-compared or ordered; project a typed scalar property instead. An unknown property or one whose selected schemas disagree about its type fails binding.

`NULL` follows three-valued logic: a comparison or match with a missing value is unknown, `NOT unknown` is unknown, and `WHERE` retains only true rows. Use `IS NULL`, rather than `= NULL`, to find missing values. For example, retain routes with an absent description or a nonempty POST description:

```sql
SELECT symbol.name, r.description
FROM http.route:* AS r
WHERE r.description IS NULL OR (r.method = 'POST' AND r.description != '')
```

Globs are anchored to the whole string. `*` matches within one slash segment, `?` matches one character, `**` matches whole segments, `/...` abbreviates `/**`, and backslashes escape glob characters. Thus `'*save*'` is a name substring search, `'internal/*'` covers one directory level, and `'internal/**'` covers descendants. `%` and `_` are not SQL wildcards. `IMATCH` uses simple case folding, so it does not expand `ß` into `ss`. Selector patterns remain case sensitive, and matching never changes canonical identity.

## Functions and bounds

| Function | Result and options |
| --- | --- |
| `files()` | Non-excluded indexed files, including files with zero definitions. Standalone source; `include_excluded => TRUE` also includes build-excluded snapshot members. |
| `definitions(s)` | Declaration witnesses, projecting the declared symbol. No options. |
| `callers(s)` | Incoming witnesses to callables, projecting the enclosing caller; `depth` defaults to 1. |
| `callees(s)` | Outgoing call witnesses, projecting the callee; `depth` defaults to 1. |
| `references(s)` | All recorded non-definition uses, projecting the referenced symbol. No options. |
| `reads(s)` | Recorded read uses, projecting the referenced symbol. No options. |
| `writes(s)` | Recorded write uses, projecting the written symbol. No options. |
| `methods(s)` | Owned method declarations, projecting each method. No options. |
| `implementers(s)` | Recorded types implementing the selected interfaces. No options. |
| `inheritors(s)` | Direct struct/interface embedding declarations. No options. |
| `incoming(s)` | Native witnesses, projecting each relationship's source; optional `via`. |
| `outgoing(s)` | Native witnesses, projecting each relationship's target; optional `via`. |
| `traverse(s)` | Reachable symbols with minimum hop distances; `direction` defaults to `outgoing`, `depth` to 3, optional `via`. |
| `path(a, b)` | One shortest recorded path; `depth` defaults to 8, optional `via`. |
| `graph(s)` | Nodes, native kinds, sites, properties, and omissions; `direction` defaults to `both`, `depth` to 2, `nodes` to 150; optional `via`, `exclude`, or `access`. |

Nested functions consume their inner source's canonical symbol projection, not its printed strings. For example, `callers(callees(func:Entry))` finds callers of the functions `Entry` calls. `references`, `reads`, and `writes` with a direct selector also match referenced dependency identities without a local declaration; a bare selector normally selects declarations. `files()` is standalone and cannot feed symbol navigation. There is no SQL `uses()` function; the compact language's `:uses` relation is documented separately.

Depth-one `callees` reports the declared call target, including an interface method. Multi-hop callee traversal (`depth > 1`), `traverse`, and call paths expand known interface implementations; those implementation witnesses carry `dispatch = TRUE`. Callers of a concrete method can also include recorded interface-dispatch sites. These facts describe possible targets in the selected snapshots, not a guarantee of runtime execution.

Source arguments precede named options, which use `=>`. Each option may appear once and only on a function listing it above. For example, `callers(func:Save, depth => 2)` is valid; `callers(depth => 2, func:Save)` and `reads(field:Total, depth => 2)` fail.

| Option | Allowed values and defaults |
| --- | --- |
| `depth` | Integer 1–8. Defaults: callers/callees 1, traverse 3, path 8, graph 2. |
| `nodes` | Graph-only integer 1–1,000; default 150. Bounds nodes, not SQL rows. |
| `direction` | Traverse: `'incoming'` or `'outgoing'` (default). Graph also accepts `'both'` (default), unioning independent directional walks. |
| `via` | Nonempty comma-separated string of exact registered native relationship names. No wildcard expansion. |
| `exclude` | Graph-only comma-separated package patterns/keywords; default `'std,builtin,gorm.io/...'`. `'none'` disables exclusions. See [graph exclusions](graph.md#excluding-packages). |
| `access` | Graph-only comma-separated `'call'`, `'read'`, `'write'` selections. Omission uses calls. Mutually exclusive with `via`. |
| `include_excluded` | Files-only `TRUE` or `FALSE` (default); includes snapshot members excluded by build selection. |

Without `via`, traversal and paths follow calls and known dispatch. With `via`, they follow only the selected native kinds. `incoming`/`outgoing` always use native relationships; omitting `via` there admits all recorded native kinds. Empty list items, unknown relationships, unsupported options, and out-of-range bounds fail explicitly. Traversal fails beyond 10,000 symbols; graph bounds report omissions.

Paths and graphs are terminal sources: use `SELECT *` without `WHERE`, `ORDER BY`, or SQL `LIMIT`, and do not nest them in other functions. Control a graph with `depth`, `nodes`, and `exclude`. A path follows the source-to-target direction and returns one shortest recorded path; no path means `total: 0` with no `path`. Graphs return nodes, edges, sites, and omission metadata; inspect candidates when a call-graph source is ambiguous.

Traversal describes recorded possible reachability. It does not guarantee execution. Ambiguous candidates and unresolved references remain visible without inferred links between similar names.

## Resolved variable types

`symbol.type` is the resolved type of a package-level variable, available in projection, predicates and ordering. For example:

```sql
SELECT symbol.name, path, line, symbol.type
FROM var:*
WHERE symbol.type = 'context.Context' OR symbol.type = '*gorm.io/gorm.DB'
```

Types use canonical import paths and preserve pointers/composites. Import aliases and type aliases resolve to their targets; distinct named types keep their identity. Local variables and fields are outside this initial type field; non-variable symbols return `NULL`. Request the field explicitly; `SELECT *` is unchanged. Types come from immutable document declarations in format 8, never from current source files. Queries requesting types require complete typed coverage and fail before filtering if a variable's type was not recorded; reindex or republish with type facts. Historical snapshots retain their original availability. External producers may supply a nonempty `declared_type` string on a proven `var` declaration in format 8. Metric-only queries retain their document-free path; type queries use the existing decoded document cache.

## Content generations

`symbol.generation` (integer) counts the distinct implementations a symbol has had along the base chain of the snapshot that declares it: 1 where the chain first defines it, plus one at every later snapshot whose `content_fp` differs. A signature-only change (a parameter rename), a comment-only edit and a move between files keep it; a re-add after a delete continues the count. It is read from `symbol_deltas` without decoding documents, and symbols no selected snapshot declares, such as standard library callees, read `NULL`. Request it explicitly; `SELECT *` is unchanged.

```sql
SELECT symbol.name, path, symbol.generation FROM func:* WHERE symbol.generation > 10 ORDER BY symbol.generation DESC
```

`uir history symbol <selector>` lists the snapshots, with their commits, at which one symbol's generation advanced. The selector must match exactly one declaration in the current checkout. Generations follow publication order: a historical import published on top of a newer head counts in import order, not commit order.

## Effects and determinism

`symbol.effects` (string) lists, sorted and comma-separated, the external effects of a function, method or package-level variable through everything it calls, references as a function value, or reads or writes as a package-level variable, however deep: `time.read`, `random.read`, `env.read`, `env.write`, `process.read`, `process.write`, `file.read`, `file.write`, `network.io`, `db.read`, `db.write`, `global.read` (a package-level variable indexed code writes), `global.write`, `goroutine`, `channel`, `lock.read`, `lock.write`, `map.order` and `unknown`. `symbol.deterministic` (boolean) is true when none applies other than the lock and `map.order` effects. Library calls are classified by the catalog in `effects/catalog.go`; Methods a catalogued library calls back on the values passed to it (`String` and `Error` for `fmt`, `MarshalJSON` for `encoding/json` including nested fields, `Less` for `sort`, and so on) count as calls. `unknown` is a call through an interface, a function value or a type parameter, a value of interface type or a function value passed to a catalogued library, a function without a body (linkname or assembly), an uncatalogued library function, an unresolved call, or a document indexed before format 12, so determinism is never over-claimed. `time.Unix` and `time.Parse` read the local zone (`env.read`). Any file read counts, including files tracked in the repository. Other symbols read `NULL`. Request the fields explicitly; `SELECT *` is unchanged. `uir effects <selector>` reports one symbol's effects with the shortest path to each.

```sql
SELECT symbol.query_name FROM func:* WHERE symbol.deterministic = TRUE
```

## Build constraints

Inspect the explicit modern Go build constraint, including files excluded from the indexed build:

```sh
uir query "SELECT path, file.build_constraint FROM files(include_excluded => TRUE) WHERE path MATCH '**/hack/**/*.go' AND file.build_constraint NOT REQUIRES 'hack' ORDER BY path"
```

`include_excluded` accepts `TRUE` or `FALSE` and defaults to false. The option exposes source revisions and excluded documents already in the selected snapshot. Gitignore, module scope and test-file discovery still apply. Excluded documents have no extracted symbols; their zero `file.definitions` is an extraction count. The option does not type-check excluded code.

`file.build_constraint` is a string containing the normalized expression without `//go:build`. A Go source without a recognized modern header returns an empty string; a non-Go source returns `NULL`. File aliases support `f.build_constraint`. Request the field explicitly: the file wildcard's eight columns do not include it.

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

All six metric/count fields above have catalog type `integer`. `files()` also exposes `path`, `module`, `package`, and `snapshot`; its `SELECT *` projects those four site fields and all four file counts. `FROM files() AS f` supports `f.path`, `f.lines`, `f.definitions`, `f.functions`, and `f.structs`. Symbol navigation cannot consume a file source. Symbol `SELECT *` retains its existing fields; request callable metrics explicitly. Standard root, location, snapshot and limit options apply. Test files participate when the snapshot indexed them. Existing coverage warnings describe syntax/partial extraction.

Source line counts and callable metrics are recorded during indexing/publication, with immutable source revisions and document membership deciding which values a snapshot serves. `file.definitions` reads the active document header; `file.functions` and `file.structs` count declarations from decoded active documents. The line/definition and callable-metric examples above read neither source bytes nor full document JSON. Navigation and requested symbol properties can require document content. Filtering and ordering still inspect matching candidates before limiting; `LIMIT` caps returned rows rather than scanning work.

Existing snapshots remain readable. Metrics are not backfilled during migration or query execution. When a requested metric was not recorded, the query fails with its snapshot/path/declaration and reindex or republish guidance; it never substitutes zero. Reindexing a historical revision provides a metric-bearing snapshot while leaving earlier documents intact. Ordinary SQL `NULL` rules apply to a known bodyless declaration and to metric fields on non-callable symbols.

External producers may include `metrics` on a callable `DocumentSymbol` in document format 7:

```json
{"version": 1, "param_count": 2, "body_lines": 4}
```

Here `version` is the metric semantics version, `param_count` is nonnegative, and `body_lines` is positive or `null` for a declaration without a body. Omit the entire metric record when it is unavailable; queries requesting it then fail with republish guidance. File lines are derived from the publication's verified source. Go indexing records function/method parameters and body spans, including bodyless interface signatures. Metrics do not change canonical symbol identities or semantic fingerprints.

Run the focused and repository acceptance checks with `gavel fixtures fixtures/queryable-metrics.md`. Real-corpus benchmark instructions are in [the metric plan](queryable-metrics-plan.md).

## Producer metrics

A non-Go producer can publish its own per-symbol integer metrics and query them beside the callable ones. A kind declares its metrics in the publication's `kinds`, by name with a description:

```json
{"name": "acme.rule", "category": "callable", "metrics": {
  "variables": {"description": "Variables the rule declares"},
  "fan_in": {"description": "Distinct rules that call this rule"}
}}
```

Every declaration of that kind then records a value for each declared metric in its document symbol's `measures`, which requires document format 10:

```json
{"key": "…", "kind": "acme.rule", "measures": {"variables": 14, "fan_in": 2}, "…": "…"}
```

Publication refuses, before writing anything, a metric name that is not a lower snake_case identifier or that is `param_count` or `body_lines`; `measures` in a document older than format 10 or on a declaration without a canonical `id`; a negative value; a name the symbol's kind does not declare; and a symbol missing a value for one its kind declares. A kind that declares no metrics carries no `measures`. Each error names the kind and the symbol, and the metric at fault. Values are stored in `declaration_measures` by document ordinal, declaration key and name (migration [`22_producer_metrics.hcl`](../storage/migrations/22_producer_metrics.hcl)), and the declared metrics in `symbol_kind_metrics`.

`symbol.metrics.<name>` is a catalogued `integer` field whenever a kind the source selects declares it, the same way a declared property types `symbol.props.<name>`. It works in projection, `WHERE` and `ORDER BY`, on symbol sources and on navigation rows such as `callers`, `callees` and `traverse`, for kinds of any category:

```sh
uir query 'SELECT symbol.name, symbol.metrics.variables, symbol.metrics.fan_in FROM acme.rule:* WHERE symbol.metrics.variables > 10 AND symbol.metrics.fan_in >= 2 ORDER BY symbol.metrics.variables DESC'
```

A metric no selected kind declares is an unknown field. A row whose kind does not declare a requested metric reads `NULL`. A declaration whose kind declares the metric but whose document recorded no value, such as one published before the kind declared it, fails the query with its snapshot, path, declaration, kind and metric, and asks to republish; it never reads as zero or `NULL`. Like the callable metrics, a metric-only query reads the stored values without decoding documents.

Metrics evolve additively. Publishing a kind again may declare new metrics, after which every new document of the kind must record them; removing a declared metric or changing its description is refused, naming the kind and the metric. To change a metric's meaning, declare a new name. Format 9 documents remain readable; only their producer metric fields are unavailable.

## Runnable domain publication

From the repository root, build the CLI and generate a publication containing HTTP → handler → topic → consumer links plus parallel Kubernetes relationships:

```sh
make build
mkdir -p .tmp
go run -tags hack ./hack/sql-domains > .tmp/sql-domains.json
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

For a server using the cookbook's Go database:

```sh
curl -fsS http://localhost:8080/api/v1/modules/query -H 'Content-Type: application/json' -d '{"args":["SELECT symbol.name AS caller FROM callers(func:Run) ORDER BY caller LIMIT 1"],"language":"sql","root":"example.org/service"}'
```

The relevant response fields below show why `total` differs from the returned row count: both `Entry` and `Replay` match, but the SQL limit admits one row. Column descriptors preserve the output name, bound field, and catalog type:

```json
{
  "columns": [{"name": "caller", "field": "symbol.name", "type": "string"}],
  "rows": [{"caller": "Entry"}],
  "total": 2
}
```

SQL CLI JSON and YAML retain this result envelope, including pre-limit totals and source witnesses. Human-readable output renders projected values, paths, or graphs. Compact CLI JSON retains its existing row format.

`GET /api/v1/modules/query-catalog` supplies functions, registered kinds, fields, and property schemas for completion. `GET /api/v1/modules/suggest-selectors?...&language=sql` completes registered selector prefixes. The Query page persists language in its URL, renders projected columns, paths, and graphs, and opens witnesses in Source. The Query page and Cmd+K share SQL completion and positioned diagnostics. Ctrl/Cmd+Space requests suggestions; Ctrl/Cmd+Enter runs the query.

Run the executable verification fixture with `gavel fixtures fixtures/sql-query.md`. It checks parser and query behavior, API integration, frontend models, and the runnable CLI publication.

## Errors and unsupported SQL

Syntax and binding errors report a message, source line/column when available, and a hint. Queries are bounded to 65,536 bytes, 64 nested parentheses, and 64 `NOT` tokens. Correct the query rather than switching database dialect: this grammar is the same on both storage backends. Missing required stored facts ask for reindexing or republishing; historical snapshots do not gain new capabilities in place. Partial/syntax coverage and missing heads remain evidence gaps, not proof that a symbol has no uses.

UIR SQL does not implement joins, SQL subqueries, CTEs, aggregation (`COUNT`, `SUM`, `GROUP BY`, `HAVING`), `DISTINCT`, `UNION`, `OFFSET`, arithmetic, scalar function calls, field-to-field comparisons, literal-list `IN`, `LIKE`, `<>`, or writes. Use nested navigation instead of joins, `MATCH`/`IMATCH` instead of `LIKE`, and `!=` instead of `<>`. A graph or path query cannot have row clauses. The [cookbook](sql-examples.md) includes valid alternatives.

The implementation sources are [`sqlsyntax/ast.go`](../query/sqlsyntax/ast.go) and [`sqlsyntax/parser.go`](../query/sqlsyntax/parser.go) for syntax, [`sql_bind.go`](../query/sql_bind.go) for functions/options/selectors, and [`sql_rows.go`](../query/sql_rows.go) and [`sql_files.go`](../query/sql_files.go) for fields and projection. `go test ./query -run TestQuery -ginkgo.focus='SQL documentation examples'` parses every SQL fence in this reference and the cookbook and checks that every registered source function has an example.
