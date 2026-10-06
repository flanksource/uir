# Python binding generator decision

Status: approved on 2026-10-04. The user selected raising the Python SDK minimum to 3.10 and updating `datamodel-code-generator` to 0.83.0. Standard-library dataclasses and a dependency-free runtime remain required. No custom generator template is used.

The original ownership plan preserved Python 3.8. The generator probes below established the choices before this runtime change was approved. The SDK now declares `requires-python = ">=3.10"`, and its checks run against an actual Python 3.10 interpreter.

## Correct fix

The generator must quote forward references inside evaluated type aliases, including nested containers, while emitting Python 3.8 syntax and imports. Its pinned `RootModel` uses `root.jinja2`, which currently assigns `field.type_hint` directly. Future annotations defer class annotations but do not defer an alias assignment such as `UIR = Union[UIRModel, List[Node]]`. The fix belongs in the generator's alias emission, with recursive-union tests.

## Attempted

All probes used the canonical schema with `--output-model-type dataclasses.dataclass --disable-timestamp --enum-field-as-literal all` and then executed the generated module with `python3`. Outputs remain under `.tmp/bindings/python`.

| Generator | Target | Result |
|---|---|---|
| 0.25.9 | 3.8 | Generation succeeds; import fails on undefined `UIRModel` |
| 0.26.5 | 3.8 | Same undefined alias |
| 0.27.3 | 3.8 | Same undefined alias |
| 0.28.5 | 3.8 | Target rejected; minimum supported target is 3.9 |
| 0.31.2 | 3.8 | Target rejected |
| 0.31.2 | 3.9 | Generation succeeds; import still fails on undefined `UIRModel` |
| 0.83.0 | 3.8 / 3.9 | Targets rejected; minimum supported target is 3.10 |
| 0.83.0 | 3.10 | Generation and module import succeed; full SDK conformance has not been implemented for this probe |

Earlier probes with distinct root names, collapsed root models, retained model order and the projected OpenAPI input also produced unresolved aliases. No generated source has been patched and no schema constraint has been relaxed.

## Unselected workaround

Keep generator 0.25.9 and Python 3.8, and supply a repository-owned `root.jinja2` override using the generator's supported custom-template interface. It would quote referenced model names in alias expressions while leaving field generation and wire definitions with the generator. This adds a maintained template tied to generator internals. It needs a `WORKAROUND` comment identifying the alias-emission defect and its upstream replacement, a recursive-union regression fixture, and actual Python 3.8 runtime verification before adoption.

This workaround was not selected, written or applied.

## Selected implementation

Python 3.10 is the SDK minimum and the generator pin is 0.83.0. Python 3.8 and 3.9 consumers must upgrade when adopting the SDK. The package owns generated dataclasses, generator-emitted model/field metadata, the canonical schema and generic wire codecs. Drift checks compare all generated artifacts without rewriting owned sources. Local wheel and source archives are built, then the wheel is explicitly installed into owned test and dependency-free consumer environments.

The SDK codec rejects malformed discriminators, refinements and model state; preserves populated recursive structures, null/empty/false values and aliases; and validates the schema vocabulary emitted by UIR. An independent JSON Schema validator checks refinement/scalar parity as a test-only dependency. Installed-wheel checks pass 676 Python tests, all 54 Go round trips, the documented consumer, and Ruff lint. Plugin adoption, broader optional-field fixtures and publication remain separate phase 3 work.

## Why now

The available pins supporting the original runtime failed at import. Raising the minimum uses the supported generator output and avoids maintaining a local template override. The consumer runtime change was explicitly approved before implementation.
