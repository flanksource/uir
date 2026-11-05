# SQL examples

These examples cover all 15 UIR SQL source functions. The [language reference](sql-query.md) defines selectors, fields, predicates, options, limits, and error behavior. Each SQL block is one complete query; paste it into the browser Query page or pass it as the quoted expression to `uir query`.

## Prepare the Go example

From the repository root, build the CLI and index the included [service module](examples/sql-go/service.go) into an isolated SQLite database:

```sh
make build
mkdir -p .tmp
.bin/uir --dsn .tmp/sql-language-go.db add docs/examples/sql-go
.bin/uir --dsn .tmp/sql-language-go.db query 'SELECT symbol.name FROM func:*' --root example.org/service
```

For the Go queries below, use that DSN and `--root example.org/service`. The module declares `Order.Total`, a `Repository` interface, `Store.Save`, and an `AuditedStore` embedding `Store`. Its call chain is `Entry → Run → Store.Save`, with `Replay → Run` as a second entry. `Save` reads `Order.Total` and writes `LastTotal`; `Load` reads `LastTotal`. `Process` calls `Save` through the `Repository` interface.

## Select declarations and definitions

Select concrete methods of `Store`; source positions describe their declarations:

```sql
SELECT symbol.name, symbol.kind, path, line
FROM method:Store.*
ORDER BY symbol.name
```

Find the declaration of the save method explicitly:

```sql
SELECT symbol.query_name, path, line, column
FROM definitions(method:Store.Save)
```

Select declarations belonging to one module's root package, using the contextual package selector:

```sql
SELECT symbol.name, symbol.kind
FROM pkg:example.org/service:.
WHERE symbol.visibility = 'exported'
ORDER BY symbol.kind, symbol.name
```

## Find callers and callees

List direct callers of `Run`: `Entry` and `Replay`. The projected symbol names are callers; `path` and `line` identify their call sites.

```sql
SELECT symbol.name AS caller, path, line, role
FROM callers(func:Run)
ORDER BY caller
```

Follow `Entry`'s call chain for up to two hops, retaining the site and distance of each hop:

```sql
SELECT symbol.name AS callee, path, line, depth
FROM callees(func:Entry, depth => 2)
ORDER BY depth, callee
```

Find callers of the functions that `Entry` calls. Nested navigation passes canonical identities, so this again finds callers of `Run`:

```sql
SELECT symbol.name AS caller, path, line
FROM callers(callees(func:Entry))
ORDER BY caller
```

Keep callers in one package while excluding methods by canonical identity:

```sql
SELECT symbol.name, path, line
FROM callers(func:Run)
WHERE symbol IN pkg:example.org/service:. AND symbol NOT IN method:Store.*
ORDER BY symbol.name
```

Inspect known interface implementation targets reached from `Process`, using multi-hop callee traversal and a Boolean predicate. A direct depth-one callee query retains the declared interface method instead:

```sql
SELECT symbol.owner, symbol.name, dispatch, path, line
FROM callees(func:Process, depth => 2)
WHERE dispatch = TRUE
ORDER BY symbol.owner
```

## Inspect references, reads, and writes

Find all non-definition uses of a field, including their recorded roles. Here `symbol.name` is the referenced field, rather than the enclosing function:

```sql
SELECT symbol.name, path, line, role
FROM references(field:Order.Total)
ORDER BY path, line
```

Find reads of the package variable in `Load`:

```sql
SELECT symbol.name, path, line, role
FROM reads(var:LastTotal)
```

Find writes of the same variable in `Store.Save`:

```sql
SELECT symbol.name, path, line, role
FROM writes(var:LastTotal)
```

These functions search **uses of their argument**, not the uses made by its body. For outgoing resolved body uses in the compact language, use `func:Run :uses`; SQL has no `uses()` function. Direct reference selectors can also find imported identities such as `references(func:fmt.Println)` when their declaration is outside the indexed module.

## Navigate types

List owned methods of `Store`:

```sql
SELECT symbol.query_name, path, line
FROM methods(type:Store)
```

Find the recorded types that implement `Repository`, including pointer implementation facts:

```sql
SELECT symbol.name, path, line
FROM implementers(type:Repository)
ORDER BY symbol.name
```

Find direct embedding declarations, such as `AuditedStore` embedding `Store`:

```sql
SELECT symbol.name, path, line
FROM inheritors(type:Store)
```

## Traverse, find a path, and draw a graph

Traverse up to three outgoing call hops. Filtering for `Save` happens after traversal, so `Run` is still followed even though it does not match the predicate:

```sql
SELECT symbol.name, depth
FROM traverse(func:Entry, direction => 'outgoing', depth => 3)
WHERE symbol.name = 'Save'
ORDER BY depth
```

Return one shortest call path from `Entry` to `Store.Save`. Its `path.symbols` are `Entry`, `Run`, and `Save`, with call-site evidence between them:

```sql
SELECT * FROM path(func:Entry, method:Store.Save, depth => 4)
```

Draw outgoing calls, bounded by hop depth and node count, with package exclusions disabled:

```sql
SELECT * FROM graph(func:Entry, direction => 'outgoing', depth => 3, nodes => 30, exclude => 'none')
```

Draw read/write access from `Store.Save` instead of calls:

```sql
SELECT * FROM graph(method:Store.Save, access => 'read,write', direction => 'outgoing', depth => 1)
```

`path()` and `graph()` return structured results. They cannot have `WHERE`, `ORDER BY`, or SQL `LIMIT`. For JSON output, run the query with `--format json` and inspect `path` or `graph`, including graph omissions.

## Inspect files, callable metrics, and variable types

Count functions and structs per indexed file, including files with no matching declaration:

```sql
SELECT f.path, f.lines, f.definitions, f.functions, f.structs
FROM files() AS f
ORDER BY f.lines DESC, f.path
```

Review callables with parameters and bodies, using a projection alias for ordering:

```sql
SELECT symbol.name, symbol.metrics.param_count AS parameters, symbol.metrics.body_lines AS body_lines
FROM func:*
WHERE symbol.metrics.param_count >= 1 AND symbol.metrics.body_lines IS NOT NULL
ORDER BY body_lines DESC, symbol.name
```

Read the proven package-variable type:

```sql
SELECT symbol.name, symbol.type
FROM var:LastTotal
WHERE symbol.type = 'int'
```

Find interface signatures with no body. `IS NULL` selects a known bodyless declaration; it does not turn unavailable metrics into a missing value:

```sql
SELECT symbol.name, symbol.metrics.param_count, symbol.metrics.body_lines
FROM methods(type:Repository)
WHERE symbol.metrics.body_lines IS NULL
```

Combine anchored name globs and explicit negation. Parentheses group the alternatives before the visibility condition:

```sql
SELECT symbol.name, symbol.kind
FROM func:*
WHERE (symbol.name MATCH 'S*' OR symbol.name MATCH 'R*')
  AND NOT (symbol.visibility = 'internal')
  AND symbol.name NOT IMATCH '*play'
ORDER BY symbol.name
```

Inspect explicit modern build headers, including excluded snapshot members. The sample file has no header, so its constraint is the empty string and it does not require `hack`:

```sql
SELECT path, file.build_constraint
FROM files(include_excluded => TRUE)
WHERE file.build_constraint NOT REQUIRES 'hack'
ORDER BY path
```

## Prepare the domain example

The [domain producer](../hack/sql-domains/main.go) supplies an HTTP route, handler, topic, consumer, and Kubernetes objects with explicit native links. From the repository root:

```sh
mkdir -p .tmp
go run -tags hack ./hack/sql-domains > .tmp/sql-language-domains.json
.bin/uir --dsn .tmp/sql-language-domains.db import .tmp/sql-language-domains.json
.bin/uir --dsn .tmp/sql-language-domains.db query "SELECT symbol.name FROM http.route:*" --root example.org/orders
```

For the remaining queries, use `.tmp/sql-language-domains.db` and `--root example.org/orders`. The publication links `Orders → Handle → OrderCreated → Consume`, and records both `k8s.selects` and `k8s.uses_config` between `checkout` and `pod`. These are producer facts; UIR does not infer them from similar names.

## Filter typed properties and missing values

Find the POST route with a missing optional description. A source alias abbreviates the route's declared properties:

```sql
SELECT symbol.name, r.method, r.path, r.description
FROM http.route:* AS r
WHERE r.method = 'POST' AND r.path IMATCH '/ORDERS/**' AND r.description IS NULL
```

The result is `Orders`, `POST`, `/orders/new`, and `NULL`. A filter using `r.description = NULL` would return no rows because the comparison is unknown.

## Inspect native incoming and outgoing witnesses

Find publishers of `OrderCreated`; `symbol.name` projects the source, `Handle`:

```sql
SELECT symbol.name, relationship.kind, relationship.resolution, path, line
FROM incoming(messaging.topic:OrderCreated, via => 'messaging.publishes_to')
```

Find what handles `Orders`; `symbol.name` projects the target, `Handle`, while `path` remains the route's evidence file:

```sql
SELECT symbol.name, symbol.package, package, relationship.kind, path
FROM outgoing(http.route:Orders, via => 'http.handled_by')
```

Traverse the native chain across domains, with minimum hop distances `Handle: 1`, `OrderCreated: 2`, and `Consume: 3`:

```sql
SELECT symbol.name, depth
FROM traverse(http.route:Orders, via => 'http.handled_by,messaging.publishes_to,messaging.delivers_to', depth => 3)
ORDER BY depth
```

## Follow native paths and parallel edges

Return the complete HTTP-to-consumer path:

```sql
SELECT * FROM path(http.route:Orders, messaging.consumer:Consume, via => 'http.handled_by,messaging.publishes_to,messaging.delivers_to')
```

Keep both native relationship kinds connecting the same two Kubernetes objects:

```sql
SELECT * FROM graph(k8s.service:checkout, via => 'k8s.selects,k8s.uses_config', direction => 'outgoing', depth => 2, nodes => 20)
```

This graph has two nodes and two distinct edges. `via` names exact registered kinds; an unregistered name fails, and it cannot combine with `access`.

## Run and verify

The reference's [producer metrics](sql-query.md#producer-metrics) section adds queries for custom integer measures once a producer declares and publishes them. The [lint guide](lint.md) shows how to turn row queries into architecture rules; lint queries reject SQL `LIMIT` and terminal graph/path results so violations cannot be truncated.

The documentation syntax check parses every `sql` block here and in the reference and requires an example of every registered source function:

```sh
go test ./query -run TestQuery -ginkgo.focus='SQL documentation examples' -ginkgo.no-color
```

For execution coverage of predicates, properties, source witnesses, graphs, and API envelopes, use the focused SQL tests in [the SQL fixture](../fixtures/sql-query.md). Its full-suite gate requires the repository's PostgreSQL test environment as well as SQLite.
