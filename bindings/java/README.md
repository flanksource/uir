# UIR Java bindings

The Java SDK contains models generated from UIR's mechanically projected OpenAPI catalog and a `WireCodec` that validates JSON against the canonical UIR JSON Schema. The local artifact coordinates are `com.flanksource:uir:1.0.0`. Publication and plugin adoption are pending.

From the UIR repository root, build and verify the SDK:

```bash
task bindings:java:build
task bindings:java:check
```

These commands require Go, Task, JDK 17 or newer, and curl. The included Gradle wrapper uses Gradle 8.14.4 and resolves Maven dependencies with the checked-in lockfile. `bindings:java:build` checks schema drift, generates models, builds binary and source JARs, prepares a dependency POM, and copies runtime dependencies. Generated models and archives remain under `.tmp/bindings/java`; set `BINDINGS_OUT` to choose another preview directory. Generation removes its own previous generated sources before writing new ones, so renamed or deleted schemas cannot leave obsolete classes in the SDK.

The binary archive is `.tmp/bindings/java/libs/uir-1.0.0.jar`; the source archive is beside it. Runtime dependencies are in `.tmp/bindings/java/runtime`, and the POM is `.tmp/bindings/java/publications/uir/pom-default.xml`. These are prepared locally without publishing or installing them into a Maven repository.

Use `WireCodec` at JSON boundaries:

```java
WireCodec codec = new WireCodec();
ObjectMapper json = new ObjectMapper();
Node target = codec.parseNode(json.readTree("{\"node_kind\":\"ref\",\"method\":\"Run\"}"));
JsonNode encoded = codec.encodeNode(target);
```

The imports and complete runnable example are in [examples/Consumer.java](examples/Consumer.java). The conformance gate compiles and runs this example against the built archive.

`parseNode(JsonNode)` returns a generated `Node` subtype and requires a valid `node_kind`. `parseStatement(JsonNode)` returns a generated `Statement` subtype and checks `statement_type`, required fields, refinements and nested structures. `encodeNode(Node)` and `encodeStatement(Statement)` return validated JSON. Invalid input or invalid generated model state throws `IllegalArgumentException` with context. Use the codec for both decoding and encoding: raw Jackson conversion alone does not enforce JSON Schema constraints.

Optional model fields are omitted when absent. Empty strings, empty lists and false values are retained, and null values inside metadata maps are preserved. The SDK leaves SLF4J provider selection to the consuming application.

`task bindings:java:check` runs exact Go to Java to Go round trips for all 48 registered kinds and six populated recursive cases, discriminator rejection, malformed statement rejection, and the consumer example. The schema is bundled in the binary archive, so validation needs no schema service.

After intentionally changing dependencies, refresh the lockfile and rerun checks:

```bash
task bindings:java:build JAVA_GRADLE_FLAGS=--write-locks
task bindings:java:check
```
