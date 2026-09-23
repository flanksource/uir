# Symbols and references

UIR represents a symbol as structured identity, a human-readable projection, and an optional persisted relationship target. These forms serve different purposes and must not be substituted for one another.

This document covers the current Go model and the relational storage contract:

- `Identifier` carries a symbol's structured language-independent name.
- `Identifier.String()` and `SymbolKey()` are display and search projections.
- `identity_key` is the root-scoped persistence identity.
- `NodeRef` carries an unresolved in-memory reference by `Identifier`.
- `uir_relationships` stores calls and other graph edges, including targets that cannot yet resolve to a node row.

## Structured identifiers

`Identifier` contains these fields:

| Field | Meaning |
| --- | --- |
| `id` | Optional producer-supplied UUID. When present, `GetUUID()` returns it unchanged. |
| `module` | Highest-level namespace, module, service root, database server, or API base. |
| `package` | Package, namespace, or database schema. |
| `type` | Type, class, table, record, or endpoint group. |
| `method` | Method or function name. |
| `field` | Field, variable, parameter, or column name. |
| `signature` | Overload or source-position disambiguator. |
| `node_type` | The semantic node kind. When omitted, `GetNodeType()` infers it from the deepest populated component. |

Producers should preserve these fields separately. A component may contain punctuation that is also used as a rendered separator, so a rendered string is not a lossless serialization.

```go
id := uir.Identifier{
    Module:    "billing",
    Package:   "invoices",
    Type:      "InvoiceService",
    Method:    "Approve",
    Signature: "(InvoiceID)->error",
    NodeType:  uir.NodeTypeMethod,
}
```

The JSON form is the portable reference form:

```json
{
  "module": "billing",
  "package": "invoices",
  "type": "InvoiceService",
  "method": "Approve",
  "signature": "(InvoiceID)->error",
  "node_type": "method"
}
```

## Rendered symbol formats

`Identifier.String()` renders common declaration identifiers as follows:

| Shape | Rendered form | Example |
| --- | --- | --- |
| Module | `module` | `billing` |
| Package | `module.package` | `billing.invoices` |
| Type | `module.package.type` | `billing.invoices.InvoiceService` |
| Method | `module.package.type:method` | `billing.invoices.InvoiceService:Approve` |
| Overload | `module.package.type:method#signature` | `billing.invoices.InvoiceService:Approve#(InvoiceID)->error` |
| Endpoint | Slash-separated populated components | `payments/v1/charges/POST` |

Field separators depend on the explicit `node_type`, and endpoint identifiers use `/` instead of declaration separators. Treat those strings as presentation, not as a grammar for round-tripping.

`ParseIdentifier` only understands the basic dot-and-colon declaration form. It does not restore `node_type`, an explicit UUID, signatures, endpoint slash forms, or separator characters embedded inside components. It is therefore not the inverse of `Identifier.String()` for every identifier.

`Identifier.Equals` also compares the rendered strings. It is suitable for the current in-memory compatibility behavior, but it is not structured durable equality and can share the same separator-collision limitations.

## Symbol key, UUID, identity key, and semantic hash

These values are related but not interchangeable:

| Value | Construction and scope | Use | Must not be used for |
| --- | --- | --- | --- |
| `Identifier.String()` | Separator-joined populated fields | Display, logs, broad name matching | Durable identity or parsing arbitrary symbols |
| `Identifier.SymbolKey()` | `lower(node_type) + ":" + Identifier.String()` | Searchable symbol projection | A global unique key |
| `Identifier.GetUUID()` | Explicit `Identifier.id`, otherwise deterministic SHA-1 UUID from `SymbolKey()` | Stable compatibility ID for the same projected symbol | Distinguishing identical symbols in different roots |
| `Identifier.IdentityKey()` / `uir_nodes.identity_key` | Versioned canonical encoding of all structured identifier fields, scoped by `root_id` | Relational identity and target resolution | Display or cross-root uniqueness by itself |
| `uir_nodes.id` | Application-generated row UUID | Foreign keys inside a snapshot | Reconstructing the source-language name |
| `semantic_hash` | Hash of semantic node content | Change detection and diffing | Symbol identity |

The database enforces uniqueness on `(root_id, identity_key)`, not on `symbol_key`. The same package, type, and method may legitimately occur in two projects, two repositories in one workspace, two submodules, or two mounts of the same repository.

`Identifier.IdentityKey()` is the only persistence encoder. Version 1 is `v1:` followed by a compact JSON array in this exact order: node type, module, package, type, method, field, signature. Empty components remain in the array, so punctuation inside a component cannot collide with separators in another component.

```text
v1:["method","billing","invoices","InvoiceService","Approve","","(InvoiceID)->error"]
```

Ingesters must use this method for both `uir_nodes.identity_key` and relationship `to_identity_key`; they must not locally reproduce the format or substitute `SymbolKey()`. Snapshot metadata records `identity_key_version = "v1"` so a future format can be migrated explicitly.

`symbol_key` remains indexed because it is useful for discovery. A lookup by symbol key can return several rows and must be narrowed by snapshot, project, root, language, or another explicit scope.

## Reference forms

### `NodeRef`

`NodeRef` embeds an `Identifier` and implements `Node`, allowing a statement to point at a symbol without embedding its declaration.

```go
target := uir.Identifier{
    Module:   "billing",
    Package:  "invoices",
    Type:     "InvoiceService",
    Method:   "Approve",
    NodeType: uir.NodeTypeMethod,
}
ref := uir.NewRef(target)
```

`NodeRef.GetNode()` currently returns the reference itself. Lazy loading is not implemented, so callers must resolve it through their tree index or relational store. Before resolution, only identifier-facing operations such as `GetIdentifier`, `GetType`, and `Pretty` are safe; `GetChildren`, `GetLanguage`, and `GetLocation` delegate through `GetNode()` and recurse indefinitely. `NodeRef.GetType()` returns only the explicit `node_type`; when it was omitted, use `ref.GetIdentifier().GetNodeType()` to infer the kind from populated fields.

### `TypeReference`

`TypeReference` describes a type expression: generics, unions, intersections, arrays, tuples, functions, optionality, and raw language-specific forms. It is not a node locator and has no root or snapshot scope. A consumer may resolve `TypeReference.Name` and `Package`, but it must retain the original type expression when resolution is ambiguous or external.

### Variable and record references

`VariableRef` and `ScopedVariableRef` refer to lexical bindings inside executable statements. `RecordReference` describes a record-level mapping such as a foreign key. Neither is a replacement for a persisted node relationship; an extractor may project them into `UIRRelationship` values when graph traversal is required.

### In-memory relationships

`UIRRelationship` connects `From` and `To` nodes with a `RelationshipType`. Supported relationship types are:

| Type | Meaning |
| --- | --- |
| `call` | Function, method, constructor, or endpoint invocation |
| `reference` | Variable, field, type, or other symbol reference |
| `import` | Module or package import |
| `inheritance` | Class inheritance or equivalent parent relation |
| `implements` | Interface or contract implementation |
| `includes` | Structural inclusion such as a chart dependency |
| `foreign_key` | Database referential constraint |
| `read` | Read from a record or data source |
| `write` | Write to a record or data source |

`MethodCallStmt.GetRelationships()` and `EndpointCallStmt.GetRelationships()` emit a `call` edge to their target. Record reads and writes emit `read` and `write` edges. These statement-level edges currently leave `From` unset; the traversal or persistence layer must attribute the edge to the enclosing node before writing `from_node_id`.

```go
call := uir.NewMethodCall("Approve", uir.Identifier{
    Module:   "billing",
    Package:  "invoices",
    Type:     "InvoiceService",
    NodeType: uir.NodeTypeMethod,
}).Build()

relationship := call.GetRelationships()[0]
target := relationship.GetTo().GetIdentifier()
```

`NodeTree.GetRelationships()` deduplicates extracted relationships by `from identifier -> to identifier @ relationship type`. Statement edges still have no source at that point, so repeated calls to the same target collapse before an enclosing source can be attributed. Do not use this deduplicated collection as a lossless persistence input. An ingester must traverse statements with enclosing-node context, attribute `From`, and generate `edge_key` from structured edge and occurrence fields before any deduplication.

## Persisted relationship targets

Every stored relationship has an owning source node and a durable target locator:

| Column | Meaning |
| --- | --- |
| `snapshot_id` | Snapshot containing the source edge. |
| `from_root_id`, `from_node_id` | Required source root and node. |
| `relationship_type` | Edge kind such as `call`, `read`, or `reference`. |
| `edge_key` | Deterministic identity of this edge within `from_node_id`; unique with the source node. |
| `to_project_key` | Optional logical target project. Null means the source snapshot's current project. |
| `to_root_key` | Optional target root within the target snapshot. |
| `to_identity_key` | Required root-local target identity. |
| `to_symbol_key` | Searchable target projection. It is not unique. |
| `to_identifier` | Lossless structured `Identifier` JSON used for display and re-resolution. |
| `to_snapshot_id`, `to_node_id` | Resolved target only when it belongs to the same snapshot. |
| `source_id`, line and column fields | Source location of the reference. |
| `statement_path` | Stable path to the statement inside the node payload. |
| `text` | Original source spelling of the reference. |

The locator survives resolution. Resolving a target adds `to_snapshot_id` and `to_node_id`; it does not erase `to_project_key`, `to_root_key`, `to_identity_key`, `to_symbol_key`, or `to_identifier`. Deleting a resolved target sets the resolved IDs to null while leaving the locator available for diagnostics and later re-resolution.

A target in another project or snapshot remains unresolved in this table because the database constraint permits `to_node_id` only when `to_snapshot_id = snapshot_id`. Consumers may follow the locator to another project's published head at query time, but must not store that external row in the same-snapshot resolved columns.

## Querying symbols and calls

[`query.md`](query.md) documents the PEG query language and the `uir project query` entity action built on these storage rules.

### In-memory tree lookup

Walk a loaded UIR tree and compare structured identifier fields when selecting one symbol:

```go
var methods []uir.Identifier
_ = uir.NodeTree{Node: tree}.Walk(func(node uir.Node) bool {
    id := node.GetIdentifier()
    if id.GetNodeType() != uir.NodeTypeMethod {
        return true
    }
    if id.Module == "billing" && id.Package == "invoices" && id.Type == "InvoiceService" && id.Method == "Approve" {
        methods = append(methods, id)
    }
    return true
})
```

This is an in-memory traversal, not a database query. `Find(...).WithName(...)` compares both `Identifier.GetName()` and `Identifier.String()`, but its current skip-filter traversal does not descend through a heterogeneous root that fails the filter. Use `NodeTree.Walk` for a whole document until that traversal behavior is corrected. Rendered-name matching can also return separator collisions, so inspect the structured identifier before selecting one.

### Relational lookup by display symbol

The SQL examples use `?` placeholders because they are intended for `gorm.DB.Raw`, which rewrites placeholders for the active dialect.

```go
var matches []storage.Node
err := database.Raw(`
    SELECT n.*
    FROM uir_nodes AS n
    WHERE n.snapshot_id = ?
      AND n.symbol_key = ?
    ORDER BY n.root_id, n.identity_key`,
    snapshotID,
    "method:billing.invoices.InvoiceService:Approve#(InvoiceID)->error",
).Scan(&matches).Error
```

This query intentionally returns a slice. Add `root_id` or join through `uir_roots.root_key` before treating one match as authoritative.

### Resolved callees of a node

A call is represented by `relationship_type = 'call'`; it is not inferred from a symbol string.

```go
var callees []storage.Node
err := database.Raw(`
    SELECT DISTINCT callee.*
    FROM uir_relationships AS edge
    JOIN uir_nodes AS callee
      ON callee.snapshot_id = edge.to_snapshot_id
     AND callee.id = edge.to_node_id
    WHERE edge.snapshot_id = ?
      AND edge.from_node_id = ?
      AND edge.relationship_type = 'call'
    ORDER BY callee.root_id, callee.identity_key`,
    snapshotID,
    callerID,
).Scan(&callees).Error
```

### Callers of a resolved node

```go
var callers []storage.Node
err := database.Raw(`
    SELECT DISTINCT caller.*
    FROM uir_relationships AS edge
    JOIN uir_nodes AS caller
      ON caller.snapshot_id = edge.snapshot_id
     AND caller.root_id = edge.from_root_id
     AND caller.id = edge.from_node_id
    WHERE edge.snapshot_id = ?
      AND edge.to_node_id = ?
      AND edge.relationship_type = 'call'
    ORDER BY caller.root_id, caller.identity_key`,
    snapshotID,
    calleeID,
).Scan(&callers).Error
```

These two queries return unique symbol nodes. Query `uir_relationships` directly, or select edge fields alongside the node, when each call occurrence and its `statement_path` or source location is required.

### Unresolved calls

Unresolved edges retain enough scope and structure to display or re-resolve the target:

```go
var unresolved []storage.Relationship
err := database.Raw(`
    SELECT edge.*
    FROM uir_relationships AS edge
    WHERE edge.snapshot_id = ?
      AND edge.relationship_type = 'call'
      AND edge.to_node_id IS NULL
    ORDER BY edge.to_project_key, edge.to_root_key, edge.to_identity_key`,
    snapshotID,
).Scan(&unresolved).Error
```

Resolve same-snapshot targets by joining the relationship's scoped locator to the root and node identity. Use `to_symbol_key` only as a candidate search key when the structured locator is incomplete.

```sql
SELECT target.id
FROM uir_relationships AS edge
JOIN uir_snapshots AS target_snapshot
  ON target_snapshot.id = edge.snapshot_id
JOIN uir_projects AS target_project
  ON target_project.id = target_snapshot.project_id
JOIN uir_roots AS target_root
  ON target_root.snapshot_id = edge.snapshot_id
 AND target_root.root_key = edge.to_root_key
JOIN uir_nodes AS target
  ON target.root_id = target_root.id
 AND target.identity_key = edge.to_identity_key
WHERE edge.id = ?
  AND edge.to_node_id IS NULL
  AND edge.to_root_key IS NOT NULL
  AND (edge.to_project_key IS NULL OR edge.to_project_key = target_project.project_key);
```

This resolver requires an explicit root key and refuses a different target project. A locator without `to_root_key` is incomplete because `identity_key` is root-scoped; use its structured identifier and `to_symbol_key` only to find candidates, then require an explicit disambiguation before persisting a resolution.

## Rules for producers and consumers

- Preserve the structured `Identifier`; never persist only its rendered string.
- Scope durable node identity by root and snapshot ownership.
- Do not assume `symbol_key` or `GetUUID()` is globally unique.
- Keep the target locator after a reference resolves.
- Generate `edge_key` from canonical structured edge fields, including occurrence identity when repeated calls are meaningful.
- Attribute statement-level relationships to their enclosing node before persistence.
- Query calls through `relationship_type = 'call'` and node IDs; use symbol strings for discovery, not graph integrity.
- Treat `NodeRef`, `TypeReference`, variable references, and record references as different contracts.
