# Phase 3: Schema and generated bindings implementation

Status: in progress on `feat/uir-schema-bindings`, based on `0350729`, in `/Users/moshe/go/src/github.com/flanksource/uir-schema-bindings`. Taskfile migration, schema contracts and all three SDK foundations are implemented. The Python minimum was raised to 3.10 with explicit user approval. Plugin and RPC cutover remain open.

## Implemented

- `Taskfile.yml` owns build, lint, schema, pinned generators, binding checks and fixture commands. The Makefile delegates existing entrypoints to those tasks.
- `task schema:check` checks JSON Schema, OpenAPI, Java generator configuration and Python registry artifacts without modifying them. Five Ginkgo artifact tests cover writes, unchanged files, drift, missing files and write failures.
- Required fields follow Go's actual JSON tags, including shadow fields in custom marshalers. Concrete definitions keep optional tags; polymorphic slots require unique discriminators through tagged definitions and `oneOf`.
- Refinement constraints derive from the statement registry: the longest registered prefix determines the variant, and explicitly accepted cross-hierarchy refinements remain valid. Independent JSON Schema validation is compared with Go's decoder for every registered kind, descendant prefixes, cross-hierarchy refinements, unknown values and newline boundaries.
- OpenAPI 3.1 models are projected mechanically from the canonical schema, preserving constraints and recursive references and supplying explicit discriminator mappings. Shared discriminator enums give union interfaces and variants the same Java getter types while preserving each variant's `const`. Constrained string refinements become named scalar components, with generated Java schema mappings to `java.lang.String`. Independent validation checks all registered payloads, descendants, unknown refinements and missing/unknown tags against the projected catalog.
- `bindings/typescript` packages `@flanksource/uir`: generated wire declarations, the canonical schema, and small parsing helpers backed by Ajv. CommonJS output preserves the existing Node.js 18 baseline. Runtime validation does not coerce values, add defaults or remove fields.
- TypeScript generation and checking use the SDK's own locked tools. They are independent of browser dependencies. SDK checks compare freshly generated declarations and schema bytes before building, without overwriting checked-in sources.
- Ginkgo conformance exercises all 12 registered nodes and 36 statements, then six populated method/type/statement cases with nested bodies, `dispatch_call`, refinements and metadata containing null, empty strings, empty lists and false. The TypeScript SDK round trips these through its actual packaged entrypoint and back into Go.
- `bindings/java` builds `com.flanksource:uir:1.0.0` with the existing Java 17 baseline, a pinned Gradle wrapper and strict dependency locking. Binary/source JARs, a POM and runtime dependencies are prepared locally. `WireCodec` validates against the bundled canonical schema before decoding and after encoding; Jackson omits absent optional fields and preserves metadata nulls. Eight conformance checks cover 54 exact round trips, discriminator rejection, five malformed statement cases and a runnable archive consumer.
- `bindings/python` builds `flanksource-uir:1.0.0` for Python 3.10+, with generated dataclasses, generator-emitted field/model metadata and a standard-library codec. The bundled canonical schema validates inputs and encoded model state, while generated annotations identify inline dataclasses. Unknown schema vocabulary fails loudly. Wheel checks use an actual Python 3.10 interpreter and separate test/consumer environments; the consumer installs without dependencies.

## Commands

Run from this worktree with Go, Task, pnpm, Node.js, uvx, a JDK and curl installed:

```bash
task schema
task schema:check
task bindings:typescript
task bindings:typescript:check
task bindings:python:check
task bindings:python:build
task bindings:java:build
task bindings:java:check
task bindings:check
task lint:schema
make build
make lint
```

`BINDINGS_OUT` changes the ignored preview destination; relative paths resolve from the repository root. Python previews/archives and Java generated sources/archives remain under `.tmp/bindings`. Python and TypeScript generation update their owned packages; check tasks verify drift without rewriting owned artifacts. See the [Python instructions](../../bindings/python/README.md), [TypeScript instructions](../../bindings/typescript/README.md) and [Java instructions](../../bindings/java/README.md) for API usage and local packaging. No SDK has been published and no plugin imports have changed.

## Evidence and blockers

- Schema contract, projection, drift and artifact tests pass. Existing root Go tests, including the handwritten Python wire model, pass. Focused schema and conformance Go lint passes.
- TypeScript build, consumer imports, 19 Vitest tests, and Go to TypeScript to Go conformance pass. A fresh preview in a separate output directory compares byte-for-byte with the checked-in SDK. Local package archive creation succeeds.
- Pins are Python `datamodel-code-generator==0.83.0`, TypeScript `json-schema-to-typescript@16.0.0` and Java OpenAPI Generator `7.25.0`.
- The [Python generator decision](python-binding-decision.md) records the failed older-generator probes and the approved Python 3.10 minimum. No generated output is patched and no custom template is used.
- Python wheel/source builds, deterministic model/metadata/schema drift checks, 676 Python tests, 54 exact Go round trips, the installed-wheel consumer and Ruff lint pass. Tests independently compare every registered kind's refinement schema against JSON Schema validation and cover malformed payloads, inline models, wire aliases, required nulls, metadata and nanosecond timestamps. Runtime dependency declarations remain empty.
- Java generation, SDK build, consumer smoke test, malformed statement rejection and exact round trips pass. Shared discriminator enums and scalar schema mappings address the original emitted-type failures without editing generated source or weakening schema constraints. The archive includes its canonical JSON Schema.
- `make build` fails at unchanged `web/src/CallGraphPane.tsx:123`: installed browser declarations accept `ring`, while the source supplies `columns`. `make lint` reports missing `web/dist` for Go embedding because the browser build fails.

## Remaining in phase 3

- Replace handwritten and vendored foreign models in plugins with UIR-owned SDK packages after their gates pass. Broaden populated fixtures to cover every optional field and each consumer's real payloads.
- Replace duplicated UIR protobuf messages with a versioned canonical JSON payload, rebuild host and plugins together, and remove converters after callers migrate.
- Add consumer builds with released dependencies and publication wiring. Phase 2 lowering and the prepared arch-unit consumer worktree are unchanged by this checkpoint.

## Tracked verification

`fixtures/ownership/schema-bindings.md` verifies the schema and Taskfile foundation, tracked as Gavel TODO `a4d3aad5-1651-4ca4-96d7-114086933866`: all five checks pass. `fixtures/ownership/binding-conformance.md` records the three-language SDK gate, tracked as TODO `6629a7b7-49b7-4f21-8f62-2c181ac6251b`: all three checks pass. Every language remains required to succeed before plugin adoption.
