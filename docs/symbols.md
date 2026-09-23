# Symbols and references

UIR has three related, non-interchangeable forms: a structured `Identifier`, rendered/searchable symbol strings, and a source-revision projection containing declarations and call locators. The current database stores those projections as JSON in `uir_source_revisions`; the old `uir_nodes` and `uir_relationships` tables are removed.

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

A module root's `root_key` comes from `go.mod`. The same identity key can occur in two roots, two source files, or two checkouts. The current `SourceRevision` is keyed by root, path, content hash, package path, and extractor version; its node projection retains the structured identifier, parent identity, slot/order, payload, semantic hash, optional field detail, and source position. The browser's node ID combines revision ID and identity key for selection; it is not a global declaration UUID or an SQL foreign key.

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

The Go AST indexer stores each call occurrence inside its source revision's JSON projection, with `FromIdentity`, structured `ToIdentifier`, `Resolvable`, statement path, source position, and original text. The projection format also has optional `ToRootKey` and `LocalRoot` fields, but the current Go extractor does not populate `ToRootKey`, and query-time resolution does not use either root field. There is no stored `to_node_id` to imply a type-checked relationship. Reused source revisions are matched by `ToIdentifier.IdentityKey()` against declarations in the selected scopes; identical identifiers in multiple roots or checkout heads cannot be constrained by an edge's intended root.

`uir query` accepts these expressions:

```sh
uir query 'nodes where symbol_key = "method:example.org/service.example.org/service/invoices.InvoiceService:Approve"' --root example.org/service
uir query 'nodes where identity_key = "v1:[\"method\",\"example.org/service\",\"example.org/service/invoices\",\"InvoiceService\",\"Approve\",\"\",\"\"]"' --root example.org/service
uir query 'callers of node where type = "InvoiceService" and method = "Approve"' --root example.org/service --location .
uir query 'callees of node where method = "Approve"' --snapshot 6f8b2c6e-79af-4b59-8736-e45696f0c546
uir query 'unresolved calls' --root example.org/service
```

The `symbol_key` and `identity_key` examples are illustrative; inspect actual indexed identifiers for a real module. Query predicates compare exact structured fields or projections. A symbol key may match several declarations and a graph target must resolve uniquely. Without an explicit checkout, graph-target selection examines heads from all registered checkouts and returns candidate rows when ambiguous. A signature-free call locator may match a declaration with a signature, but `go/ast` cannot prove dynamic receiver types or compilation validity. See the [query guide](query.md) for grammar and [storage design](../storage/README.md) for snapshot scope.

For an in-memory UIR document, `NodeTree.Walk` and structured identifier comparisons remain independent of the relational index. `Find(...).WithName(...)` compares `GetName()` and `String()`, but its skip-filter traversal may miss descendants of a heterogeneous root. Prefer `Walk` when searching the entire tree.
