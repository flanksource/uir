# Typed selectors and glob filters for compact queries

Status: implemented on 2026-09-24. This document records the query contract and the implementation sequence.

## Goal and current seam

Add `pkg:`, `mod:`, `func:`, `field:`, and `struct:` selectors, with anchored globs and `+`/`-` include and exclude modifiers. Each selector must work alone, in set expressions, as a relation subject, and as a binary relation operand. Keep the existing snapshot selection, canonical symbol IDs, result envelope, and relation directions.

Before this change, `query/grammar.peg` emitted one untyped `symbol` token; `query/compact_resolve.go` matched its rendered name and `query/compact_execute.go` turned it into a set of active canonical symbols. Unary `<`, `>`, `=`, `<<`, `:impl`, `:methods`, and `~w` returned location rows, while `&` and `|` composed projected symbol sets and `>>` found a call path. `+pkg` and `-pkg` filtered result-row package paths, and `-f` excluded a file suffix. The browser had its own tokenization and completion context.

## Language contract

| Selector | Unary meaning in the selected snapshots | Match key |
| --- | --- | --- |
| `pkg:packagePattern` or `pkg:modulePattern:packagePattern` | Active canonical symbols declared in matching Go packages, including a package symbol when indexed | Full `package_path`, or `module_key` plus the package path relative to that module |
| `mod:pattern` | Active canonical symbols belonging to matching registered Go module roots | `module_key` / selected root key |
| `func:pattern` | Package functions and receiver or interface methods | `kind IN ('func','method')`, then qualified name |
| `field:pattern` | Struct fields, including indexed fields of literal struct types; not variables or constants | `kind = 'field'`, then qualified name |
| `struct:pattern` | Defined, non-alias named Go types whose underlying type is a struct in the selected snapshot | `kind = 'type'`, active document type form, then qualified name |

The first two selectors are scopes that produce symbol sets, not new module or package result entities. This lets `pkg:example.org/shop:store & func:Save` and `mod:example.org/shop >> func:Run` use the existing set and path machinery. A bare subject keeps its current name-resolution behavior; typed selectors always denote sets, so a short name matching several symbols is intentional and can be narrowed with `&`, a package, or a module.

Examples accepted by the current parser:

```text
pkg:example.org/shop/store
pkg:example.org/shop:store
pkg:example.org/*:internal/**
mod:example.org/shop & struct:Store
func:Store.Save +pkg:example.org/shop:store
field:Store.count -mod:example.org/legacy
func:Run < pkg:example.org/shop/app
func:Run > func:Store.Save
func:Run >>3 func:Store.Save
(func:Save <) +pkg:example.org/shop/app
```

### Glob rules

- Match case-sensitively and anchor the whole selected key. `*` matches zero or more characters except `/`; `?` matches one character except `/`; `**` matches across `/` boundaries. Treat `.`, `@`, `#`, `$`, and `!` as literal; escape operator characters with `\`, while `:` remains a selector separator. Implement a bounded glob compiler, not an unescaped SQL `LIKE` or regexp built from user text.
- The one-pattern `pkg:packagePattern` form and `mod:modulePattern` match complete import/module paths. For example, `pkg:example.org/shop/*` matches direct child packages; `pkg:example.org/shop/**` matches that root package and descendants. `pkg:store` means only a package whose entire path is `store`; it is not a fuzzy suffix search.
- The two-pattern `pkg:modulePattern:packagePattern` form matches `modulePattern` against a selected registered module root key, then matches `packagePattern` against each package's path relative to that root. The package at the module root is spelled `.`; `*` selects direct child packages, and `**` selects the root and all descendants. Thus `pkg:example.org/shop:store` selects `example.org/shop/store`, `pkg:example.org/shop:.` selects `example.org/shop`, and `pkg:example.org/*:internal/**` selects `internal` and its descendants separately within each matching module. A symbol must belong to the matched module root; never let the relative pattern cross into another module, even when module roots have overlapping path prefixes. A module pattern matching no selected registered root returns an empty set.
- `func:`, `field:`, and `struct:` match `name` when the pattern has no `.` or `/`, the owner-qualified suffix when it has `.` but no `/`, and the full `query_name` when it has `/`. Thus `func:Save`, `func:Store.Save`, and `func:example.org/shop/store.Store.Save` have increasingly narrow scopes. Globs apply to whichever key this rule selects.
- Allow `**` only as a complete slash-delimited segment and reject malformed patterns such as `***`, an empty pattern, an empty side of `pkg:modulePattern:packagePattern`, a third colon, dangling escapes, or unmatched syntax. `**/` may match zero segments; document and test the root-package case. The `.` root-package token is valid only as the entire relative package pattern. There is no implicit descendant expansion, regex syntax, brace expansion, or filesystem access.
- Match only active symbols from the selected primary heads, `--location`, or `--snapshot`. An exact selector with no active match returns an empty result with coverage; invalid syntax and unsupported scope combinations are errors. Keep `total` as the untruncated row count and apply `--limit` only to returned rows.

### Include and exclude modifiers

`+kind:pattern` and `-kind:pattern` are postfix modifiers on a selector or parenthesized expression. A positive modifier keeps matches; a negative modifier removes matches. Repeated positive modifiers of the same kind are ORed, negatives of the same kind are ORed and subtracted, and different kinds are ANDed. Modifier order does not change the result. The modifier group ends at a relation or binary operator; parentheses explicitly attach modifiers to a completed expression.

```text
func:* +pkg:example.org/shop/app +pkg:example.org/shop/store -func:Test*
(func:* +pkg:example.org/shop:app +pkg:example.org/shop:store) -pkg:example.org/shop:store/test/**
(func:Save <) +pkg:example.org/shop/app -func:Test*
(func:Save | func:Load) -mod:example.org/legacy
```

An unmodified `kind:pattern` is a positive selector. A leading `-kind:pattern` is invalid because there is no input set to subtract from. When modifiers apply to a symbol set, test the symbol's typed properties. When they apply to a relation result, project rows to canonical result symbols first, filter that set, then retain only rows belonging to those symbols. Rows without a canonical projected symbol cannot satisfy a typed modifier; count their omission in `stages`. This is intentionally distinct from the existing `-f` location filter and the existing space-separated `+pkg`/`-pkg` location filters. Document the two package-filter meanings explicitly, and migrate examples to the new spelling only where symbol filtering is intended.

### Unary and binary relation semantics

Keep the current unary operators. A binary relation `A op B` first evaluates `A op` with its existing traversal, then retains rows whose projected canonical result symbol belongs to selector set `B`. It returns the same location-row kind as the unary form, with `symbols` identifying the left-hand targets and `matches` carrying only witnesses that matched `B`. The right side can be a selector or a parenthesized set expression. Do not turn a binary relation into a plain `&`: that would lose the call/reference positions. Resolve both operands before reporting ambiguity; typed selectors are sets even when their spelling is exact.

| Operator | Unary `A op` | Binary `A op B` |
| --- | --- | --- |
| `<` | Incoming call sites for callables, references otherwise | Keep sites whose enclosing canonical caller or referrer is in `B` |
| `>` | Outgoing call sites from a function or method | Keep sites whose resolved callable callee is in `B`; unresolved calls cannot match `B` |
| `<<N` | Callers within the existing depth bound | Keep callers in `B` after traversal; do not prune intermediate callers |
| `:impl` | Types implementing the left interface | Keep implementing types in `B` |
| `:methods` | Methods owned by the left type | Keep methods in `B` |
| `~w` | Write references to the left symbol | Keep writes whose enclosing canonical symbol is in `B` |
| `=` | Definition rows | Remains unary; reject `A = B` rather than inventing equality semantics |
| `>>N` | No unary form | Existing shortest call path from callable symbols in `A` to callable symbols in `B` |

For `<` and `~w`, a file-scope occurrence without an enclosing canonical symbol cannot satisfy a binary right operand. For `>` and `>>`, a right operand containing no callable symbols is an error naming the incompatible kind. For `:impl` and `:methods`, retain the existing left-kind validation. Binary relation results should preserve `RootKey`, snapshot, path, position, role, depth, and dispatch flags of the surviving rows. With no witnesses, return an empty result rather than a fabricated match. `&` and `|` remain binary set operators over canonical IDs; `A >>N B` remains a path operator over endpoint sets.

Bind selectors and postfix modifiers tighter than relations, relations tighter than `&`, and `&` tighter than `|`; `>>` joins complete endpoint expressions as it does now. Thus `func:Save < pkg:example.org/shop/app & func:Run` means `(func:Save < pkg:example.org/shop/app) & func:Run`; use `func:Save < (pkg:example.org/shop/app & func:Run)` to constrain one binary relation's right operand. An operator with no right operand before `&`, `|`, `)`, or end of input is unary. Use parentheses around a unary relation before applying a new typed modifier, as in `(func:Save <) +pkg:example.org/shop/app`.

## Snapshot-aware struct classification

`symbols.kind` is currently only `type` for structs, interfaces, other named types, and aliases. A canonical type ID can survive a shape change across snapshots, so a global `symbols.type_form` would give the wrong answer for at least one snapshot. Use the active definition document as the authority.

Extend each typed `DocumentSymbol` with a validated `type_form` for type declarations, populated from `go/types`: `struct` for a non-alias `*types.TypeName` whose `*types.Named` underlying type is `*types.Struct`; `interface`, `other`, and `alias` for the remaining forms. Do not infer this from field count, since an empty struct has no fields. Version the document writer and its input hash so new snapshots contain the field. Keep old immutable snapshots readable: the v1 canonical `shape` already renders underlying structs as `type Name... struct {` and aliases as `type Name... = ...`; a strict v1 shape decoder can classify them, and must fail with snapshot/document context if the stored shape cannot be classified. Do not mutate published documents or silently label an unknown form `other`. `syntax` and unproven partial declarations have no canonical ID and remain outside typed selector results, with existing coverage reporting.

## Implementation sequence

1. Add Ginkgo parser tables first for all five selectors, wildcard boundaries, modifiers, precedence, binary forms, and explicit errors. Add focused integration expectations in `query/` against the existing indexed fixtures, including a struct-to-interface snapshot change. Confirm these fail before editing the parser or evaluator.
2. Extend `query/grammar.peg`, regenerate `query/grammar.peg.go` with `make query-parser`, and extend `query/parser.go` and `query/query.go` with typed selector and optional relation-right AST fields. Parse selector kind, optional module pattern, and package/name glob into separate fields; do not encode them back into an untyped name string. Reject a third colon and empty sides at the parser boundary.
3. Add one glob matcher and typed resolver in `query/compact_resolve.go`. For two-pattern `pkg:`, filter selected roots by the module pattern, compute each candidate package's relative path at a `/` boundary, and match the package pattern there. Apply SQL equality or literal-prefix pruning where possible, then validate full globs against structured fields, intersect with active postings, and preserve snapshot provenance until the result set is complete. Keep the existing bare-name resolver as its own mode.
4. Add per-document type form extraction, validation, and v1 decoding in `indexer/` and `storage/`. Prove an empty struct, a defined type over another struct, an alias, an interface, and a type whose form changes between snapshots.
5. Extend `query/compact_execute.go` and `query/compact_graph.go` to evaluate modifier groups and binary relations while retaining witness rows, dispatch, depth, and the existing result envelope. Match binary right operands by canonical IDs; apply the right operand only after bounded traversal for `<<N`.
6. Update `query/compact_suggest.go`, `web/src/query-completion.ts`, `web/src/query-language.ts`, and their Vitest coverage so completion and highlighting understand prefixes, globs, modifiers, both `pkg:` forms, module-root and relative-package completion, and whether the cursor is on the left or right of a relation. Keep CLI/API `args: [expression]` and scope options unchanged; add or extend `packages/api/test/*.spec.ts` for serialized expressions and server results.
7. Update `docs/query.md` with the exact grammar, operator table, examples, migration note for existing package row filters, and coverage limits. Run focused Ginkgo and Vitest suites, `make lint`, `make build`, and `go test ./...`; report any unrelated baseline failures separately. For the visible query editor, verify completion and binary query results in the local browser with agent-browser.

## Acceptance checks

- Each of the five selectors returns the expected active symbol set alone and when composed with `&`, `|`, unary relations, binary relations, and `>>`; `func:` includes methods, `field:` excludes variables, and `struct:` excludes aliases and interfaces.
- Glob tests cover exact paths, zero- and multi-segment `**`, `*`, `?`, case sensitivity, punctuation, malformed patterns, and include/exclude OR-within-kind and AND-across-kind behavior. Both `pkg:` forms agree for an exact module/package pair; relative `.` and `**` cover the root; a module glob can select several roots without crossing module boundaries; the two-pattern form works as a unary selector, modifier, and binary operand.
- Unary relation rows remain unchanged; binary forms retain only independently expected witness positions. A binary `<<N` can match a caller through an intermediate symbol excluded by its right operand. Null projected symbols and noncallable operands follow the explicit rules above.
- The same type ID classifies as `struct` only in the snapshot where its active declaration is a struct. Old typed snapshots remain queryable; malformed historical shapes fail visibly. Syntax and partial coverage is reported rather than interpreted as a proven negative.
- CLI, HTTP, and browser expression input agree on the grammar and preserve root/location/snapshot selection, `total`, `limit`, coverage, and error behavior.
