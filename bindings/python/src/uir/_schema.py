"""Evaluate the schema vocabulary emitted by UIR's Go generator."""

import json
import math
import re
from datetime import datetime
from importlib.resources import files


SCHEMA = json.loads(files(__package__).joinpath("uir.schema.json").read_text())
_KEYWORDS = {
    "$defs", "$ref", "$schema", "additionalProperties", "anyOf", "const",
    "description", "enum", "format", "items", "not", "oneOf", "pattern",
    "properties", "required", "title", "type",
}
_TYPES = {
    "null": lambda value: value is None,
    "boolean": lambda value: type(value) is bool,
    "string": lambda value: isinstance(value, str),
    "integer": lambda value: type(value) is int or (
        type(value) is float and math.isfinite(value) and value.is_integer()
    ),
    "number": lambda value: type(value) is int or (type(value) is float and math.isfinite(value)),
    "array": lambda value: isinstance(value, list),
    "object": lambda value: isinstance(value, dict),
}


def resolve(reference):
    if not reference.startswith("#/"):
        raise RuntimeError(f"Unsupported UIR schema reference: {reference}")
    path = tuple(part.replace("~1", "/").replace("~0", "~") for part in reference[2:].split("/"))
    schema = SCHEMA
    for part in path:
        schema = schema[part]
    return schema, path


def check_vocabulary(schema):
    if isinstance(schema, bool):
        return
    unknown = schema.keys() - _KEYWORDS
    if unknown:
        raise RuntimeError(f"Unsupported UIR schema keywords: {sorted(unknown)}")
    if "$ref" in schema:
        resolve(schema["$ref"])
    if "type" in schema and schema["type"] not in _TYPES:
        raise RuntimeError(f"Unsupported UIR schema type: {schema['type']}")
    if "format" in schema and schema["format"] not in ("uuid", "date-time"):
        raise RuntimeError(f"Unsupported UIR schema format: {schema['format']}")
    for keyword in ("$defs", "properties"):
        for child in schema.get(keyword, {}).values():
            check_vocabulary(child)
    for keyword in ("anyOf", "oneOf"):
        for child in schema.get(keyword, []):
            check_vocabulary(child)
    for keyword in ("items", "not", "additionalProperties"):
        if keyword in schema:
            check_vocabulary(schema[keyword])


def matches(value, schema):
    try:
        validate(value, schema)
    except ValueError:
        return False
    return True


def validate(value, schema, path="$"):
    if schema is False:
        raise ValueError(f"Invalid UIR at {path}: forbidden value")
    if schema is True:
        return
    if "$ref" in schema:
        validate(value, resolve(schema["$ref"])[0], path)
    if "type" in schema and not _TYPES[schema["type"]](value):
        raise ValueError(f"Invalid UIR at {path}: expected {schema['type']}, received {value!r}")
    for keyword in ("const", "enum"):
        if keyword in schema:
            allowed = [schema[keyword]] if keyword == "const" else schema[keyword]
            if not any(type(value) is type(item) and value == item for item in allowed):
                raise ValueError(f"Invalid UIR at {path}: expected {allowed!r}, received {value!r}")
    for keyword in ("anyOf", "oneOf"):
        if keyword in schema:
            count = sum(matches(value, child) for child in schema[keyword])
            if count == 0 or (keyword == "oneOf" and count != 1):
                raise ValueError(f"Invalid UIR at {path}: failed {keyword}, received {value!r}")
    if "not" in schema and matches(value, schema["not"]):
        raise ValueError(f"Invalid UIR at {path}: forbidden schema branch, received {value!r}")
    if isinstance(value, str):
        validate_string(value, schema, path)
    if isinstance(value, list) and "items" in schema:
        for index, item in enumerate(value):
            validate(item, schema["items"], f"{path}[{index}]")
    if isinstance(value, dict):
        validate_object(value, schema, path)


def validate_string(value, schema, path):
    if "pattern" in schema and re.search(schema["pattern"], value) is None:
        raise ValueError(f"Invalid UIR at {path}: pattern {schema['pattern']!r}")
    if schema.get("format") == "uuid":
        if re.fullmatch(r"[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}", value) is None:
            raise ValueError(f"Invalid UIR at {path}: expected uuid")
    if schema.get("format") == "date-time":
        matched = re.fullmatch(
            r"(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})"
            r"(?:\.\d+)?(?:[Zz]|[+-](?:[01]\d|2[0-3]):[0-5]\d)", value,
        )
        if matched is None:
            raise ValueError(f"Invalid UIR at {path}: expected date-time")
        try:
            datetime(*(int(part) for part in matched.groups()))
        except ValueError as error:
            raise ValueError(f"Invalid UIR at {path}: expected date-time") from error


def validate_object(value, schema, path):
    missing = set(schema.get("required", [])) - value.keys()
    if missing:
        raise ValueError(f"Invalid UIR at {path}: missing {sorted(missing)}")
    properties = schema.get("properties", {})
    additional = schema.get("additionalProperties", True)
    for key, item in value.items():
        if not isinstance(key, str):
            raise ValueError(f"Invalid UIR at {path}: expected string object key")
        validate(item, properties.get(key, additional), f"{path}.{key}")


check_vocabulary(SCHEMA)
