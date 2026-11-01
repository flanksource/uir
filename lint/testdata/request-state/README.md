# Request-state lint example

This self-contained module exercises the UIR port of OIPA's `noGlobalRequestState` rule. The configuration checks exact resolved types, including aliases and inferred types. The small local packages stand in for application configuration/runtime types and the database package; no application infrastructure is accessed.

From the UIR repository root:

```sh
make binary
GOWORK=off .bin/uir --dsn .tmp/request-state-example.db lint --config lint/testdata/request-state/.uir.yaml --format json --no-progress
```

The command exits 1 with 13 error findings, one per forbidden global variable. Locals, fields, distinct named types, unrelated packages, containers, deeper pointers and pointers to `context.Context` do not match. The copied original rule is in `reference.rules.go`, excluded from normal builds by its build tag.

Run the direct comparison from the UIR root:

```sh
go test -tags ruleguardparity ./lint -ginkgo.focus='Ruleguard parity'
```

The comparison pins Ruleguard v0.4.5 and Go 1.22.12 because that Ruleguard runner cannot read Go 1.27 export data. Go downloads the toolchain/dependencies if absent. All 11 original findings match UIR's name positions and messages. UIR additionally catches `GroupA` and `GroupB`, which the original single-name AST pattern misses.
