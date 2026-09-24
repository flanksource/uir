# UIR Model Reference

This document is the canonical model reference for the Universal Intermediate Representation (UIR) used in this repository.

It is written for two audiences:

- plugin and extractor authors that need to generate valid UIR
- consumers that need to read or transform emitted UIR JSON

`uir-api.md` covers broader architectural and conceptual material. This document stays close to the current implementation in the root `uir` package, the current JSON shape, and the current plugin transport layer.

## Scope and source of truth

- The Go types in the root `uir` package (`github.com/flanksource/uir`) are the canonical model.
- The JSON field names on those types are the canonical serialized field names.
- The registered concrete node variants are the entries in `Nodes`.
- The registered concrete statement variants are the entries in `Statements`.
- When this document and the code disagree, the code wins.

## Serialization contract

### Root vs node documents

- `UIR` is the root aggregate. It carries neither `node_kind` nor `node_type`.
- `node_kind` is the node discriminator. It is the kind the node's concrete Go type is registered under in `NodeMarshaler`: the node's static `GetType()` (`package`, `class`, `method`, `record_field`, …) for every registered node, and `ref` for a `NodeRef`. The codec stamps it whenever a node crosses an interface-typed (`Node`) slot — every element of the flat node array (`MarshalNodes` / `UnmarshalJSON`) and the `Node`-typed statement fields (`MethodCallStmt.Method`, `EndpointCallStmt.endpoint`, `RecordReadStmt.Record`, `RecordWriteStmt.Record`). A node nested through a concrete field (`UIR.packages`, `TypedNode.methods`, …) carries no `node_kind`, because its Go type is already known. Decoding a `Node` slot whose document has no `node_kind`, or one nothing is registered under, is an error rather than a guess.
- `node_type` is `Identifier` data, not a discriminator. A `MethodNode` may carry `node_type: "constructor"`, and a `NodeRef` carries the `node_type` of the node it points at, so `{"node_kind": "ref", "node_type": "method", "method": "GetUser"}` decodes as a reference to a method, never as a `MethodNode`. Stamping the concrete kind over `node_type` would change the node's identity, which is why the discriminator has a key of its own.
- Concrete statements are identified by `statement_type`, which the codec stamps from the concrete type's registered kind rather than from the embedded `Type` field.
- A statement whose `Type` is refined past its registered kind keeps `statement_type` at the kind and carries the refined value in `statement_refinement`, e.g. `{"statement_type": "call", "statement_refinement": "call:package"}`. A refinement is accepted when its longest registered `:`-prefix is the statement's own kind, or when the kind lists it explicitly (a `control:block` may be refined to `doc`); anything else is refused on encode and decode.
- `MethodNode.body` is always a `BlockStmt`, even when the body object omits `statement_type` in current Go builder output.

### Shared embedding model

Most concrete nodes embed the same three building blocks:

- `Metadata`
- `Identifier`
- `SourceCode`

That means most node objects can carry:

- annotations, comments, and arbitrary properties
- `module`, `package`, `type`, `method`, `field`, `signature`, and `node_type`
- a `sourceCode` object

### Empty-object behavior

The Go model uses value types for several nested objects. In practice this means some fields often serialize as empty objects instead of being omitted, for example:

- `sourceCode: {}`
- `validation: {}`
- `input: {}`
- `output: {}`

Consumers should treat those empty objects as "present but empty", not as meaningful data.

### Current Go JSON quirks

These are part of the current emitted surface and consumers should tolerate them:

- `MethodCallStmt` marshals its target node under `Method`, not `method`.
- `RecordReadStmt` and `RecordWriteStmt` marshal their target node under `Record`, not `record`.
- `TypedNode` is registered as `node_kind: "class"` and defaults to `node_type: "class"`, not `"type"`.
- `UIR.functions` stores `MethodNode` values. In current Go output those nodes still use `node_type: "method"`, and a `MethodNode` crossing a `Node` slot is stamped `node_kind: "method"`.
- `SourceCode` in Go JSON inlines location fields under `sourceCode`; the proto transport nests them under `source_code.location`.

## Model map

### Canonical root shape

| Field | Type | Meaning |
| --- | --- | --- |
| `modules` | `[]ModuleNode` | Top-level modules or namespaces. |
| `packages` | `[]PackageNode` | Top-level packages or schemas. |
| `types` | `[]TypedNode` | Top-level types not nested under packages. |
| `records` | `[]ASTRecord` | Top-level records or schemas. |
| `tables` | `[]RecordTable` | Top-level relational tables and views. SQL extraction emits these rather than `records`. |
| `endpoints` | `[]ASTEndpoint` | Top-level integration endpoints. |
| `functions` | `[]MethodNode` | Top-level callable declarations. |
| `hierarchy` | `HierarchyGraph` | Optional non-AST structural graph. |
| `rawFiles` | `[]RawFile` | Verbatim files to emit alongside generated code. |

### Canonical concrete node variants

These are the node kinds currently registered in `NodeMarshaler`, keyed by the `node_kind` a node carries across a `Node` slot. For every variant except `NodeRef` the kind equals the node's default `node_type`.

| `node_kind` | Go type | Typical role |
| --- | --- | --- |
| `module` | `ModuleNode` | Highest-level grouping such as module, namespace, package scope, or tenant. |
| `package` | `PackageNode` | Package, schema, namespace, folder-level logical grouping. |
| `class` | `TypedNode` | Class, interface-like type, struct, object type, or composite declaration. |
| `method` | `MethodNode` | Method or function declaration. |
| `record` | `ASTRecord` | Table-like or schema-like data shape. |
| `endpoint` | `ASTEndpoint` | External integration point such as HTTP, Kafka, JMS, SOAP, or gRPC endpoint. |
| `record_field` | `RecordField` | Field within a record, method signature, type, or package variable list. |
| `table` | `RecordTable` | Relational table or view. Unlike `record` it keeps what a database catalogue reports: typed columns, an ordered primary key, indexes, and foreign keys. Project it with `AsRecord()` when a consumer only understands records. |
| `column` | `RecordColumn` | Column of a table, carrying the dialect's own SQL type alongside the portable `fieldType`, plus ordinal, nullability, primary-key and auto-increment as distinct facts. |
| `index` | `RecordIndex` | Index over a table's columns, in index order. |
| `foreign_key` | `RecordForeignKey` | Referential constraint from one table's columns to another's. |
| `ref` | `NodeRef` | Leaf reference to a node by its `Identifier`. Its `node_type` is the referenced node's type, so it round-trips as a reference instead of decoding as the node it names. |

### Canonical concrete statement variants

These are the statement kinds currently registered in `StatementMarshaler`. A statement is always stamped with one of these under `statement_type`; a finer `Type` travels under `statement_refinement`.

| `statement_type` | Go type | Role |
| --- | --- | --- |
| `assignment` | `AssignmentStmt` | Assignment or mutation. |
| `assignment:binary` | `BinaryStmt` | Binary expression. |
| `control:block` | `BlockStmt` | Statement block and scope. |
| `control:break` | `BreakStmt` | Loop break. |
| `assignment:cast` | `CastStmt` | Type cast. |
| `control:condition` | `ConditionStmt` | Wrapped condition expression. |
| `decl:const` | `ConstDeclStmt` | Constant declaration. |
| `control:continue` | `ContinueStmt` | Loop continue. |
| `call:api` | `EndpointCallStmt` | Endpoint invocation. |
| `assignment:expression` | `ExprStmt` | Expression wrapper. |
| `control:loop:for` | `ForStmt` | `for` loop. |
| `decl:func` | `FunctionDeclStmt` | Inline or nested function declaration. |
| `control:if` | `IfStmt` | Conditional branch. |
| `assignment:literal` | `LiteralStmt` | Literal value. |
| `call` | `MethodCallStmt` | Method or function invocation. |
| `call:read:record` | `RecordReadStmt` | Read from record-like source. |
| `call:write:record` | `RecordWriteStmt` | Write to record-like source. |
| `control:return` | `ReturnStmt` | Return statement. |
| `control:switch` | `SwitchStmt` | Switch or match. |
| `control:throw` | `ThrowStmt` | Throw or raise. |
| `control:try` | `TryStmt` | Try/catch. |
| `assignment:tuple` | `TupleStmt` | Tuple expression. |
| `decl:type` | `TypeDeclStmt` | Type declaration. |
| `assignment:unary` | `UnaryStmt` | Unary expression. |
| `decl:var` | `VariableDeclStmt` | Variable declaration. |
| `assignment:variable` | `VariableStmt` | Variable expression. |
| `control:loop:while` | `WhileStmt` | `while` loop. |
| `doc` | `DocStmt` | Documentation node in executable trees. |
| `test` | `TestStmt` | Test block. |
| `raw` | `RawStmt` | Lossless raw source fragment. |
| `assignment:destructure` | `DestructuringStmt` | Object or array destructuring. |
| `assignment:template` | `TemplateLiteralStmt` | Template or interpolated string. |

## Shared primitives

### Identifier

`Identifier` is the logical name carrier for most nodes.

See [`docs/symbols.md`](docs/symbols.md) for rendered formats, durable identity scope, reference forms, and call-query examples.

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | `string` | Optional explicit UUID-like identifier. Rarely needed for plugin authors. |
| `module` | `string` | Top-level grouping. For endpoints this is often the base URL or service root. |
| `package` | `string` | Package, schema, namespace, or secondary grouping. |
| `type` | `string` | Type, class, table, topic family, or record name. |
| `method` | `string` | Method or function name. |
| `field` | `string` | Field or variable name. |
| `signature` | `string` | Disambiguator for overloads or statement-local identity. |
| `node_type` | `string` | The node's declared type (e.g. `method` or `constructor`). Identifier data, not the codec discriminator — see `node_kind`. |

Notes:

- UIR names are hierarchical, not generic. Most declarations do not have a standalone `name` field.
- Endpoint identifiers stringify with `/` separators instead of `.` and `:`.
- Producers should populate the deepest meaningful identifier components and leave unrelated ones empty.

### Location

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | `string` | Source path or virtual path. |
| `start_line` | `int` | First source line. |
| `end_line` | `int` | Last source line. |
| `column` | `int` | Optional source column. |
| `last_modified` | `timestamp` | Optional last modification time. |

### SourceCode

In Go JSON, `Location` is inlined inside `sourceCode`.

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | `string` | Source or virtual path. |
| `start_line` | `int` | First source line. |
| `end_line` | `int` | Last source line. |
| `column` | `int` | Optional source column. |
| `last_modified` | `timestamp` | Optional modification time. |
| `content` | `string` | Optional source snippet. |
| `language` | `string` | Language hint such as `go`, `xml`, `typescript`, `sql`, or `yaml`. |

### Metadata

| Field | Type | Meaning |
| --- | --- | --- |
| `annotations` | `[]Annotation` | Structured annotations or decorators. |
| `comments` | `[]Comment` | Source comments or extracted docs. |
| `properties` | `map[string]any` | Unstructured extension data. |

### Annotation

`Annotation` reuses `Identifier` and carries a single typed value or a named value map.

| Field | Type | Meaning |
| --- | --- | --- |
| identifier fields | `Identifier` | Symbolic identity for the annotation. |
| `value` | `TypedValue` | Single annotation value. |
| `values` | `map[string]TypedValue` | Named annotation attributes. |

### Comment

`Comment` inlines `Location`.

| Field | Type | Meaning |
| --- | --- | --- |
| location fields | `Location` | Comment location. |
| `text` | `string` | Comment body. |
| `type` | `CommentType` | Single-line, multi-line, or documentation. |
| `context` | `string` | Optional extraction context. |

### TypedValue

Exactly one of these should normally be set.

| Field | Type | Meaning |
| --- | --- | --- |
| `string` | `string` | String value. |
| `float` | `float64` | Floating-point value. |
| `int` | `int64` | Integer value. |
| `bool` | `bool` | Boolean value. |
| `date` | `timestamp` | Timestamp value. |
| `unit` | `Unit` | Structured unit value. |

### Unit

| Field | Type | Meaning |
| --- | --- | --- |
| `type` | `UnitType` | Unit family such as `currency`, `bytes`, or `seconds`. |
| `value` | `string` | Optional modifier such as currency code or timezone. |

### TypeReference

`TypeReference` is the main cross-language type carrier.

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | `string` | Base type name such as `string`, `Promise`, `User`, or `Map`. |
| `fieldType` | `RecordFieldType` | Primitive or schema-oriented fallback type. |
| `typeArgs` | `[]TypeReference` | Generic arguments. |
| `union` | `[]TypeReference` | Union members. |
| `intersection` | `[]TypeReference` | Intersection members. |
| `optional` | `bool` | Optional or nullable marker. |
| `literalValue` | `string` | Literal type payload. |
| `isArray` | `bool` | Array marker. |
| `tupleElements` | `[]TypeReference` | Tuple element types. |
| `functionParams` | `[]TypeReference` | Function parameter types. |
| `functionReturns` | `TypeReference` | Function return type. |
| `rawType` | `string` | Language-specific raw type when UIR cannot model the shape precisely. |
| `package` | `string` | Import source for generated code. |

### TypeParam

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | `string` | Generic parameter name. |
| `constraint` | `TypeReference` | Optional upper bound or constraint. |
| `default` | `TypeReference` | Optional default type. |

### Expression

`Expression` is used mainly in validations and record read/write statements.

| Field | Type | Meaning |
| --- | --- | --- |
| source location fields | `SourceCode` | Optional expression location and source text. |
| `type` | `ExpressionType` | Expression language such as `sql`, `xpath`, or `cel`. |
| `expression` | `string` | Raw expression text. |

## Canonical node families

### RawFile

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | `string` | Output-relative path. |
| `content` | `string` | Verbatim file contents. |

Use `RawFile` for generated artifacts that are not best represented as AST nodes, for example `package.json`, `tsconfig.json`, or linter configuration.

### ModuleNode

Shared fields:

- `Metadata`
- `Identifier`
- `SourceCode`

Module-specific fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `packages` | `[]PackageNode` | Packages nested inside the module. |

### PackageNode

Shared fields:

- `Metadata`
- `Identifier`
- `SourceCode`

Package-specific fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `types` | `[]TypedNode` | Types owned by the package. |
| `records` | `[]ASTRecord` | Records owned by the package. |
| `endpoints` | `[]ASTEndpoint` | Endpoints owned by the package. |
| `functions` | `[]MethodNode` | Package-level functions. |
| `variables` | `[]RecordField` | Package-level variables or constants. |
| `initFunctions` | `[]MethodNode` | Package initialization functions. |

### TypedNode

Shared fields:

- `Metadata`
- `Identifier`
- `SourceCode`

Type-specific fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `visibility` | `Visibility` | Visibility modifier. |
| `variables` | `[]RecordField` | Fields or member variables. |
| `methods` | `[]MethodNode` | Methods owned by the type. |
| `types` | `[]TypedNode` | Nested types. |
| `constructor` | `MethodNode` | Constructor. |
| `destructor` | `MethodNode` | Destructor or cleanup method. |
| `typeParams` | `[]TypeParam` | Generic parameters. |
| `implements` | `[]TypeReference` | Implemented contracts. |
| `extends` | `TypeReference` | Parent type. |
| `isAbstract` | `bool` | Abstract type marker. |

### MethodNode

Shared fields:

- `Metadata`
- `Identifier`
- `SourceCode`

Method-specific fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `visibility` | `Visibility` | Visibility modifier. |
| `params` | `[]RecordField` | Structured parameter list. |
| `returns` | `[]RecordField` | Structured return values. |
| `errors` | `[]ASTError` | Declared or discovered errors. |
| `body` | `BlockStmt` | Executable body tree. |
| `isAsync` | `bool` | Async marker. |
| `isGenerator` | `bool` | Generator marker. |
| `typeParams` | `[]TypeParam` | Generic parameters. |
| `returnType` | `TypeReference` | Declared return type. |

Notes:

- `returns` and `returnType` are complementary. Use `returns` for named structured returns and `returnType` for language-level type information.
- The same `MethodNode` shape is used for package functions and root-level functions.

### ASTRecord

Shared fields:

- `Metadata`
- `Identifier`
- `SourceCode`

Record-specific fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `extends` | `[]ASTRecord` | Parent records to inline or compose. |
| `description` | `string` | Human-readable description. |
| `recordType` | `RecordType` | Semantic record kind such as SQL table, file schema, or function input. |
| `fields` | `[]RecordField` | Record fields. |
| `examples` | `[]string` | Example payloads or row snippets. |
| `validation` | `RecordValidation` | Whole-record validation rules. |
| `references` | `[]RecordReference` | External record references. |

Notes:

- `Identifier.type` is the record name.
- `recordType` describes semantics, not identity.
- `extends` embeds full records, not references.

### RecordField

Shared fields:

- `Metadata`
- `Identifier`
- `SourceCode`

Field-specific fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `label` | `string` | Human-friendly display label. |
| `fieldType` | `RecordFieldType` | High-level field kind. |
| `typeRef` | `TypeReference` | Detailed language-level type. |
| `defaultValue` | `TypedValue` | Default or initializer value. |
| `readOnly` | `bool` | Read-only field marker. |
| `writeOnly` | `bool` | Write-only field marker. |
| `validation` | `FieldValidation` | Per-field validation rules. |
| `visibility` | `Visibility` | Visibility marker when used as a member variable. |

Notes:

- The field name lives in `Identifier.field`.
- `fieldType` is the coarse schema category. Use `typeRef` when you need richer type information.

### FieldValidation

| Field | Type | Meaning |
| --- | --- | --- |
| `minLength` | `int` | Minimum string length. |
| `maxLength` | `int` | Maximum string length. |
| `maxItems` | `int` | Maximum array size. |
| `minItems` | `int` | Minimum array size. |
| `minProperties` | `int` | Minimum object property count. |
| `maxProperties` | `int` | Maximum object property count. |
| `uniqueItems` | `bool` | Array uniqueness constraint. |
| `sortedItems` | `bool` | Sorted-array expectation. |
| `regex` | `string` | Regular expression constraint. |
| `enum` | `[]string` | Allowed literal values. |
| `min` | `float64` | Inclusive numeric minimum. |
| `minExclusive` | `float64` | Exclusive numeric minimum. |
| `maxExclusive` | `float64` | Exclusive numeric maximum. |
| `max` | `float64` | Inclusive numeric maximum. |
| `minDate` | `string` | Earliest allowed date or date expression. |
| `maxDate` | `string` | Latest allowed date or date expression. |
| `required` | `bool` | Required marker. |
| `unique` | `bool` | Uniqueness marker. |
| `nonEmpty` | `bool` | Non-empty marker. |
| `expressions` | `[]Expression` | Custom validation expressions. |
| `unitTypes` | `[]UnitType` | Allowed unit families. |
| `conditions` | `map[Expression]FieldValidation` | Conditional validation rules. |

Note:

- `conditions` is part of the Go model, but it uses non-string map keys. Treat it as advanced or implementation-specific unless you fully control both producer and consumer.

### RecordValidation

| Field | Type | Meaning |
| --- | --- | --- |
| `uniqueFields` | `[]string` | Composite uniqueness field set. |
| `minRecords` | `int` | Minimum record count. |
| `maxRecords` | `int` | Maximum record count. |
| `anyOf` | `[]string` | Any-of field constraints. |
| `oneOf` | `[]string` | One-of field constraints. |
| `allOf` | `[]string` | All-of field constraints. |
| `noneOf` | `[]string` | None-of field constraints. |
| `expressions` | `[]Expression` | Custom record-level validation expressions. |
| `conditions` | `map[Expression]RecordValidation` | Conditional record rules. |

Note:

- `conditions` has the same non-string-map-key caveat as `FieldValidation.conditions`.

### RecordReference

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | `string` | Target record or field name. |
| `mapping` | `string` | Mapping expression or join mapping. |
| `recordReferenceType` | `RecordReferenceType` | Foreign-key or soft-key relationship. |

### ASTEndpoint

Shared fields:

- `Metadata`
- `Identifier`
- `SourceCode`

Endpoint-specific fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `method` | `EndpointPointMethod` | HTTP verb when applicable. |
| `endpointType` | `EndpointType` | Integration kind such as `http`, `kafka`, or `grpc`. |
| `input` | `ASTRecord` | Request, message, or input schema. |
| `output` | `ASTRecord` | Response, message, or output schema. |
| `examples` | `[]string` | Example payloads or calls. |
| `errors` | `[]ASTError` | Declared or observed errors. |

Notes:

- Endpoint identity still comes from the embedded identifier fields.
- For HTTP endpoints, producers usually use `method` plus identifier path components to represent the route.

### ASTError

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | `string` | Optional stable error identifier. |
| `name` | `string` | Error name. |
| `sourceCode` | `SourceCode` | Error declaration or extraction location. |
| `errorType` | `ASTErrorType` | Error category. |
| `description` | `string` | Human-readable description. |
| `code` | `TypedValue` | Protocol-level or domain-level error code. |

## Hierarchy graph

`HierarchyGraph` represents non-AST structural hierarchies that still need symbol attachment and source provenance.

### HierarchyGraph

| Field | Type | Meaning |
| --- | --- | --- |
| `nodes` | `[]HierarchyNode` | Graph nodes. |

### HierarchyNode

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | `string` | Stable node identifier within the graph. |
| `kind` | `string` | Hierarchy-specific node kind. |
| `name` | `string` | Raw name. |
| `displayName` | `string` | Render-friendly label. |
| `properties` | `map[string]string` | String properties. |
| `values` | `map[string]TypedValue` | Typed values. |
| `sourceCode` | `SourceCode` | Source location. |
| `children` | `[]HierarchyEdge` | Ordered child edges. |
| `attachments` | `[]HierarchyAttachment` | Links back to UIR symbols. |
| `imports` | `[]ImportSpec` | Import metadata. |
| `exports` | `[]ExportSpec` | Export metadata. |

### HierarchyEdge

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | `string` | Edge label. |
| `targetId` | `string` | Target hierarchy node ID. |
| `edgeKind` | `string` | Hierarchy-specific edge kind. |
| `order` | `int` | Stable order within siblings. |

### HierarchyAttachment

| Field | Type | Meaning |
| --- | --- | --- |
| `role` | `string` | Attachment role. |
| `symbolRef` | `UIRSymbolRef` | Symbol attachment target. |
| `placementName` | `string` | Placement hint for generators. |

### UIRSymbolRef

| Field | Type | Meaning |
| --- | --- | --- |
| `symbolKind` | `string` | Symbol kind, usually a node type. |
| `module` | `string` | Symbol module. |
| `package` | `string` | Symbol package. |
| `type` | `string` | Symbol type. |
| `method` | `string` | Symbol method. |
| `name` | `string` | Symbol display name or field name. |

### ImportSpec and ExportSpec

`HierarchyNode` imports and exports are generator-facing structural metadata.

`ImportSpec`:

| Field | Type | Meaning |
| --- | --- | --- |
| `module` | `string` | Import source. |
| `defaultName` | `string` | Default import name. |
| `namespaceName` | `string` | Namespace import alias. |
| `named` | `[]ImportNamedSpec` | Named imports. |
| `typeOnly` | `bool` | Type-only import marker. |

`ImportNamedSpec`:

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | `string` | Imported symbol name. |
| `alias` | `string` | Local alias. |

`ExportSpec`:

| Field | Type | Meaning |
| --- | --- | --- |
| `kind` | `string` | Export kind. |
| `name` | `string` | Exported symbol name. |
| `alias` | `string` | Export alias. |
| `module` | `string` | Re-export source. |

## Statement model

### Common statement shape

All concrete statements embed:

- `statement_type`
- `statement_refinement`, only when the statement's `Type` is refined past its registered kind (see [Root vs node documents](#root-vs-node-documents))
- inline source location fields through `SourceCode`

Statements are usually nested under `MethodNode.body.children`.

### Block and control flow statements

`BlockStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `statement_type` | `control:block` | Block discriminator when emitted. |
| `variables` | `[]RecordField` | Variables introduced by the block. |
| `children` | `[]Statement` | Nested statements. |

`IfStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `condition` | `ConditionStmt` | Branch condition. |
| `then` | `BlockStmt` | Then branch. |
| `else` | `BlockStmt` | Else branch. |

`SwitchStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `value` | `ExprStmt` | Switch input. |
| `cases` | `[]object` | Case list. Each case object carries `condition` and `body`. |

`ForStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `init` | `AssignmentStmt` | Initialization step. |
| `cond` | `ConditionStmt` | Loop condition. |
| `body` | `BlockStmt` | Loop body. |
| `update` | `ExprStmt` | Update step. |

`WhileStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `condition` | `ConditionStmt` | Loop condition. |
| `body` | `BlockStmt` | Loop body. |

`TryStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `body` | `BlockStmt` | Protected body. |
| `catch` | `BlockStmt` | Catch body. |

`ReturnStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `value` | `ExprStmt` | Returned value. |

`ThrowStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `exception` | `ExprStmt` | Thrown value. |

`BreakStmt` and `ContinueStmt` carry no fields beyond `statement_type` and optional source location.

### Declaration statements

`VariableDeclStmt` and `ConstDeclStmt` inline a `RecordField`.

That means their JSON shape carries the usual record-field fields directly on the statement object, for example:

- `field`
- `label`
- `fieldType`
- `typeRef`
- `defaultValue`
- `validation`

`FunctionDeclStmt` inlines a `MethodNode`.

`TypeDeclStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | `string` | Declared type name. |

### Expression and data-movement statements

`ExprStmt` is a wrapper around one of the concrete expression forms below.

| Field | Type | Meaning |
| --- | --- | --- |
| `literal` | `LiteralStmt` | Literal payload. |
| `variable` | `ScopedVariableRef` | Scoped variable reference. |
| `binary` | `BinaryStmt` | Binary expression. |
| `cast` | `CastStmt` | Cast expression. |
| `unary` | `UnaryStmt` | Unary expression. |
| `method_call` | `MethodCallStmt` | Method call expression. |
| `endpoint_call` | `EndpointCallStmt` | Endpoint call expression. |
| `record_read` | `RecordReadStmt` | Record read expression. |
| `tuple` | `TupleStmt` | Tuple expression. |

`AssignmentStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `target` | `ScopedVariableRef` | Assignment target. |
| `value` | `ExprStmt` | Assigned value. |
| `op` | `AssignmentOp` | Assignment operator. |

`BinaryStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `left` | `ExprStmt` | Left operand. |
| `op` | `BinaryOp` | Binary operator. |
| `right` | `ExprStmt` | Right operand. |

`UnaryStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `op` | `UnaryOp` | Unary operator. |
| `operand` | `ExprStmt` | Operand. |

`LiteralStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `value` | `string` | Literal payload as text. |
| `fieldType` | `RecordFieldType` | Literal category. |

`CastStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `expr` | `ExprStmt` | Cast input. |
| `targetType` | `string` | Target type text. |

`TupleStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `elements` | `[]ExprStmt` | Tuple elements. |

`VariableStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | `string` | Variable name. |

### Call and I/O statements

`MethodCallStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `Method` | `NodeRef or node` | Callee symbol. Current Go JSON uses the capitalized key `Method`. |
| `receiver` | `ExprStmt` | Receiver expression for instance calls. |
| `arguments` | `[]object` | Positional or named argument objects. |

`EndpointCallStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `endpoint` | `NodeRef or node` | Target endpoint. |
| `arguments` | `[]object` | Call argument objects. |

`RecordReadStmt` and `RecordWriteStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `Record` | `NodeRef or node` | Target record. Current Go JSON uses the capitalized key `Record`. |
| expression fields | `Expression` | Optional query, filter, or selector carried inline as `type` and `expression`. |
| `arguments` | `[]object` | Read or write argument objects. |
| metadata fields | `Metadata` | Optional comments, annotations, or properties on the I/O operation itself. |

### Statement helper substructures

`ScopedVariableRef`:

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | `string` | Variable name in scope. |
| `defaultValue` | `TypedValue` | Optional default when null or missing. |
| identifier fields | `Identifier` | Optional fully-qualified reference when the variable refers to a specific symbol. |

`Arguments` is an array of objects with:

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | `string` | Optional argument name. Omit for positional arguments. |
| `value` | `ExprStmt` | Argument payload. |

`ConditionStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `expr` | `ExprStmt` | Wrapped condition expression. |

### Documentation, tests, and passthrough statements

`DocStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `doc_type` | `DocType` | Document fragment kind such as `h1`, `paragraph`, or `code_block`. |
| `content` | `string` | Rendered content. |
| `style` | `string` | Optional style hint. |
| `children` | `[]DocStmt` | Nested documentation nodes. |

`TestStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| embedded block fields | `BlockStmt` | Test body. |
| `name` | `string` | Test name. |

`RawStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `source` | `string` | Verbatim raw source. |
| `language` | `string` | Raw source language. |

`DestructuringStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `isArray` | `bool` | Array destructuring marker. |
| `bindings` | `[]DestructureBinding` | Extracted bindings. |
| `rest` | `string` | Rest binding name. |
| `value` | `ExprStmt` | Source expression. |

`DestructureBinding`:

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | `string` | Bound property or element name. |
| `alias` | `string` | Local alias. |
| `default` | `ExprStmt` | Default value. |
| `nested` | `DestructuringStmt` | Nested destructuring pattern. |

`TemplateLiteralStmt`:

| Field | Type | Meaning |
| --- | --- | --- |
| `strings` | `[]string` | Literal string fragments. |
| `expressions` | `[]ExprStmt` | Embedded expressions. |
| `tag` | `ExprStmt` | Optional template tag. |

## Relationships and references

### NodeRef

`NodeRef` is the lightweight reference form for another UIR node. It serializes as an inline `Identifier`.

Use `NodeRef` when one node points at another and you do not want to duplicate the full target object.

### UIRRelationship

`UIRRelationship` is not currently embedded in the `UIR` root, but it is the canonical relationship object produced by tree traversal and analysis.

| Field | Type | Meaning |
| --- | --- | --- |
| metadata fields | `Metadata` | Relationship annotations or comments. |
| source location fields | `SourceCode` | Relationship source location. |
| `id` | `uuid` | Relationship ID. |
| `relationship_type` | `RelationshipType` | Import, call, read, write, inheritance, and related relationship kinds. |
| `from` | `Node` | Source node. |
| `to` | `Node` | Target node. |

## Enumerations

### Node types

Defined values:

`package`, `class`, `method`, `module`, `record`, `table`, `column`, `index`, `foreign_key`, `endpoint`, `function`, `record_field`, `dependency`, `interface`, `constructor`, `function_variable`, `package_variable`, `import`, `annotation`, `comment`, `statement`, `""`

Practical note:

- only the concrete node variants listed in the canonical node table are currently registered for polymorphic node unmarshalling

### Visibility

`public`, `private`, `protected`, `internal`, `package`, `""`

### Record field types

`string`, `number`, `boolean`, `array`, `object`, `enum`, `date`, `float`, `int`, `ip`, `cidr`, `url`, `email`, `uuid`, `json`, `jsonb`, `yaml`, `xml`, `csv`, `phone`, `text`, `secret`, `map`

### Record reference types

`foreignKey`, `softKey`

### Record types

`input`, `output`, `sql:table`, `sql:view`, `sql:index`, `sql:stored_proc`, `sql:function`, `cache`, `file:yaml`, `file:json`, `file:xml`, `file:csv`, `other`

### Expression types

`sql`, `xpath`, `jsonpath`, `regex`, `jq`, `yq`, `jmespath`, `cel`, `SpEL`, `ognl`

### Endpoint types

`soap`, `jms`, `kafka`, `tcp`, `http`, `grpc`, `graphql`

### Endpoint methods

`GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `OPTIONS`, `HEAD`

### Relationship types

`import`, `call`, `reference`, `inheritance`, `implements`, `includes`, `foreign_key`, `read`, `write`, `""`

### Comment types

`single_line`, `multi_line`, `documentation`

### Error types

`syntax`, `validation`, `compilation`, `runtime`, `configuration`, `network`, `auth`, `authz`, `timeout`

### Assignment operators

`=`, `+=`, `-=`, `*=`, `/=`, `%=`, `&=`, `|=`, `**=`, `??=`, `&&=`, `||=`, `append`

### Binary operators

`&&`, `||`, `==`, `!=`, `<`, `<=`, `>`, `>=`, `+`, `-`, `*`, `/`, `%`, `like`, `ilike`, `regex`, `startsWith`, `endsWith`, `contains`, `??`, `?.`, `in`, `instanceof`, `**`, `&`, `|`, `^`, `<<`, `>>`, `>>>`, `===`, `!==`

### Unary operators

`!`, `-`, `+`, `++`, `--`, `*`, `&`, `typeof`, `void`, `delete`, `await`, `yield`, `...`, `~`, `keyof`, `readonly`

## Producer best practices

### 1. Emit the smallest correct semantic node

- Use `TypedNode` for classes, structs, and composite types.
- Use `ASTRecord` for table-like or schema-like data models.
- Use `ASTEndpoint` for external integration points.
- Use `RawStmt` only when UIR cannot represent the construct without losing meaning.

### 2. Populate identifiers consistently

- `module` should be stable across all children from the same logical source.
- `package` should group related declarations.
- `type`, `method`, and `field` should be filled only when they are actually part of the symbol identity.
- Use `signature` for overloads or statement-level disambiguation, not for human-readable labels.

### 3. Prefer structured typing

- Put coarse schema information in `fieldType`.
- Put richer language-level information in `typeRef`.
- Use `TypeReference.rawType` only when the richer type system still cannot model the source shape.

### 4. Preserve source provenance

- Always set `sourceCode.path` when the source is known.
- Include `start_line`, `end_line`, and `column` when extraction can provide them cheaply.
- Use virtual paths for non-file-backed sources such as remote APIs, database schemas, or generated virtual artifacts.

### 5. Keep ownership and references separate

- Put declarations under the owning `UIR`, `ModuleNode`, `PackageNode`, or `TypedNode`.
- When one statement or node points to another symbol, prefer `NodeRef` or a relationship instead of embedding a duplicate full node.

### 6. Use records deliberately

- Use `ASTRecord.recordType` to explain what the record means.
- Use `ASTEndpoint.input` and `ASTEndpoint.output` for request/response schemas.
- Use `MethodNode.params` and `MethodNode.returns` for function signatures.

### 7. Treat hierarchy and raw files as first-class outputs

- Use `HierarchyGraph` when the source contains meaningful structural hierarchies that are not best expressed as AST nodes.
- Use `RawFile` when generation must emit verbatim support files alongside code.

### 8. Be conservative with extension properties

- Prefer typed fields first.
- Use `Metadata.properties` for source-specific details that do not yet deserve a core model field.
- Keep property keys stable and namespaced when they become plugin-specific.

## Canonical vs helper and transport types

### Canonical emitted surface

Treat these as the stable emitted model for producers and consumers:

- `UIR`
- `RawFile`
- `ModuleNode`
- `PackageNode`
- `TypedNode`
- `MethodNode`
- `ASTRecord`
- `RecordField`
- `ASTEndpoint`
- `ASTError`
- `HierarchyGraph` and related hierarchy types
- registered statement variants in `Statements`
- shared primitives such as `Identifier`, `SourceCode`, `TypeReference`, `TypedValue`, `Expression`, and validation types

### Helper or internal convenience types

These are useful in code, but they are not the main target shape for plugin authors:

- `UIRNode`: one-of wrapper around concrete nodes
- `Stmt`: helper wrapper that selects one concrete statement
- `NodeList`: utility wrapper
- `UIRStatement`: base helper with unimplemented `Node` methods
- `ASTFunction`: exported but not part of the canonical registered node surface
- builder APIs such as `NewPackage`, `NewType`, and `NewMethod`

### Transport-layer differences

The plugin proto transport is close to the Go model, but it is not identical.

Notable differences today:

- `SourceCode` uses nested `location` in proto, but inlined location fields in Go JSON.
- Statements and expressions are wrapped in proto `Stmt` and `ExprStmtWrapper` `oneof` messages.
- `TypeReference.package` is represented as `import_module` in proto.
- `TypedValue.unit` is part of the Go model but is not represented in the current proto `TypedValue`.
- `Metadata.properties` is `map[string]any` in Go but stringly typed in proto.
- Some proto messages still reflect older shapes for annotations and endpoints.

Practical rule:

- if you are producing JSON directly, target the Go JSON shape
- if you are speaking the plugin protocol, target `plugin/proto/plugin.proto`

### Cross-language bindings

The Python and TypeScript bindings are intended to mirror the Go model. They are useful producer libraries, not the source of truth. When there is drift, the Go model and current transport mapping take precedence.

## Examples

Examples below are trimmed for readability. Real Go JSON may include empty `sourceCode`, `validation`, `input`, or `output` objects.

### Minimal root

```json
{
  "packages": [
    {
      "module": "github.com/example/service",
      "package": "com.example.service",
      "node_type": "package",
      "sourceCode": {
        "path": "internal/service/user_service.go"
      }
    }
  ]
}
```

### Type with one method

```json
{
  "type": "UserService",
  "node_type": "class",
  "visibility": "public",
  "methods": [
    {
      "method": "GetUser",
      "node_type": "method",
      "params": [
        {
          "field": "id",
          "node_type": "record_field",
          "fieldType": "string"
        }
      ],
      "returns": [
        {
          "field": "user",
          "node_type": "record_field",
          "fieldType": "object"
        }
      ],
      "body": {
        "children": [
          {
            "statement_type": "control:return",
            "value": {
              "statement_type": "assignment:expression",
              "variable": {
                "name": "user"
              }
            }
          }
        ]
      }
    }
  ]
}
```

### Record with field validation

```json
{
  "type": "User",
  "node_type": "record",
  "recordType": "file:json",
  "fields": [
    {
      "field": "email",
      "node_type": "record_field",
      "fieldType": "string",
      "validation": {
        "required": true,
        "regex": "^[^@]+@[^@]+$"
      }
    }
  ],
  "validation": {
    "uniqueFields": ["email"]
  }
}
```

### Method body with polymorphic statements

```json
{
  "children": [
    {
      "statement_type": "assignment",
      "target": {
        "name": "price"
      },
      "value": {
        "statement_type": "assignment:expression",
        "cast": {
          "statement_type": "assignment:cast",
          "expr": {
            "statement_type": "assignment:expression",
            "variable": {
              "name": "priceStr"
            }
          },
          "targetType": "float"
        }
      },
      "op": "="
    },
    {
      "statement_type": "control:return",
      "value": {
        "statement_type": "assignment:expression",
        "variable": {
          "name": "price"
        }
      }
    }
  ]
}
```

### Hierarchy graph fragment

```json
{
  "hierarchy": {
    "nodes": [
      {
        "id": "screen:quote",
        "kind": "screen",
        "name": "quote",
        "children": [
          {
            "name": "button",
            "targetId": "action:save",
            "edgeKind": "contains",
            "order": 0
          }
        ],
        "attachments": [
          {
            "role": "handler",
            "symbolRef": {
              "symbolKind": "method",
              "module": "github.com/example/app",
              "package": "ui.quote",
              "type": "QuoteScreen",
              "method": "Save"
            }
          }
        ]
      }
    ]
  }
}
```

## Practical checklist for new plugins

When implementing a new extractor or generator, make sure the emitted UIR answers these questions:

- What is the root ownership structure: module, package, type, or root-level node?
- Does every emitted declaration have the right identifier components?
- Does every declaration carry a path and location when the source is known?
- Are function signatures modeled with `params`, `returns`, and `returnType` instead of freeform text?
- Are external integrations represented as `ASTEndpoint` and `EndpointCallStmt`?
- Are data schemas represented as `ASTRecord` and `RecordField`?
- Are cross-node links represented by refs or relationships instead of duplicated nodes?
- Are language-specific leftovers isolated in `rawType`, `properties`, or `RawStmt` instead of leaking into core fields?

If the answer is yes, the plugin is usually emitting UIR at the right level of fidelity.
