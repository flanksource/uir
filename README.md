# UIR — Universal Intermediate Representation

A language-agnostic model for describing code: modules, packages, types, methods, records, relational tables, endpoints and the statements inside method bodies. Extractors emit UIR; generators and analysers consume it.

The Go types in the root `uir` package are the canonical model. `UIR.md` is the model reference; `uir-api.md` covers the broader architecture; `schema/uir.schema.json` is the JSON Schema for the serialized form.

```go
import "github.com/flanksource/uir"
```

## What's here

| Path | What it is |
| --- | --- |
| `.` (`package uir`) | The model: node and statement types, enums, identifiers, fluent builders, pretty printers, semantic hashing, polymorphic JSON, and tree traversal. |
| `diff/` | Structural diffing. `DiffNode` compares two nodes; `DiffTree` compares whole documents and classifies renames and moves. |
| `render/` | Adapters onto [clicky](https://github.com/flanksource/clicky)'s `api.TreeNode` for printing a UIR, or its hierarchy overlay, as a grouped tree. |
| `python/` | A parallel Python port of the model (pure stdlib). |
| `java/` | A parallel Java port of the model (Jackson-based, source only — no build file is checked in). |

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

Nodes and statements are polymorphic, discriminated by `node_type` and `statement_type`:

```go
doc, err := uir.UnmarshalJSON(data)
```

`uir.NodeMarshaler` and `uir.StatementMarshaler` are the registries; concrete variants register themselves from the `Nodes` and `Statements` prototype slices. The discriminator is derived from `GetStatementType()`, not from the embedded cache field, so struct literals that leave it unset still round-trip.

## Python and Java

The Python port mirrors the Go model and emits the same JSON — `cross_language_test.go` proves it by running `python/test_uir.py` and unmarshalling its output into the Go types.

```sh
python3 python/test_uir.py
```

The Java port under `java/com/flanksource/uir/` is source only; no `pom.xml` or `build.gradle` is checked in, and it is compiled ad hoc (see `java/README.md`).

## Development

```sh
make build     # compile every package
make test      # go test ./...
make lint      # golangci-lint
make fmt       # go fmt + go mod tidy
```
