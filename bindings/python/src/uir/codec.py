"""Convert wire values using the generator's model and field metadata."""

import json
from dataclasses import is_dataclass
from functools import cache
from importlib.resources import files
from typing import get_args, get_type_hints

from . import model
from ._schema import SCHEMA, matches, resolve, validate


_METADATA = json.loads(files(__package__).joinpath("models.json").read_text())
if _METADATA["version"] != 1:
    raise RuntimeError(f"Unsupported UIR model metadata version: {_METADATA['version']}")
_MODELS = {
    tuple(item["source_path"]): item for item in _METADATA["models"]
    if is_dataclass(getattr(model, item["class_name"]))
}
_CLASSES = {getattr(model, item["class_name"]): item for item in _MODELS.values()}


def parse_node(value) -> model.Node:
    return _parse(value, {"$ref": "#/$defs/Node"})


def parse_statement(value) -> model.Statement:
    return _parse(value, {"$ref": "#/$defs/Statement"})


def parse_uir(value) -> model.UIR:
    return _parse(value, SCHEMA)


def encode_node(value: model.Node):
    return _encode(value, {"$ref": "#/$defs/Node"})


def encode_statement(value: model.Statement):
    return _encode(value, {"$ref": "#/$defs/Statement"})


def encode_uir(value: model.UIR):
    return _encode(value, SCHEMA)


def _parse(value, schema):
    _check_json(value)
    validate(value, schema)
    return _decode(value, schema, ())


def _decode(value, schema, path, hint=None):
    if not isinstance(schema, dict):
        return value
    if "$ref" in schema:
        target, target_path = resolve(schema["$ref"])
        return _decode(value, target, target_path, hint)
    for keyword in ("oneOf", "anyOf"):
        if keyword in schema:
            for index, child in enumerate(schema[keyword]):
                if matches(value, child):
                    return _decode(value, child, path + (keyword, str(index)), hint)
            raise ValueError("Invalid UIR: no matching model branch")
    if isinstance(value, list) and "items" in schema:
        return [_decode(item, schema["items"], path + ("items",), hint) for item in value]
    if not isinstance(value, dict):
        return value
    properties = schema.get("properties", {})
    metadata = _MODELS.get(path)
    if metadata is None and properties:
        metadata = _CLASSES[_dataclass_hint(hint)]
    names = {field["alias"]: field["name"] for field in metadata["fields"]} if metadata else {}
    hints = _hints(getattr(model, metadata["class_name"])) if metadata else {}
    decoded = {
        key: _decode(item, properties.get(key, schema.get("additionalProperties", True)), path + ("properties", key), hints.get(names.get(key)))
        for key, item in value.items()
    }
    if metadata is None:
        return decoded
    result = getattr(model, metadata["class_name"])(**{names[key]: item for key, item in decoded.items()})
    result._uir_present = frozenset(decoded)
    return result


def _encode(value, schema):
    encoded = _wire(value)
    _check_json(encoded)
    validate(encoded, schema)
    return encoded


@cache
def _hints(cls):
    return get_type_hints(cls)


def _dataclass_hint(hint):
    if is_dataclass(hint):
        return hint
    candidates = [_dataclass_hint(child) for child in get_args(hint)]
    candidates = [child for child in candidates if child is not None]
    if len(candidates) > 1:
        raise RuntimeError(f"Ambiguous generated model for inline UIR object: {hint}")
    return candidates[0] if candidates else None


def _check_json(value):
    if isinstance(value, dict):
        for key, item in value.items():
            if not isinstance(key, str):
                raise ValueError("Invalid UIR: object keys must be strings")
            _check_json(item)
    elif isinstance(value, list):
        for item in value:
            _check_json(item)
    else:
        if value is not None and not isinstance(value, (str, bool, int, float)):
            raise ValueError(f"Invalid UIR: expected JSON value, received {value!r}")
        try:
            json.dumps(value, allow_nan=False)
        except (TypeError, ValueError) as error:
            raise ValueError(f"Invalid UIR: expected JSON value, received {value!r}") from error


def _wire(value):
    if is_dataclass(value) and not isinstance(value, type):
        if type(value) not in _CLASSES:
            raise ValueError(f"Invalid UIR: unknown model {type(value).__name__}")
        metadata = _CLASSES[type(value)]
        present = getattr(value, "_uir_present", frozenset())
        return {
            field["alias"]: _wire(getattr(value, field["name"]))
            for field in metadata["fields"]
            if getattr(value, field["name"]) is not None or field["required"] or field["alias"] in present
        }
    if isinstance(value, list):
        return [_wire(item) for item in value]
    if isinstance(value, dict):
        return {key: _wire(item) for key, item in value.items()}
    return value
