# UIR Python bindings

`flanksource-uir` provides generated dataclasses and wire codecs for Python 3.10 or newer, with no runtime dependencies. The generator is pinned to `datamodel-code-generator==0.83.0`. Publication and plugin adoption remain pending.

From the UIR repository root, generate, build and verify:

```bash
task bindings:python
task bindings:python:build
task bindings:python:check
```

Generation requires Go, Task and uv. Checks run on an actual Python 3.10 interpreter selected by uv; uv can install that interpreter if it is missing. The check compares generated dataclasses, model metadata and schema bytes without rewriting package sources. Binary and source archives are written to `.tmp/bindings/python/dist`; `BINDINGS_OUT` selects another preview directory.

Install the local wheel in a consumer environment:

```bash
python3 -m pip install .tmp/bindings/python/dist/flanksource_uir-1.0.0-py3-none-any.whl
python3 bindings/python/examples/consumer.py
```

The import package is `uir`. Use the codecs at wire boundaries:

```python
from uir import encode_node, parse_node
from uir.model import NodeNodeRef

target = parse_node({"node_kind": "ref", "method": "Run"})
assert isinstance(target, NodeNodeRef)
target.method = "Execute"
payload = encode_node(target)
```

`parse_node`, `parse_statement` and `parse_uir` accept JSON-compatible values and return generated dataclasses, recursively decoding nested models. Their corresponding `encode_*` functions return validated JSON-compatible values. Validation checks the canonical bundled schema before decoding and after encoding, including required fields, discriminators, refinements, types and unexpected fields. Invalid wire values raise `ValueError` with context.

Fields, aliases and model names come from the generator's emitted metadata and annotations. Empty strings, empty lists, false and metadata nulls are preserved. Optional fields with absent `None` defaults are omitted; required nullable fields and explicitly supplied nullable values retain null. Constructing a generated dataclass directly does not validate it: encode it through the appropriate codec before sending it.

The private validator implements the vocabulary emitted by UIR's schema generator, rather than accepting arbitrary JSON Schemas. Unsupported keywords, formats and references fail during package import. Tests compare refinement and scalar validation with an independent JSON Schema implementation, verify inline models and wire aliases, and exercise an installed wheel through all 48 registered kinds and six populated recursive Go fixtures. The standalone consumer is included in those checks. Checks explicitly reinstall the rebuilt wheel into owned test and consumer environments under `.tmp/bindings/python`; the consumer environment installs the wheel without dependencies. Ruff checks the handwritten Python code.
