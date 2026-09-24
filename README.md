# UIR — Universal Intermediate Representation

A language-agnostic model for describing code: modules, packages, types, methods, records, relational tables, endpoints and the statements inside method bodies. Extractors emit UIR; generators and analysers consume it.

The Go types in the root `uir` package are the canonical model. `UIR.md` is the model reference; `uir-api.md` covers the broader architecture; `schema/uir.schema.json` is the JSON Schema for the serialized form, generated from those Go types by `make schema` — see [Schema](#schema).

```go
import "github.com/flanksource/uir"
```

## What's here

| Path | What it is |
| --- | --- |
| `.` (`package uir`) | The model: node and statement types, enums, identifiers, fluent builders, pretty printers, semantic hashing, polymorphic JSON, and tree traversal. |
| `diff/` | Structural diffing. `DiffNode` compares two nodes; `DiffTree` compares whole documents and classifies renames and moves. |
| `render/` | Adapters onto [clicky](https://github.com/flanksource/clicky)'s `api.TreeNode` for printing a UIR, or its hierarchy overlay, as a grouped tree. |
| `indexer/` | Incremental Go AST indexing into immutable module-root snapshots, exposed as a context-bound Clicky task. |
| `query/` | PEG query grammar plus module, checkout, snapshot, symbol, and call resolution. |
| `symboldiff/` | Commit-to-commit symbol diff over two snapshots' documents, with per-symbol line counts from hash-verified Git blobs. |
| `schema/` | `uir.schema.json`, generated from the Go types by `make schema`. |
| [`docs/symbols.md`](docs/symbols.md) | Identifier formats, symbol and identity keys, reference forms, and call projection queries. |
| [`docs/query.md`](docs/query.md) | Query grammar, scope resolution, command usage, and result contract. |
| [`docs/diff.md`](docs/diff.md) | `uir diff`: commit selection, symbol classification, shape diff rendering, line attribution, and failure modes. |
| [`docs/indexing.md`](docs/indexing.md) | Go AST coverage, incremental snapshot lifecycle, Git root and submodule behavior, and syntax-only limitations. |
| `cmd/genschema` | The schema generator's entry point; the logic lives in `internal/schemagen`. |
| `cmd/uir` | CLI for adding module directories, querying snapshots, and incrementally reindexing Go workspaces. |
| `web/` | Vite and Clicky UI browser for saved snapshots, embedded in `uir serve`. |
| `python/` | A parallel Python port of the model (pure stdlib). |
| `java/` | A parallel Java port of the model (Jackson-based, source only — no build file is checked in). |

## Schema

`schema/uir.schema.json` is generated, never hand-edited. Run `make schema` after changing a model type, an enum constant or a doc comment; `TestSchemaIsUpToDate` fails the build when the checked-in file no longer matches the Go types. The same command writes `python/statement_kinds.py` from the statement registry, guarded by `TestPythonStatementKindsIsUpToDate`.

The generator (`internal/schemagen`, driven by `cmd/genschema`) reflects over the same fields `encoding/json` marshals, and reads the package source for the two things reflection cannot see: doc comments, which become `description`, and the values of typed string constants, which become `enum` — most `StatementType` values are concatenations of other constants rather than literals, so they only resolve under a type check.

The document root is `oneOf` a `UIR` object and the flat array of nodes that `uir.UnmarshalJSON` reads. `#/$defs/Node` and `#/$defs/Statement` are the polymorphic unions, driven by the `uir.Nodes` and `uir.Statements` registries: registering a type is all it takes to describe it. Each member pins its own discriminator (`node_kind`, `statement_type`) with `const`, so a validator can name the kind it failed on, and every statement member also declares the optional `statement_refinement`.

## Building a document

Every node has a fluent builder. `Build()` returns the node; node-level builders also expose `AsNode()` and `WithSource(path, start, end)`.

```go
method := uir.NewMethod("GetUser").
    WithPackage("com.example.service").
    WithType("UserService").
    WithParam(uir.Field("id", uir.RecordFieldTypeString)).
    WithReturnType(uir.TypeReference{Name: "User"}).
    WithBody(uir.NewBlock().
        WithStatement(uir.NewReturn(uir.VarExpr("user"))).
        Build()).
    Build()

doc := &uir.UIR{}
doc.Add(uir.NewPackage("com.example.service").WithFunction(method).Build())
```

This snippet is `ExampleUIR_Add` in `example_test.go`, so it is compiled and run by the test suite.

Terse helpers cover expressions and literals: `Var`, `VarExpr`, `StringLit`, `IntLit`, `BoolLit`, `Literal`, `Field`, `BinaryExpr`, `UnaryExpr`, `CastExpr`, `ObjectExpr`, `NewReturn`, `NewThrow`.

## Printing

Every node and statement implements `Pretty() api.Text`, so a document renders as source-like coloured output. `render` turns one into a tree:

```go
tree := render.Tree(doc)                          // default git_root,folder,file grouping
tree = render.BuildTree(doc, render.ParseGroupBy("package,file"))
hier := render.BuildHierarchyTree(doc)            // the HierarchyGraph overlay
```

## Diffing

Hashes are selective: they cover semantic structure and deliberately ignore source locations and raw source text, so a node that only moved still hashes equal.

```go
result := diff.DiffNode(before, after, diff.DefaultNodeDiffOptions())
result = diff.DiffTree(beforeDoc, afterDoc, diff.DefaultTreeDiffOptions())

for _, change := range result.Changes {
    fmt.Println(change.Kind, change.Path) // added, deleted, modified, replaced, renamed, moved, rename_move
}
```

Identity classification stays separate from hash equality, which is what lets a relocated-but-identical node be reported as `moved` rather than as an add/delete pair.

## Serialization

Nodes and statements are polymorphic, discriminated by `node_kind` and `statement_type`:

```go
data, err := uir.MarshalNodes(nodes)
doc, err := uir.UnmarshalJSON(data)
```

`uir.NodeMarshaler` and `uir.StatementMarshaler` are the registries; concrete variants register themselves from the `Nodes` and `Statements` prototype slices. Each discriminator is the kind the concrete Go type is registered under, stamped by the codec, so struct literals that leave the embedded type fields unset still round-trip.

- `node_kind` is the node discriminator, written whenever a node crosses an interface-typed `Node` slot (the flat node array, `MethodCallStmt.Method`, `RecordReadStmt.Record`, …). It is the node's static `GetType()` for every registered node, and `ref` for a `NodeRef`. A node nested through a concrete field carries none, and decoding a `Node` slot without one is an error.
- `node_type` is `Identifier` data, not a discriminator: a `MethodNode` may be a `constructor`, and a `NodeRef` carries the `node_type` of the node it points at, so a reference to a method is `{"node_kind": "ref", "node_type": "method", …}` and decodes as a `NodeRef`.
- `statement_type` is the statement's registered kind (`GetStatementType()`). When the statement's `Type` is refined past that kind, the refined value travels under `statement_refinement` — `{"statement_type": "call", "statement_refinement": "call:package"}` — and decodes back exactly. A refinement is accepted only when its longest registered `:`-prefix is the statement's own kind, or the kind lists it explicitly (`control:block` accepts `doc`); anything else is refused on both encode and decode. The Python port checks refinements against the same registered kinds (`python/statement_kinds.py`, generated by `make schema`).

## Python and Java

The Python port mirrors the Go model and emits the same JSON — `cross_language_test.go` proves it by running `python/test_uir.py` and unmarshalling its output into the Go types.

```sh
python3 python/test_uir.py
```

The Java port under `java/com/flanksource/uir/` is source only; no `pom.xml` or `build.gradle` is checked in, and it is compiled ad hoc (see `java/README.md`).

## Snapshot browser

`uir serve` opens the saved UIR database in a local web browser. It lists Go module roots, their checkout locations and snapshots, explores sources and nodes, runs PEG queries, and adds or reindexes local directories.

```sh
go run ./cmd/uir --dsn "$UIR_DSN" serve --host localhost --port 8080
```

Set `UIR_DSN` to a PostgreSQL DSN or SQLite `.db` path and open `http://localhost:8080`. For live frontend development from the repository root, install dependencies with `pnpm --dir web install` and run `go run ./cmd/uir --dsn "$UIR_DSN" serve --dev`. See [serve documentation](docs/serve.md) for source behavior and API routes. Opening an older database discards legacy project snapshots; back it up first if that history matters.

## Development

```sh
make build     # build embedded browser assets and compile every Go package
make test      # Go, browser, and API tests
make lint      # Go and browser lint
make fmt       # go fmt + go mod tidy
make query-parser # regenerate query/grammar.peg.go
```
