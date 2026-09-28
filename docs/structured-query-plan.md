# Structured query CLI plan

Status: implemented and verified.

## Query contract

`uir query` accepts an optional compact expression. Boolean `--methods`, `--vars`, `--types`, `--modules`, and `--packages` flags select a union of node kinds; with none, a relation starts from its compatible symbol kind, and a filter alone starts from all active symbols. Repeatable `--include` and `--exclude` globs match full module, package, or qualified symbol paths and filter the starting nodes before relation traversal. Exclusions win. `--callers`, `--calls`, `--implements`, and `--inherits` project incoming callers, outgoing calls, implementers of interfaces, and direct embedders of types. Multiple relations union their results.

## Implementation

1. Add failing Ginkgo and API tests for flag binding, AST equivalence, result shapes, scopes, and embedding.
2. Parse an optional text expression with PEG, compile flags into the same `Expr` tree, and execute the composed tree once. Add text selectors and modifiers corresponding to the flags so both input methods have identical semantics.
3. Extend the evaluator to represent module and package nodes and preserve relation witness rows through set unions. Keep the current query envelope, coverage, warnings, source positions, and pre-limit total.
4. Record direct Go struct and interface embedding edges in versioned typed documents and reverse postings. Older snapshots stay readable for other queries; inheritance queries require a snapshot containing embedding facts.
5. Update query documentation and verify focused tests, the API contract, `go test ./...`, `make lint`, and `make build`.
