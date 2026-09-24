# Symbols and references

UIR has four related, non-interchangeable forms: a structured `Identifier`, rendered/searchable symbol strings, a canonical symbol id proven by the Go type checker, and a per-file document containing declarations and occurrences. The current database stores those documents as JSON in `documents.content`, one per file version and package input hash, and the canonical symbols in the global `symbols` table with an inverted index in `symbol_postings`; the old `uir_nodes` and `uir_relationships` tables are removed.

## Structured identifier

| Field | Meaning |
| --- | --- |
| `id` | Optional producer-supplied UUID. |
| `module` | Highest-level namespace, Go module, service root, or equivalent. |
| `package` | Package or namespace. |
| `type` | Type, class, table, record, or endpoint group. |
| `method` | Method or function name. |
| `field` | Field, variable, parameter, or column name. |
| `signature` | Overload or source-position disambiguator. |
| `node_type` | Explicit kind; when omitted, `GetNodeType()` infers it from populated fields. |

```json
{
  "module": "example.org/service",
  "package": "example.org/service/invoices",
  "type": "InvoiceService",
  "method": "Approve",
  "signature": "(InvoiceID)->error",
  "node_type": "method"
}
```

`Identifier.String()` renders a common declaration as `module.package.type:method#signature`; an endpoint uses slash-separated components. `SymbolKey()` prefixes the lowercase node type, for example `method:example.org/service.example.org/service/invoices.InvoiceService:Approve#(InvoiceID)->error`. These strings are for display and discovery. Components can contain separators, and `ParseIdentifier` handles only the basic dot-and-colon declaration form; it does not round-trip explicit UUIDs, node types, signatures, or endpoint slash forms. `Identifier.Equals` currently compares rendered strings, so it inherits their collision limits.

| Value | Scope and purpose |
| --- | --- |
| `Identifier.String()` | Human-readable projection, not durable identity. |
| `SymbolKey()` | Node-kind-prefixed search projection, not globally unique. |
| `GetUUID()` | Supplied ID or deterministic SHA-1 UUID derived from `SymbolKey()`; cannot distinguish the same symbol in two roots. |
| `IdentityKey()` | Versioned encoding of every structured identifier component; stable lookup key within a logical module root. |
| `semantic_hash` | Hash of semantic node content for change/diff detection, not identity. |

`IdentityKey()` version 1 is `v1:` followed by a compact JSON array in this order: node type, module, package, type, method, field, signature. Empty components remain, so punctuation inside one component cannot collide with separators elsewhere:

```text
v1:["method","example.org/service","example.org/service/invoices","InvoiceService","Approve","","(InvoiceID)->error"]
```

A module root's `root_key` comes from `go.mod`. The same identity key can occur in two roots, two source files, or two checkouts. The current `SourceRevision` is keyed by root, path, content hash, and package path, and its document by root, path, and package input hash. Each document symbol entry carries the identity key as `key` and retains the structured identifier, parent identity, slot/order, payload, semantic hash, optional field detail, and name and extent ranges. The browser's node ID combines revision ID and identity key for selection; it is not a global declaration UUID or an SQL foreign key.

## Canonical symbols

When a package type-checks, its documents are typed (`indexed`, or `partial` when the package has type errors) and every declaration and every identifier use that names a package-level object carries a canonical symbol id. The id is the SHA-256 of a length-delimited encoding of `(identity_version, module_key, package_path, kind, owner_id, name, ordered parameter types)`, stored as `symbols.canonical_key`; for example `Store.Save(ctx context.Context, inv Invoice) error` in `example.org/service/invoices` encodes as:

```text
10:uir-symbol,1:1,19:example.org/service,28:example.org/service/invoices,6:method,64:<id of Store>,4:Save,1:2,15:context.Context,36:example.org/service/invoices.Invoice,
```

| Part | Value |
| --- | --- |
| `module_key` | The Go module path; `std` for the standard library; empty only for `builtin`. |
| `kind` | `package`, `type`, `func`, `method`, `field`, `var`, `const`, or `builtin` (universe objects such as `len`, `int`, `error`, `nil`). |
| `owner_id` | A method's receiver type or interface; a field's struct type, or the field or variable whose literal struct type declares it. |
| Parameter types | Fully qualified named types, composite types spelled out, type parameters by ordinal (`$0`), the variadic parameter as `...T`. Parameter names, results, and field types are not identity. |

Renaming a parameter or changing a result keeps the id and changes the symbol's `shape_hash`; changing a parameter type or a name creates a new id, and the old row is retained. `visibility` is `exported` only when the name and every owner in its chain are exported. Parameters, results, receivers, function-scoped declarations, type parameters, and labels are not canonical: their occurrences have a null `symbol` and a `note`. Builtins get symbol rows but no postings. The symbol row does not record where a symbol is declared; its `definition` postings do, per document, so one id can be declared in several checkout heads, and moving a declaration to another file moves only its posting.

Each typed entry also carries `shape` (the canonical rendering of its declared type: a signature on one line, a struct or interface one gofmt-aligned member per line), `shape_hash` over that rendering, `body_hash` over the declaration's tokens without comments or layout, and `implements` for a type that satisfies interfaces declared in its package or its imports. `symbol_postings` indexes each document's `definition`, `reference` (every occurrence with a symbol, whatever its role), and `implements` facts with an occurrence count; the symbol operations of `uir query` read these postings for the heads or snapshot in scope and then only the documents they select. `symbols.search_name` (the name lowercased and restricted to `[a-z0-9]`) backs exact name lookup and prefix search. A `syntax` document, produced when a package cannot be type-checked, has no ids and no postings; its symbols keep only `key`.

## Reference forms

`NodeRef` embeds an `Identifier` and implements `Node`, allowing a statement to refer to a declaration without embedding it:

```go
target := uir.Identifier{
    Module:   "example.org/service",
    Package:  "example.org/service/invoices",
    Type:     "InvoiceService",
    Method:   "Approve",
    NodeType: uir.NodeTypeMethod,
}
ref := uir.NewRef(target)
```

`NodeRef.GetNode()` currently returns the reference itself; lazy loading is not implemented. Before resolution, only identifier-facing operations such as `GetIdentifier`, `GetType`, and `Pretty` are safe. `GetChildren`, `GetLanguage`, and `GetLocation` delegate through `GetNode()` and recurse indefinitely. `NodeRef.GetType()` returns the explicit `node_type`; if omitted, infer the kind through `ref.GetIdentifier().GetNodeType()`.

`TypeReference` represents a type expression (including generics, unions, arrays, tuples, and optionality), not a node locator. `VariableRef` and `ScopedVariableRef` refer to lexical bindings; `RecordReference` describes record-level mappings such as foreign keys. They must not be substituted for declaration identity.

`UIRRelationship` connects `From` and `To` nodes with a relationship type such as `call`, `reference`, `import`, `inheritance`, `implements`, `includes`, `foreign_key`, `read`, or `write`. Statement-level call edges can leave `From` unset until a traversal attributes them to an enclosing declaration. `NodeTree.GetRelationships()` deduplicates by rendered endpoints and type, so it is not a lossless source of call occurrences.

## Persisted call projection and query behavior

Every call occurrence carries a call locator: the enclosing declaration's identity key as `enclosing_key`, the structured target identifier as `target`, `resolvable`, `local_root`, statement path, range, and original text. In a `syntax` document the symbol is null and `target` is what the AST could name; in a typed document the symbol is the callee's canonical id, `target` names the callee's package and, for a method, the receiver type the type checker proved, and `resolvable` is whether the callee has a canonical symbol, so a call to an undefined function or a local closure is unresolved. Calls carry no target root, and query-time resolution does not use `local_root`.

Two query families read these forms. `nodes` and `unresolved calls` compare `Identifier` fields and `IdentityKey()` against the declarations and call locators of active documents, typed and syntax alike; identical identifiers in multiple roots or checkout heads cannot be constrained by a locator's intended root, and a signature-free locator may match a declaration with a signature. The symbol operations (`references`, `definitions`, `implementations`, `callers`, `callees`, `search`) compare canonical symbol ids instead, so what they return is compiler-proven: a reference is an occurrence whose `symbol` is the target's id, a definition a symbol entry with that id, an implementation a type whose `implements` names it, a caller a `call` occurrence of it, and a callee a `call` occurrence whose `enclosing` is it. One identity declared at several checkout heads resolves to one target with several declarations, not to ambiguous candidates; only different identities matching one selector (for example `Run(int)` at one head and `Run(string)` at another) are candidates. `callers ... including dispatch` adds calls through the interfaces a method's receiver type implements. These operations see nothing in `syntax` packages, which have no ids or postings, and only the proven facts of `partial` ones; every result lists such packages under `coverage`.

`uir query` accepts these expressions:

```sh
uir query 'nodes where symbol_key = "method:example.org/service.example.org/service/invoices.InvoiceService:Approve"' --root example.org/service
uir query 'nodes where identity_key = "v1:[\"method\",\"example.org/service\",\"example.org/service/invoices\",\"InvoiceService\",\"Approve\",\"\",\"\"]"' --root example.org/service
uir query 'references of node where package = "example.org/service/invoices" and type = "InvoiceService" and method = "Approve"' --root example.org/service
uir query 'callers of node where type = "InvoiceService" and method = "Approve" including dispatch' --root example.org/service --location .
uir query 'callees of node where method = "Approve"' --snapshot 6f8b2c6e-79af-4b59-8736-e45696f0c546
uir query 'definitions of node where symbol_id = "3f9c…"' --root example.org/service
uir query 'search "InvoiceService.Ap"' --root example.org/service
uir query 'unresolved calls' --root example.org/service
```

The `symbol_key`, `identity_key`, and `symbol_id` examples are illustrative; inspect actual indexed identifiers for a real module. Without an explicit checkout, the symbol operations read the heads of all registered checkouts. See the [query guide](query.md) for grammar and resolution, and the [storage design](../storage/README.md) for snapshot scope.

For an in-memory UIR document, `NodeTree.Walk` and structured identifier comparisons remain independent of the relational index. `Find(...).WithName(...)` compares `GetName()` and `String()`, but its skip-filter traversal may miss descendants of a heterogeneous root. Prefer `Walk` when searching the entire tree.
