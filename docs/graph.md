# Call graphs

`uir graph` draws the calls around one function or method of the indexed snapshots: the functions that call it, the functions it calls, or both. Each call carries its call sites, and each site the conditions that must hold to reach it. The graph is built by the `graph` package from the symbol index (`query/graph_source.go`); the conditions are read from the hash-verified source of the snapshot (`query/graph_guards.go`) and lowered to UIR by the `golang` package.

## Command

```sh
uir graph <selector> [--direction callees|callers|both] [--depth <n>] [--limit <n>] [--exclude <patterns>] [--root <module>] [--location <checkout>] [--snapshot <uuid>]
uir graph --symbol <id> [...]
```

| Option | Meaning |
| --- | --- |
| `<selector>`, `--selector` | A [compact symbol expression](query.md) that selects the root, such as `example.org/service/orders.Submit`, `orders.Submit`, or `func:Submit & pkg:example.org/service:orders`. It must select symbols: a relation (`<`, `>`, `<<`) or a path (`>>`) is rejected. The first argument and the flag are the same option. |
| `--symbol` | The canonical symbol id of the root, as a node's `id` or a candidate's `id` carries it. Give a selector or a symbol, never both. |
| `--direction` | `callees`, `callers`, or `both` (the default). |
| `--depth` | How many calls away from the root the graph reaches, from 1 through 8. The default is 2. |
| `--limit` | The most nodes the graph holds, from 1 through 1000. The default is 150. |
| `--exclude` | Comma-separated [package patterns](#excluding-packages) whose nodes the graph leaves out. The default is `std,builtin,gorm.io/...`; `none` excludes nothing. |
| `--root`, `--location`, `--snapshot` | The snapshots to read, as for [`uir query`](query.md). With none, the primary head of every indexed root. `--location` and `--snapshot` are mutually exclusive. |

The command is a Clicky operation: `--format json` and `--format yaml` print the envelope below, and the default format prints a tree. `uir serve` exposes it as `GET /api/v1/modules/graph` with the same options as query parameters (`selector`, `symbol`, `direction`, `depth`, `limit`, `exclude`, `root`, `location`, `snapshot`); `exclude` is one comma list, `exclude=std,gorm.io/...`.

```sh
uir graph example.org/service/orders.Submit --direction both --depth 2
```

```text
ƒ Submit example.org/service/orders orders.go:16
├── callers
│   ╰── ƒ Checkout orders.go:35
├── callees
│   ├── ƒ charge ×2 [order.Total > 0] orders.go:18
│   │   ╰── ƒ charge [order.Rush] orders.go:31 ↩ expanded elsewhere
│   ├── hook unresolved [hook != nil] orders.go:24
│   ├── ƒ Notifier.Notify orders.go:26
│   ╰── → dispatch ƒ Mail.Notify orders.go:26
╰── omitted: 1 unresolved
```

A line is one edge: the neighbour, its package when that differs from its parent's, `×N` when the edge has more than one call site, the guards and the `path:line` of the first site. `→ dispatch` marks a call that reaches the neighbour through an interface method. `unresolved` marks a target the index could not resolve. A node is expanded once; `↩ expanded elsewhere` marks every other edge that reaches a node which has neighbours of its own. The `omitted:` line summarises what was left out, and an `excluded:` line, printed when exclusion patterns left nodes out, tallies them per package, most first: `excluded: fmt 3, builtin 1`.

## Choosing the root

The root is one function or method. A selector that matches exactly one is drawn. A selector that matches several draws nothing: the response has empty `roots`, `nodes`, and `edges`, the matches in `candidates`, and a `resolve` stage that counts them. Narrow the selector, or pass a candidate's `id` as `--symbol`. A selector that matches symbols but no function or method (a type, a package) is an `invalid_query` error, and one that matches nothing is `symbol_not_found`.

## What the graph holds

| Field | Meaning |
| --- | --- |
| `roots` | The id of the root node. |
| `nodes` | One node per symbol: `id` (the canonical symbol id), `identifier`, `kind` (`func`, `method`, `builtin`, `unresolved`, `package`), `label` (the name within its package, `Store.Save`), `group` (the package path, or `builtin` for a predeclared function such as `len`), `depth` (0 for the root, negative for callers, positive for callees), `in` and `out`, and `location`. |
| `edges` | One edge per caller, callee, and type: `id` (`from|to|type`), `from`, `to`, `type` (`call` or `dispatch`), `sites`, and `properties`. |
| `groups` | The packages of the nodes, as `id` and `label`, for a renderer that clusters by package. |
| `omitted` | What the graph leaves out: `node_limit`, `beyond_depth`, `unresolved`, `unreadable_source`, and `excluded`. |
| `exclude` | The effective [exclusion patterns](#excluding-packages): the ones given, or the defaults. |
| `packages` | Every package the walk reached, drawn or excluded: `path`, `external`, `nodes`, and `excluded`. |
| `stages` | How the request was resolved: the scope, the index coverage, the root, and the size of the graph. |
| `warnings` | Registered checkouts skipped because they have no indexed head. |
| `candidates` | The functions and methods an ambiguous selector matched. |

A node's `location` is where the symbol is declared: `root_key`, `checkout_path`, `snapshot_id`, `source_id`, `path`, `line`, `column`, and `identity_key`. `source_id` and `identity_key` together are the id of the same declaration in `GET /api/v1/modules/browse` (`<source_id>:<identity_key>`). A symbol declared outside the selected snapshots, such as `fmt.Errorf`, has no location. A symbol declared more than once, such as a function with one declaration per build tag, is located at the first by path, and `properties.declarations` counts them.

`in` and `out` count a node's incoming and outgoing edges in the index, not only the edges drawn, so the difference from the edges drawn is how many more there are to expand. They are totals only in the direction the graph was walked: `out` for the root and its callees, `in` for the root and its callers. In the other direction they count the edges drawn. Edges to excluded nodes are not counted.

An edge's `sites` are its call sites in order of path, line, and column. A site has `path`, `line`, `column`, `text` (the whole call as written, arguments included, `channel.Send(order.Kind)`), and `guards`. A site in a file listed in `omitted.unreadable_source` keeps the callee as the index recorded it, `channel.Send`, because the call cannot be read back.

An edge's `properties` are string facts its source knows beyond the sites, such as the configurations that select a dispatch target; the Go index sets none, and the key is absent when there are none. When several steps of a source fold into one edge, the first non-empty properties are kept.

A `dispatch` edge runs from a caller to a method that implements the interface method the caller calls. The same site therefore appears on two edges: the `call` edge to the interface method, and a `dispatch` edge to each implementation in the selected snapshots.

A call the index could not resolve, such as a call of a function value, is an edge to an `unresolved` node. Its id names the caller, because two callers' unresolved calls of one name are not known to be one target. A call made outside any declaration, in a package-level variable initialiser, is an edge from a `package` node.

## Guards

A site's `guards` are the conditions that must hold for control to reach the call, outermost first, as plain text. A site with no guards is reached whenever its function runs.

| The call is in | Guard |
| --- | --- |
| an `if` body | the condition |
| an `else` block | the condition negated, `!(a && b)` |
| an `else if` body | the earlier conditions negated, then its own |
| a tagged `switch` case | `tag == value`, joined with `\|\|` for a case with several values |
| a tagless `switch` case | the case expression |
| a `default` clause | the negated disjunction of every other case |
| a case reached by `fallthrough` | its own condition or the condition of the case that falls into it |
| a type switch case | `x.(type) == T` |
| a `select` case | the communication as written, `value := <-ch`, or `default` |
| a `for` body or post statement | the loop condition |

A call in an `if` condition or init statement, a `switch` tag, a case's own value list, or a `for` init or condition is not guarded by that statement. A `for` with no condition and a `range` loop add no guard. A function literal keeps the guards around it. Negation is not simplified: the `else` of `if !ok` is `!!ok`.

Guards are syntax, not proof. They are the conditions of the statements written around the call, read without type information, so `err != nil` is the text of the condition and nothing is evaluated. An early `return` above a call is not a guard on it, and neither is the left operand of `&&` or `||` on a call in the right operand.

The source of a site is read as [`GET /api/v1/modules/content`](serve.md) reads it: the local file when its hash still matches the snapshot, else the stored blob, else the pinned Git blob. When none can be recovered, or the source does not parse, the file is listed in `omitted.unreadable_source`, a `guards` stage names the first failure, and the file's sites carry no guards and only the callee as their `text`. Their edges are still drawn.

## Excluding packages

Calls into the standard library, builtins, and database layers crowd out the calls a reader follows, so by default the graph leaves them out. `--exclude` (`exclude` over HTTP) takes a comma list of patterns, and the list given replaces the defaults:

| Pattern | Matches |
| --- | --- |
| `std` | the standard library |
| `builtin` | Go's predeclared functions, `len`, `append`, `panic`, whose group is `builtin` |
| `external` | every package outside the modules in the query scope: the standard library, builtins, and dependencies |
| `example.org/lib` | that package only |
| `example.org/lib/...` | that package and every package below it, as a Go package pattern |
| `none` | nothing; it must be the only pattern |

With no patterns the defaults `std,builtin,gorm.io/...` apply. An excluded node is not drawn, walked, or counted in `in` and `out`, and `omitted.excluded` counts the distinct excluded nodes per package. A node that only a node at the depth bound reaches is not counted there, nor in `beyond_depth`. The root is never excluded, even when its package is: its package's other nodes are.

`packages` lists every package the walk reached, drawn or excluded, ordered by `nodes` (drawn and excluded nodes together), most first, then by path. `external` marks a package outside the modules in the query scope, which the `external` pattern matches, and `excluded` a package the effective patterns match. A viewer builds its package filter from this list, so an excluded package can be switched back on.

A pattern that is empty, repeated, a glob such as `gorm.io/*`, a `...` anywhere but a trailing `/...`, or `none` beside another pattern is an `invalid_query` error.

## Limits

The graph is breadth-first from the root, to `depth` calls and `limit` nodes.

- `omitted.beyond_depth` counts the nodes at the depth bound that have neighbours further out. Raise `--depth`, or draw the graph of one of them.
- `omitted.node_limit` is true when nodes were left out because the limit was reached. Nodes nearer the root are kept first.
- `omitted.unresolved` counts the call sites on edges to unresolved nodes.
- A node at the depth bound is still asked for its neighbours, so its edges to nodes already in the graph are drawn, and recursion and calls back towards the root are visible.
- Callees are walked from the root and its callees, callers from the root and its callers. A caller's other callees and a callee's other callers are not part of the graph.

Only the selected snapshots contribute edges. Callers in a module that is not indexed are absent, and dispatch reaches only the implementations in the selected snapshots. The `stages` entry named `coverage` reports packages that are partially indexed; a call graph over them is incomplete in the same way a [query](query.md#results-and-limits) is.

`uir graph <symbol> --direction callers --depth N` and `uir query '<symbol> <<N'` walk the same caller edges. The query returns rows with a depth and fails beyond 10000 symbols; the graph returns nodes, edges, and guards, and truncates at `--limit`.

## Custom sources

The `graph` package knows nothing about Go. A frontend for another language, or for business rules held outside source files, draws the same graph by implementing `graph.Source`:

| Method | Returns |
| --- | --- |
| `Describe(ctx, id)` | The node for an id; an id the source does not know is an error. |
| `Out(ctx, id)` | One `graph.Step` per call or data access the node makes: the neighbour `Node`, and an `Edge` with its `Type` (`call`, `dispatch`, `read` or `write`), `Kind`, `Sites` and `Properties`. |
| `In(ctx, id)` | One step per call made to the node, in the same shape. |

`graph.Build(ctx, source, roots, graph.Options{Direction, Depth, Limit, Exclude, Access, Theme})` walks it and returns the `graph.Graph` envelope described above; `Build` sets each edge's `ID`, `From` and `To`, merges steps to one neighbour of one type, and bounds, orders and counts the result. A node with `Unresolved` set is a leaf that is never asked for its neighbours. Nothing in `Options` is defaulted.

An edge's `type` is not limited to calls: a source may report `read` and `write` edges to the data a body accesses. `Options.Access` lists the types `Build` follows (`graph.Follows`): a step of any other type is not drawn, counted in `in` and `out`, or walked, `call` also follows `dispatch`, and an empty list follows every type. `graph.ParseAccess` reads the `call`, `read` and `write` values a request names, comma-separated and in any case, into that list, and returns an empty list, every type, when they name none. `Build` refuses an entry that is not a `uir.RelationshipType`. `graph.FromRelationships` serves `call`, `dispatch`, `read` and `write` relationships, so `Access` chooses among them.

### Helpers for a custom source

| Helper | Does |
| --- | --- |
| `graph.FromResolver(resolver)` | Serves a `graph.Resolver` (`Describe`, `References`, `Referrers`) whose edges are references made by name. A reference's `Resolution` draws a `call` to the one target of a `static` name, a `dispatch` to each implementation of a `dynamic` name (its `Properties` naming what selects it) and to each candidate of an `ambiguous` one (with `status: ambiguous`), and a `call` to the `unresolved:<kind>:<name>` leaf (`graph.UnresolvedID`) of an `unresolved` one. `Reference.Steps` and `Reference.EdgeTo` are the same mapping for a source that lists references itself. |
| `graph.Absorb(source, absorbedInto)` | Folds structural nodes into their parent. Among callees, a step to an absorbed node is replaced in place by that node's own callees, each site drawn once; among callers, an absorbed node is drawn as the node its parent id describes. Absorbed nodes whose callees absorb one another are a cycle, and an error. |
| `graph.FoldMembers(steps, graph.Folding{MemberOf, Owner, Fold})` | Folds the data steps of one body into one per node and type. Folding (the `columns` toggle off) draws a member, such as a column or field, as its owner, the edge listing the members in its `Member.Property`; drawn apart, an owner's site that a covering member stands for on the same line is left out. Each returned `Folded` names the input steps it stands for, and members of one owner listed under different properties are an error. `graph.Surviving` names the steps drawn apart before any is folded, for a source that names a node only once it is drawn. |
| `graph.FillGuards(graph, guards)` | Sets every positioned site's guards from a function of the edge and site; a site whose source is unreadable (`graph.ErrUnreadableSource`) keeps the guards its source gave it and is listed in `omitted.unreadable_source`. |
| `graph.ExcludeKindsGroups(kinds, groups)` | An `Options.Exclude` matching node kinds and groups case-insensitively and whole, with `*` wildcards, comma-separated lists and `!` negations as `collections.MatchItems` reads them. |
| `graph.DataID`, `graph.SplitDataID`, `graph.SplitMember`, `graph.IsDataKind` | Data node ids: `table:`, `column:`, `procedure:`, `sqlfunction:`, `entity:` and `field:` followed by the name, which clicky-ui's `CallGraph` reads to tell data from code. A column's table is before its last dot, a field's entity before its first. |
| `graph.Filter` | The flags and query parameters every call-graph route spells alike (`direction`, `depth`, `limit`, `access`, `columns`), as clicky-ui's `CallGraphFetchParams` sends them; `Filter.Options()` turns them into `Options`. The exclusion and guard flags name a frontend's own kinds, groups and conditions, so a route declares them with its own help. |
| `graph.Cache` (`Load`), `graph.Params`, `graph.Cached` | Store built graphs under the canonical encoding of their request and inputs (`Params.Encode`, keyed by `graph.ParamsKey`); `Cached` serves a stored result or builds and stores one. |

`Graph.Pretty()` draws the result as the tree shown under [Command](#command). A node's name is its label, after the icon and in the colour of its `Kind` when that is a UIR node type; `Graph.Tree(graph.TreeOptions{Name: ...})` draws it with a renderer of the frontend's own, which is how `uir graph` adds the Go symbol icons.

## Errors

Over HTTP an invalid request returns 400 with `code: "invalid_query"`, a `message`, and a `hint`: a depth, limit, or direction out of range, an exclusion pattern it cannot match, neither or both of `selector` and `symbol`, a selector that does not parse or selects no function or method, or a symbol id that names something else. A selector or symbol id that is not in the selected index returns 404 with `code: "symbol_not_found"`.
