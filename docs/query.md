# Querying indexed modules

`uir query` uses the PEG grammar in `query/grammar.peg` and a resolution pipeline over immutable module snapshots. It does not translate the expression into SQL. `nodes` and `unresolved calls` read the declarations and call occurrences of the documents a snapshot activates. The symbol operations (`references`, `definitions`, `implementations`, `callers`, `callees`, and `search`) resolve canonical symbols through the `symbols` table and read only the documents their `symbol_postings` select.

## CLI and scope

```sh
uir query 'nodes where method = "Run"' --root example.org/service
uir query 'references of node where type = "Store" and method = "Save"' --root example.org/service
uir query 'callers of node where type = "Store" and method = "Save" including dispatch' --root example.org/service --location ../service-feature
uir query 'search "Store.Sa"' --root example.org/service
uir query 'unresolved calls' --snapshot 6f8b2c6e-79af-4b59-8736-e45696f0c546 --format json
```

`--dsn` on the root command accepts a PostgreSQL DSN, `sqlite://` URL, or plain `.db` path. Without it, the CLI uses its configured local SQLite database. `--root` is the full `go.mod` module path; `--location` identifies a registered canonical checkout; `--snapshot` selects one historical published snapshot by UUID. Location and snapshot are mutually exclusive. A snapshot plus root must agree. `--limit` defaults to 100 and accepts 1–1000. Clicky's shared format flags select table, JSON, YAML, and other supported renderers.

Without an explicit location or snapshot, `nodes` and `unresolved calls` read the primary head of each selected root, and the symbol operations read the head of every registered checkout of each selected root. A selected historical snapshot never silently falls back to a newer head.

### Output envelope

`uir query` and `POST /api/v1/modules/query` return one envelope. JSON and YAML carry every field; table, CSV, and the other tabular renderers show `matches` (the CLI table also prints the other collections beneath it), and HTTP responses set `X-Total-Count` to `total`.

| Field | Content |
| --- | --- |
| `operation` | The parsed operation, e.g. `references` or `search`. |
| `total` | Rows found before `--limit` was applied. For `search` the scan stops once the limit is filled, so `total` is then a lower bound and the `search` stage says "scan stopped at the limit". |
| `matches` | Result rows, bounded by the limit. |
| `declarations` | The target's definitions, in the row shape; empty for operations without a single target. |
| `symbols` | The canonical symbols the selector resolved to: `id`, `module_key`, `package_path`, `kind`, `owner_id`, `owner`, `name`, `visibility`, `parameter_types`. Empty for `nodes`, `unresolved calls`, and `search`. |
| `coverage` | Packages in scope that are `partial`, `syntax`, or `excluded`: `root_key`, `location`, `snapshot_id`, `package_path`, `coverage`, and `diagnostics`, the number of recorded diagnostics for that package. |
| `stages` | Pipeline decisions as `name`/`value` pairs. |

Every row carries `kind`, `root`, `symbol` (the identifier's symbol key: the row's own declaration, or the declaration enclosing an occurrence), `location` (checkout), `source` (display `path:line:column`), and `snapshot_id`. Rows add, when set and omitted otherwise: `path`, `line`, `column`, `end_line`, `end_column`, `role`, `symbol_id`, `enclosing_id`, `enclosing_key`, `coverage`, and `dispatch` (`true` only for a caller reached through an interface).

```json
{
  "operation": "references",
  "total": 1,
  "matches": [{
    "kind": "reference", "root": "example.org/refs", "symbol": "method:example.org/refs/app:Run#()",
    "location": "/src/refs", "source": "app/app.go:5:28", "snapshot_id": "1ae9058d-daec-4f34-bd5b-21832ff480f7",
    "path": "app/app.go", "line": 5, "column": 28, "end_line": 5, "end_column": 32, "role": "call",
    "symbol_id": "f22a813c…", "enclosing_id": "d7e469f7…",
    "enclosing_key": "v1:[\"method\",\"\",\"example.org/refs/app\",\"\",\"Run\",\"\",\"()\"]", "coverage": "indexed"
  }],
  "declarations": [{
    "kind": "definition", "root": "example.org/refs", "symbol": "method:example.org/refs/store.Store:Save#()",
    "location": "/src/refs", "source": "store/store.go:5:14", "snapshot_id": "1ae9058d-daec-4f34-bd5b-21832ff480f7",
    "path": "store/store.go", "line": 5, "column": 14, "end_line": 5, "end_column": 18, "role": "definition",
    "symbol_id": "f22a813c…", "coverage": "indexed"
  }],
  "symbols": [{
    "id": "f22a813c…", "module_key": "example.org/refs", "package_path": "example.org/refs/store", "kind": "method",
    "owner_id": "6959d8b2…", "owner": "Store", "name": "Save", "visibility": "exported", "parameter_types": []
  }],
  "coverage": [{
    "root_key": "example.org/refs", "location": "/src/refs", "snapshot_id": "1ae9058d-daec-4f34-bd5b-21832ff480f7",
    "package_path": "example.org/refs/broken", "coverage": "partial", "diagnostics": 1
  }],
  "stages": [
    {"name": "parse", "value": "references"}, {"name": "scope", "value": "1 snapshots"},
    {"name": "coverage", "value": "incomplete: 1 package is not fully indexed (1 partial)"},
    {"name": "resolve", "value": "1 symbols"}, {"name": "execute", "value": "1 rows"}
  ]
}
```

## Grammar

```text
nodes [where predicate [and predicate ...]]
references of node where predicate [and predicate ...]
definitions of node where predicate [and predicate ...]
implementations of node where predicate [and predicate ...]
callers of node where predicate [and predicate ...] [including dispatch]
callees of node where predicate [and predicate ...]
search "prefix" [where root = "module/path"]
unresolved calls [where root = "module/path"]
predicate := field = "value"
```

Keywords and fields are lowercase; values use double-quoted Go-style strings. Comparisons are exact equality, not substring or regex matching. `root` must agree with `--root` when both are present. An incomplete expression, unknown field, unquoted value, `including dispatch` after anything but `callers`, or trailing input fails at parse time.

The two families accept different predicates, and a predicate the operation cannot evaluate is an error rather than a filter that matches nothing:

| Operations | Predicates |
| --- | --- |
| `nodes` | `node_type`, `module`, `package`, `type`, `method`, `field`, `signature`, `language`, `symbol_key`, `identity_key`, `root`, compared with each declaration's structured identifier |
| `references`, `definitions`, `implementations`, `callers`, `callees` | `symbol_id`, `module`, `package`, `type`, `method`, `field`, `kind`, `name`, `owner`, `root`, compared with columns of `symbols` |
| `search`, `unresolved calls` | `root` only |

A symbol selector maps onto `symbols` columns: `module` is `module_key`, `package` is `package_path`, `kind` is one of `package`, `type`, `func`, `method`, `field`, `var`, `const`, `builtin`, `name` is the symbol's name, `owner` is its owner's name, and `symbol_id` is the canonical id. `type` alone selects a type named that; with `method` or `field` it selects the owner. `method` without an owner matches functions and methods; with one, only methods. The selector must name a symbol through `symbol_id`, `name`, `type`, `method`, or `field`, and conflicting names (`method = "Run" and name = "Stop"`) are rejected.

```text
nodes where language = "go" and node_type = "method"
references of node where package = "example.org/service/invoices" and type = "Store" and method = "Save"
definitions of node where kind = "func" and name = "Run"
implementations of node where type = "Saver"
callers of node where type = "Store" and method = "Save" including dispatch
callees of node where package = "example.org/service/api" and method = "Run"
search "sav"
search "Store.Sa" where root = "example.org/service"
unresolved calls where root = "example.org/service"
```

## Resolution

`query.Pipeline.RunModules(ctx, expression, query.ModuleScopeOptions{RootKey: root, Location: location, SnapshotID: snapshot, Limit: limit})` parses the expression, reconciles the root selector, rejects predicates the operation does not support, and resolves snapshot scope. It returns `ModuleQueryResult`:

| Field | Meaning |
| --- | --- |
| `operation`, `stages` | The parsed operation and each successful pipeline decision: `parse`, `scope`, `coverage`, then `resolve`, `dispatch`, or `search` where they apply, and `execute`. |
| `matches` | Rows, bounded by the limit, in a stable order. |
| `total` | Rows before the limit. |
| `symbols` | The canonical symbols a selector resolved to, with owner name, visibility, and parameter types. |
| `declarations` | The target's definitions, kept apart from `matches` so occurrence rows are never multiplied by the number of declaration locations. |
| `coverage` | Every package in the selected snapshots that is `partial`, `syntax`, or `excluded`. |

A match carries kind, root, checkout, snapshot, module-relative path, the start (`line`, `column`) and end (`end_line`, `end_column`) of its range in one-based lines and UTF-16 columns, and a structured `uir.Identifier`. Symbol-operation rows also carry `role`, `symbol_id`, `enclosing_id` and `enclosing_key` for an occurrence, the `coverage` of the document the row was read from, and `dispatch` for a caller reached through an interface. A declaration's range is its name; an occurrence's is the identifier.

### Symbol operations

1. **Scope.** Each selected head or snapshot gets its active document set from `storage.ActiveDocuments` (effective sources, the snapshot's package input hashes, and a batched document lookup).
2. **Resolve.** The selector reads `symbols`, looking a name up through `search_name` and an owner through a subquery on the owner's name. No match is an error that states the scope's coverage. When the selector matches several symbols, only those with a posting in an active document stay; if more than one remains, the result is `candidate` rows, one per declaration in scope (or one unpositioned row for a symbol that is only referenced in scope), and no traversal happens. The same identity declared at two heads is one symbol with two declarations, not two candidates.
3. **Intersect.** Postings are read per root for `(symbol, role, root)` and intersected with the union of that root's active sets. The query counts the postings first: when they are no more than the active documents it range-scans them and probes the active set in memory; otherwise it probes the posting index with the active document ids in `IN` batches of 256. Symbol id lists are batched the same way. A posting whose document is active at several heads yields a row for each.
4. **Read.** Only the documents the intersection selected are decoded, once each, and validated against their source revisions.

| Operation | Postings | Rows |
| --- | --- | --- |
| `definitions` | `definition` | One `definition` row per declaration, from the document's symbol entry. |
| `references` | `reference` | One `reference` row per occurrence whose `symbol` is the target, except the declaring `definition` occurrence, named by its enclosing declaration (or by its package at file scope). `declarations` holds the target's definitions. |
| `implementations` | `implements` | One `implementation` row per declared type whose `implements` names the target, which must be a `type`. |
| `callers` | `reference` | One `caller` row per `call` occurrence of the target. With `including dispatch`, the target must be a method `T.M`: the `implements` entries of T's declarations in scope name interfaces I, and each I's method with M's name and parameter types joins the targets; its call rows carry `dispatch`. Interfaces outside T's import closure are not recorded, so dispatch through them is not proven. |
| `callees` | `definition` | One `callee` row per `call` occurrence whose `enclosing` is the target, resolved or not; the identifier is the call's target locator and `symbol_id` is empty for an unresolved call. |

Occurrence and declaration rows are ordered by root, checkout, path, line, column, kind, and symbol id.

### Search

`search "prefix"` normalizes the input as `search_name` is stored (lowercased, restricted to `[a-z0-9]`, so `save_v2` and `savev2` search alike) and range-scans `search_name >= prefix AND search_name < upper`. `upper` increments the prefix within the ordered alphabet `0-9` then `a-z` with carry: `9` becomes `a`, and a trailing `z` is dropped while the character before it increments (`fizz` scans below `fj`); a prefix of only `z` has no upper bound. Every scanned row is then kept only when its `search_name` starts with the prefix, so a column collation that orders the range differently from byte order can only make the scan read more rows, never miss or admit one. Builtins are excluded. The candidates keep those with a `definition` posting in an active document, ranked by kind (type, func, method, field, const, var), module, package, owner name, name, and id; each is returned once per declaration as a `symbol` row, and only the documents of rows within the limit are decoded. `Owner.Name` input splits on the dot: owners resolve by exact `search_name`, and their children are range-scanned by the name prefix (`Store.` lists every member of `Store`). More than one dot, or input with none of the alphabet, is an error.

### Coverage

Every result lists the packages in scope whose coverage is not `indexed` and states `complete` or `incomplete` in its `coverage` stage. A `partial` package records only proven facts, and a `syntax` package has no postings at all, so an empty or short symbol result over such a package is not proof of absence.

### Nodes and unresolved calls

`nodes` filters the explorable declarations of every active document by the predicates. `unresolved calls` lists call occurrences whose locator is unresolved or whose target identifier matches no declaration in scope; a signature-free locator may match a declaration with a signature. Call locators carry no target root, so identical identifiers across roots or checkouts can match across scopes. Both read typed and `syntax` documents alike, so packages that do not type-check keep their AST-derived answers.

The extractor type-checks each package with `go/packages` and `go/types`, applying the build variant recorded in the snapshot's configuration hash; a package that cannot be type-checked falls back to `syntax` coverage, whose documents hold the AST extractor's declarations and call locators. External dependencies are not indexed automatically.

The generated parser is checked in. After changing `query/grammar.peg`, run `make query-parser`; tests check parser behavior.
