# Querying indexed modules

`uir query` reads the snapshot-aware symbol index using a compact Go symbol expression. It resolves canonical symbols and traverses recorded declarations, occurrences, implementations, and calls. Queries run against the primary head of each selected registered root unless `--location` or `--snapshot` selects another indexed view. The parser is generated from `query/grammar.peg` with `make query-parser`.

## Quick start

```sh
uir query 'store.Store.Save' --root example.org/service
uir query 'store.Store.Save <' --root example.org/service
uir query 'store.Store.Save <' --root example.org/service --group-by=file,module
uir query 'store.Store.Save < -pkg store' --root example.org/service
uir query 'store.Store.Save < -pkg example.org/service/...' --root example.org/service
uir query 'example.org/service/store.Store.Save =' --root example.org/service
uir query 'example.org/service/store.Save*' --root example.org/service
uir query 'api.* >> store.Store.Save' --root example.org/service --format json
uir query 'pkg:example.org/service:store & struct:Store' --root example.org/service
uir query 'func:Store.Save < pkg:example.org/service:api/**' --root example.org/service
uir query 'func:* +pkg:example.org/service:api -func:Test*' --root example.org/service
uir query --methods --calls --include 'example.org/service/api/**' --root example.org/service
uir query 'store.Store.Save' --callers --exclude 'example.org/service/internal/**'
uir query --types --inherits --root example.org/service
uir query --modules --packages --format json
```

An expression is optional when a structured flag is present. `--methods`, `--vars`, `--types`, `--modules`, and `--packages` select node kinds; combining kinds forms a union. `--callers`, `--calls`, `--implements`, and `--inherits` traverse incoming calls, outgoing calls, interface implementations, and direct struct or interface embedding respectively. Multiple relation flags union their witness rows. With no kind or expression, a relation starts from its compatible symbol kind (`func:*` for calls and `type:*` for type relations). Repeatable `--include` and `--exclude` accept full module, package, or qualified symbol path globs; includes form a union, and exclusions win. These flags compile into the same expression tree as the text grammar before execution. For example, `--methods --calls` is `method:* >`, and `--types --inherits` is `type:* :inherits`.

`--root` is a registered module path. `--location` selects a registered checkout and `--snapshot` selects a historical published snapshot; they cannot be combined. `--limit` defaults to 100 and accepts 1–1000. The root command's `--dsn` selects the UIR database. Without an explicit scope, queries use available primary heads and warn about registered checkouts that have no head; an empty result is inconclusive while any head is missing. An explicitly selected root or checkout without a head still fails with "… is registered but not indexed; run `uir reindex` on its checkout". The HTTP equivalent is `POST /api/v1/modules/query` with optional `args: [expression]`, the same structured flag names as JSON fields, and optional `root`, `location`, `snapshot`, and `limit` fields.

Human-readable results use a colored symbol tree. `--group-by` selects the tree levels and their order from `module`, `package`, and `file`; the default is `module,package,file`. The root package is folded into its module, and a definition on the declaration line appears only on the symbol node. Other matching source positions remain as leaves. Call-path results follow hop order. CLI JSON/YAML and the HTTP JSON response retain the full query envelope.

## Grammar

```text
expression  := chain ("&" chain | "|" chain)* | expression ">>" [depth] expression
chain       := primary modifier* (relation filter* primary? | relation filter*)*
primary     := symbol | selector | "(" expression ")"
selector    := "pkg:" package-glob | "pkg:" module-glob ":" relative-package-glob
             | "mod:" module-glob | "func:" name-glob | "field:" name-glob | "struct:" name-glob
             | ("all:" | "method:" | "var:" | "type:" | "module:" | "package:" | "path:") glob
modifier    := ("+" | "-") selector
symbol      := qualified-name | symbol-glob
relation    := "<" | ">" | "=" | "<<" [depth] | ":impl" | ":inherits" | ":methods" | "~w"
filter      := "-f" file-suffix | "+pkg" package | "-pkg" package | "~w"
depth       := 1..8
```

Parentheses group sets and path endpoints. `&` binds more tightly than `|`; `>>` joins two complete endpoint expressions. Both `<<` and `>>` are bounded, with defaults of 3 and 8 hops respectively. Invalid or incomplete expressions fail at the parser. The symbol is a Go qualified name, never a predicate or a bound variable.

Typed selectors return sets of active declared canonical symbols. `pkg:` and `mod:` select symbols declared in packages or registered module roots; they do not create package or module result entities. `module:` and `package:` select the actual indexed tree nodes. `all:` selects all declared symbols, `method:` selects methods, `var:` variables, and `type:` named types. `func:` includes functions and methods, `field:` includes struct fields, and `struct:` includes defined non-alias types with a struct underlying type in the selected snapshot. Short typed names may select several symbols without an ambiguity error. An exact typed selector with no active match returns an empty result.

Selector globs are case sensitive and anchored to the whole key. `*` matches within one slash-delimited segment, `?` matches one character within it, and `**` must occupy a whole segment and can match zero or more segments. A trailing `/...` is shorthand for `/**` in any typed selector, including the module part of a two-part `pkg:` selector. Periods and literal `@`, `#`, `$`, and `!` have no special meaning; escape operator characters with `\`. A colon remains a selector separator. One-part `pkg:example.org/service/store` matches a full import path. Two-part `pkg:example.org/service:store` matches the `store` package relative to that registered module root; `pkg:example.org/*:internal/**` searches matching module roots without crossing a nested module boundary. Use `.` only as the complete relative pattern for a module's root package and `**` for the root plus descendants. For `func:`, `field:`, and `struct:`, a pattern with no dot or slash matches a name; a dot selects the owner-qualified name; a slash selects the complete `query_name`.

Postfix `+kind:pattern` keeps symbols and `-kind:pattern` removes them. Multiple positive modifiers of one kind are ORed, multiple negatives are subtracted, and different kinds are ANDed. For example, `func:* +pkg:example.org/service:api +pkg:example.org/service:store -func:Test*` keeps functions from either package except matching test names. Parenthesize a unary relation before filtering its projected result: `(func:Save <) +pkg:example.org/service:api`. A leading exclusion is invalid.

| Expression | Result |
| --- | --- |
| `sub.Thing.Do` | Resolve a symbol and show its declaration. |
| `sub.Thing.Do <` | Incoming call sites, including known interface dispatch for methods. For other symbols, incoming references. |
| `sub.Thing.Do >` | Call sites within that function or method. |
| `sub.Thing.Do =` | Definition locations. |
| `sub.Thing.Do <<3` | Callers up to three edges away, with a depth on each row. |
| `sub.Thing.Field ~w` | Write references. `sub.Thing.Field < ~w` is equivalent. |
| `sub.Iface :impl` | Types indexed as implementing the interface. |
| `sub.Base :inherits` | Types that directly embed the named struct or interface. Older snapshots without embedding facts require reindexing. |
| `sub.Thing :methods` | Methods owned by the type. |
| `sub.Thing.Do < -f _test.go` | Incoming rows excluding paths with that suffix. |
| `sub.Thing.Do < +pkg api` | Incoming rows inside a package named `api`; a full import path selects exactly that package. |
| `sub.Thing.Do < -pkg sub` | Incoming rows outside package `sub`; use the full import path when several packages share the name. |
| `sub.Thing.Do < -pkg example.org/mod/...` | Exclude callers in the module root package and every subpackage. `+pkg` accepts the same pattern. |
| `os.Exec.Command < & io.ReadAll >` | Symbols shared by the two projected result sets. |
| `main.* >> sub.Thing.Do` | Shortest recorded call chain, if one exists. |
| `func:Save < pkg:example.org/service:api` | Incoming call sites whose enclosing caller is declared in that package. |
| `func:Run > func:Store.Save` | Outgoing call sites whose resolved callee is that method. |
| `struct:Store :methods func:Save` | Method witnesses of matching struct types, narrowed to `Save`. |

A name with `/` is matched against its full import path; without `/`, the package portion is suffix matched. Bare symbol globs are case sensitive and anchored: `*` matches within one slash-delimited segment, `?` matches one character, and `**` matches across whole slash-delimited segments. The existing `.*` form selects direct children of the named package or owner. Other globs expand to all matching active canonical symbols, including variables, without an ambiguity prompt. An exact spelling that resolves to several canonical symbols returns positioned candidates with their full `query_name`; use a full path to disambiguate. `&` and `|` compare canonical symbol identities after each side's location rows are projected to symbols. Chaining a relation traverses the projected symbols from the previous step.

Package filters without `/...` match one package. A filter ending in `/...` matches that full Go import path and every descendant at a `/` boundary. Use the full `go.mod` module path followed by `/...` to cover a whole module. Omit `--root` to search every registered primary root; the filter then removes callers from the selected package or module while retaining indexed consumers in other roots.

Binary relations keep the unary relation's witness rows and retain only rows whose projected canonical symbol belongs to the right operand in the same selected snapshot. `<` projects the enclosing caller or referrer, `>` the resolved callee, `<<N` the caller after traversal, `:impl` the implementing type, `:methods` the owned method, and `~w` the enclosing writer. `=` has only a unary form. Use parentheses to make a set the right operand, as in `func:Save < (pkg:example.org/service:api & func:Run)`. A bare right operand binds before `&`, so `func:Save < pkg:example.org/service:api & func:Run` intersects the binary relation result with `func:Run`. The older space-separated `+pkg value` and `-pkg value` filter location rows; colon modifiers filter projected canonical symbols.

## Results and limits

The JSON envelope contains `operation`, `total`, `matches`, `symbols`, `declarations`, `coverage`, `warnings`, and `stages`. Each warning names a skipped `root_key` and `location` and suggests `uir reindex --all`. A found `>>` query also contains `path` with ordered `symbols` and the call-site `calls` connecting them; `total` is one. When no path exists, `total` is zero and `path` is absent. HTTP sets `X-Total-Count` to `total`. Result rows include root, checkout, snapshot, source position, package path, role, canonical symbol ID, and enclosing symbol ID where available. `coverage` lists packages with partial, syntax-only, or excluded indexing, so an empty result is not proof of absence in those packages.

The browser expression field uses a Monaco language for compact queries. Completion shows selectors, modifiers, relations, filters, set and path operators at the cursor. It suggests active canonical symbols for bare names without globs and indexed modules, packages, or typed names for selector prefixes. Arrow keys select suggestions; Enter or Tab inserts one and opens the next relevant choices. Ctrl/Cmd+Space opens suggestions; Ctrl/Cmd+Enter runs the query. `GET /api/v1/modules/suggest?prefix=store.Store.S&root=example.org/service&snapshot=<uuid>` supplies qualified symbol names. `GET /api/v1/modules/suggest-selectors?prefix=pkg:example.org/service:st&root=example.org/service&snapshot=<uuid>` supplies typed selector completions. Both accept `location` or `snapshot` and an optional `limit` up to 100. Suggestion responses are `{items, warnings}` envelopes; unscoped suggestions skip missing heads and report them in `warnings`.

Failed runs mark the expression in Monaco on both the Query page and Cmd+K palette. Invalid syntax returns HTTP 400 with `code: "invalid_query"`, a readable `message`, a `hint`, and `context.line` and `context.column` when the parser has a position. A symbol absent from the selected index returns HTTP 404 with `code: "symbol_not_found"` and a hint to check the symbol or snapshot. The editor marks a reported position; errors without one mark the full expression. Editing the expression clears the marker. Other API failures show their structured message and hint without an HTTP status wrapper.

Only registered roots and their selected snapshots contribute graph edges. Interface dispatch includes relationships recorded by the index; it cannot prove calls through interfaces outside the indexed import closure. The path query is reachability over calls, not dataflow or sanitizer-aware taint analysis. Joins over two free variables belong in a full query language such as CodeQL or Joern. This grammar replaces the former `from`/`where` style, `nodes`, `search`, and `unresolved calls` query expressions; those spellings now fail parsing.
