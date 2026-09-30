# Call graphs

`uir graph` draws the calls around one function or method of the indexed snapshots: the functions that call it, the functions it calls, or both. Each call carries its call sites, and each site the conditions that must hold to reach it. The graph is built by the `graph` package from the symbol index (`query/graph_source.go`); the conditions are read from the hash-verified source of the snapshot (`query/graph_guards.go`) and lowered to UIR by the `golang` package.

## Command

```sh
uir graph <selector> [--direction callees|callers|both] [--depth <n>] [--limit <n>] [--root <module>] [--location <checkout>] [--snapshot <uuid>]
uir graph --symbol <id> [...]
```

| Option | Meaning |
| --- | --- |
| `<selector>`, `--selector` | A [compact symbol expression](query.md) that selects the root, such as `example.org/service/orders.Submit`, `orders.Submit`, or `func:Submit & pkg:example.org/service:orders`. It must select symbols: a relation (`<`, `>`, `<<`) or a path (`>>`) is rejected. The first argument and the flag are the same option. |
| `--symbol` | The canonical symbol id of the root, as a node's `id` or a candidate's `id` carries it. Give a selector or a symbol, never both. |
| `--direction` | `callees`, `callers`, or `both` (the default). |
| `--depth` | How many calls away from the root the graph reaches, from 1 through 8. The default is 2. |
| `--limit` | The most nodes the graph holds, from 1 through 1000. The default is 150. |
| `--root`, `--location`, `--snapshot` | The snapshots to read, as for [`uir query`](query.md). With none, the primary head of every indexed root. `--location` and `--snapshot` are mutually exclusive. |

The command is a Clicky operation: `--format json` and `--format yaml` print the envelope below, and the default format prints a tree. `uir serve` exposes it as `GET /api/v1/modules/graph` with the same options as query parameters (`selector`, `symbol`, `direction`, `depth`, `limit`, `root`, `location`, `snapshot`).

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

A line is one edge: the neighbour, its package when that differs from its parent's, `×N` when the edge has more than one call site, the guards and the `path:line` of the first site. `→ dispatch` marks a call that reaches the neighbour through an interface method. `unresolved` marks a target the index could not resolve. A node is expanded once; `↩ expanded elsewhere` marks every other edge that reaches a node which has neighbours of its own. The last line summarises what was left out.

## Choosing the root

The root is one function or method. A selector that matches exactly one is drawn. A selector that matches several draws nothing: the response has empty `roots`, `nodes`, and `edges`, the matches in `candidates`, and a `resolve` stage that counts them. Narrow the selector, or pass a candidate's `id` as `--symbol`. A selector that matches symbols but no function or method (a type, a package) is an `invalid_query` error, and one that matches nothing is `symbol_not_found`.

## What the graph holds

| Field | Meaning |
| --- | --- |
| `roots` | The id of the root node. |
| `nodes` | One node per symbol: `id` (the canonical symbol id), `identifier`, `kind` (`func`, `method`, `builtin`, `unresolved`, `package`), `label` (the name within its package, `Store.Save`), `group` (the package path), `depth` (0 for the root, negative for callers, positive for callees), `in` and `out`, and `location`. |
| `edges` | One edge per caller, callee, and type: `id` (`from|to|type`), `from`, `to`, `type` (`call` or `dispatch`), and `sites`. |
| `groups` | The packages of the nodes, as `id` and `label`, for a renderer that clusters by package. |
| `omitted` | What the graph leaves out: `node_limit`, `beyond_depth`, `unresolved`, and `unreadable_source`. |
| `stages` | How the request was resolved: the scope, the index coverage, the root, and the size of the graph. |
| `warnings` | Registered checkouts skipped because they have no indexed head. |
| `candidates` | The functions and methods an ambiguous selector matched. |

A node's `location` is where the symbol is declared: `root_key`, `checkout_path`, `snapshot_id`, `source_id`, `path`, `line`, `column`, and `identity_key`. `source_id` and `identity_key` together are the id of the same declaration in `GET /api/v1/modules/browse` (`<source_id>:<identity_key>`). A symbol declared outside the selected snapshots, such as `fmt.Errorf`, has no location. A symbol declared more than once, such as a function with one declaration per build tag, is located at the first by path, and `properties.declarations` counts them.

`in` and `out` count a node's incoming and outgoing edges in the index, not only the edges drawn, so the difference from the edges drawn is how many more there are to expand. They are totals only in the direction the graph was walked: `out` for the root and its callees, `in` for the root and its callers. In the other direction they count the edges drawn.

An edge's `sites` are its call sites in order of path, line, and column. A site has `path`, `line`, `column`, `text` (the callee as written, `channel.Send`), and `guards`.

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

The source of a site is read as [`GET /api/v1/modules/content`](serve.md) reads it: the local file when its hash still matches the snapshot, else the stored blob, else the pinned Git blob. When none can be recovered, or the source does not parse, the file is listed in `omitted.unreadable_source`, a `guards` stage names the first failure, and the file's sites carry no guards. Their edges are still drawn.

## Limits

The graph is breadth-first from the root, to `depth` calls and `limit` nodes.

- `omitted.beyond_depth` counts the nodes at the depth bound that have neighbours further out. Raise `--depth`, or draw the graph of one of them.
- `omitted.node_limit` is true when nodes were left out because the limit was reached. Nodes nearer the root are kept first.
- `omitted.unresolved` counts the call sites on edges to unresolved nodes.
- A node at the depth bound is still asked for its neighbours, so its edges to nodes already in the graph are drawn, and recursion and calls back towards the root are visible.
- Callees are walked from the root and its callees, callers from the root and its callers. A caller's other callees and a callee's other callers are not part of the graph.

Only the selected snapshots contribute edges. Callers in a module that is not indexed are absent, and dispatch reaches only the implementations in the selected snapshots. The `stages` entry named `coverage` reports packages that are partially indexed; a call graph over them is incomplete in the same way a [query](query.md#results-and-limits) is.

`uir graph <symbol> --direction callers --depth N` and `uir query '<symbol> <<N'` walk the same caller edges. The query returns rows with a depth and fails beyond 10000 symbols; the graph returns nodes, edges, and guards, and truncates at `--limit`.

## Errors

Over HTTP an invalid request returns 400 with `code: "invalid_query"`, a `message`, and a `hint`: a depth, limit, or direction out of range, neither or both of `selector` and `symbol`, a selector that does not parse or selects no function or method, or a symbol id that names something else. A selector or symbol id that is not in the selected index returns 404 with `code: "symbol_not_found"`.
