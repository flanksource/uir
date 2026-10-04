---
cwd: /Users/moshe/go/src/github.com/flanksource/uir-schema-bindings
---

# Generated language SDK conformance

All gates must pass before generated SDKs replace the handwritten plugin models. Python's approved minimum is 3.10, with a dependency-free runtime; its gate includes an installed wheel, malformed payloads, schema parity, lint and Go round trips.

| Name | Command | Exit Code | CEL Validation |
|---|---|---|---|
| TypeScript SDK drift, package build, consumer and Go round trips | task bindings:typescript:check | 0 | exitCode == 0 |
| Python 3.10 SDK drift, installed wheel, consumer, validation, lint and Go round trips | task bindings:python:check | 0 | exitCode == 0 |
| Java SDK archive, consumer, validation and Go round trips | task bindings:java:check | 0 | exitCode == 0 |
