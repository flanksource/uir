# UIR TypeScript bindings

The `@flanksource/uir` package contains wire types generated from UIR's Go model and runtime validation against its canonical JSON Schema. The package is prepared locally; publication and plugin adoption are pending.

From the UIR repository root, regenerate sources and run the SDK checks:

```bash
task bindings:typescript
task bindings:typescript:check
```

`bindings:typescript` updates `model.d.ts` and `uir.schema.json`. Do not edit these files. `bindings:typescript:check` checks schema and SDK drift without rewriting checked-in sources, builds the package, runs Vitest, verifies a consumer import, and runs Go to TypeScript to Go round trips. Go, Task, pnpm and Node.js 18 or newer are required for these commands. The package uses CommonJS so existing Node.js consumers can load the bundled JSON Schema without import attributes.

Use the parser matching the wire boundary:

```typescript
import { parseNode, parseStatement, parseUIR } from '@flanksource/uir';

const target = parseNode({ node_kind: 'ref', method: 'Run' });
const call = parseStatement({ statement_type: 'dispatch_call', candidates: [target] });
const document = parseUIR([target]);
```

`parseNode(value)` validates a polymorphic node and requires `node_kind`. `parseStatement(value)` validates a polymorphic statement and requires `statement_type`; supported refinements are checked against the Go registry's hierarchy. `parseUIR(value)` validates either the hierarchical document object or the flat node array. Each accepts an unknown value, returns the generated TypeScript type, and throws `TypeError` with validation details for invalid input.

Validation preserves input fields and values, including metadata containing null, empty strings, empty lists, and false. Concrete nested fields use their actual wire shape and can omit discriminators where Go does. Serialize validated values with `JSON.stringify`.

To prepare a local package archive after checks:

```bash
pnpm --dir bindings/typescript pack --pack-destination ../../.tmp
```
