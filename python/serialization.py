"""UIR JSON serialization for Python."""

import json
from dataclasses import fields, is_dataclass
from typing import Any, Optional, get_args
from datetime import datetime
from enum import Enum

try:
    from .uir_types import *
    from .enums import *
    from .statement_kinds import REGISTERED_KINDS, CROSS_HIERARCHY
except ImportError:
    from uir_types import *
    from enums import *
    from statement_kinds import REGISTERED_KINDS, CROSS_HIERARCHY


def _pop_present(d: dict, moves: dict) -> dict:
    """Move each present key of d named in moves to a new dict under its new name."""
    return {new: d.pop(old) for old, new in moves.items() if old in d}


# Go's statements name the node they call, read or write through a Node-typed
# field, encoded as a node object stamped with its node_kind; these dataclasses
# keep those fields flat, so the encoder regroups them into the Go shape.
def _method_call_wire(d: dict) -> dict:
    ref = _pop_present(d, {"module": "module", "package": "package", "type": "type", "method": "method"})
    if ref:
        d["Method"] = {"node_kind": "ref", **ref}
    return d


def _endpoint_call_wire(d: dict) -> dict:
    endpoint = _pop_present(d, {"environment": "module", "id": "method", "endpointType": "endpointType"})
    if endpoint:
        d["endpoint"] = {"node_kind": "endpoint", "node_type": "endpoint", **endpoint}
    return d


def _record_access_wire(d: dict) -> dict:
    record = _pop_present(d, {"environment": "module", "id": "type"})
    if record:
        d["Record"] = {"node_kind": "ref", "node_type": "record", **record}
    expression = _pop_present(d, {"expressionType": "type", "expression": "expression"})
    if expression:
        d["expression"] = expression
    return d


_WIRE_SHAPES = {
    "MethodCallStmt": _method_call_wire,
    "EndpointCallStmt": _endpoint_call_wire,
    "RecordReadStmt": _record_access_wire,
    "RecordWriteStmt": _record_access_wire,
}


def _registered_kind(cls) -> Optional[str]:
    """The kind Go registers a statement dataclass under: its type field's default."""
    for f in fields(cls):
        if f.name == "type" and isinstance(f.default, ASTStatementType):
            return f.default.value
    return None


# Refinements resolve against every kind Go registers (statement_kinds.py is
# generated from its registry), not only the kinds these dataclasses model, so a
# type that names a Go-only kind such as assignment:object is refused here as Go
# would refuse it. A dataclass whose kind Go does not register could never decode.
_UNREGISTERED = sorted(
    kind
    for kind in (_registered_kind(globals()[ref.__forward_arg__]) for ref in get_args(Statement))
    if kind not in REGISTERED_KINDS
)
if _UNREGISTERED:
    raise ImportError(f"statement kinds {_UNREGISTERED} are not registered in Go; regenerate statement_kinds.py")


def _stamp_statement_type(d: dict, kind: str, carried: Optional[str]) -> dict:
    """Stamp the registered kind under statement_type, and a Type refined past it
    under statement_refinement, as Go's statement registry writes them."""
    if carried and carried != kind:
        _check_refinement(kind, carried)
        d["statement_refinement"] = carried
    d["statement_type"] = kind
    return d


def _check_refinement(kind: str, refined: str) -> None:
    """Refuse a Type the Go decoder would not read back as a statement of kind."""
    if refined in CROSS_HIERARCHY.get(kind, ()):
        return
    resolved = refined
    while resolved and resolved not in REGISTERED_KINDS:
        resolved = resolved.rpartition(":")[0]
    if resolved != kind:
        raise ValueError(
            f"statement_refinement {refined!r} does not refine {kind!r}: "
            f"it resolves to {resolved or 'no registered kind'!r}"
        )


class UIREncoder(json.JSONEncoder):
    """Custom JSON encoder for UIR types."""

    def default(self, obj):
        if is_dataclass(obj) or isinstance(obj, (datetime, Enum)):
            return self._encode(obj)
        return super().default(obj)

    def _encode(self, value):
        """Encode a value bottom-up, keeping each dataclass's type for _WIRE_SHAPES."""
        if is_dataclass(value) and not isinstance(value, type):
            raw = {f.name: self._encode(getattr(value, f.name)) for f in fields(value)}
            kind = _registered_kind(type(value))
            carried = raw.pop("type") if kind else None
            d = self._convert_dataclass(raw)
            shape = _WIRE_SHAPES.get(type(value).__name__)
            if shape:
                d = shape(d)
            return _stamp_statement_type(d, kind, carried) if kind else d
        if isinstance(value, list):
            return [self._encode(item) for item in value]
        if isinstance(value, dict):
            return {key: self._encode(item) for key, item in value.items()}
        if isinstance(value, Enum):
            return value.value
        if isinstance(value, datetime):
            return value.isoformat()
        return value

    def _convert_dataclass(self, raw_dict: dict) -> dict:
        """Rename an encoded dataclass's keys to their JSON names and drop empty values."""
        d = {}
        for key, value in raw_dict.items():
            if value is None:
                continue
            if isinstance(value, list) and len(value) == 0:
                continue
            if isinstance(value, dict) and len(value) == 0:
                continue
            if isinstance(value, str) and value == "" and key not in ['name', 'id']:
                continue

            d[self._to_camel_case(key)] = value
        return d

    @staticmethod
    def _to_camel_case(snake_str: str) -> str:
        """Convert snake_case to camelCase."""
        components = snake_str.split('_')
        if len(components) == 1:
            return components[0]
        # Handle special cases
        if snake_str == 'else_block':
            return 'else'
        if snake_str == 'type_name':
            return 'type'
        if snake_str == 'field_type':
            return 'fieldType'
        if snake_str == 'endpoint_type':
            return 'endpointType'
        if snake_str == 'record_type':
            return 'recordType'
        if snake_str == 'expression_type':
            return 'expressionType'
        if snake_str == 'error_type':
            return 'errorType'
        if snake_str == 'default_value':
            return 'defaultValue'
        if snake_str == 'read_only':
            return 'readOnly'
        if snake_str == 'write_only':
            return 'writeOnly'
        if snake_str == 'start_line':
            return 'startLine'
        if snake_str == 'end_line':
            return 'endLine'
        if snake_str == 'record_reference_type':
            return 'recordReferenceType'
        if snake_str == 'min_length':
            return 'minLength'
        if snake_str == 'max_length':
            return 'maxLength'
        if snake_str == 'max_items':
            return 'maxItems'
        if snake_str == 'min_items':
            return 'minItems'
        if snake_str == 'min_properties':
            return 'minProperties'
        if snake_str == 'max_properties':
            return 'maxProperties'
        if snake_str == 'unique_items':
            return 'uniqueItems'
        if snake_str == 'sorted_items':
            return 'sortedItems'
        if snake_str == 'min_exclusive':
            return 'minExclusive'
        if snake_str == 'max_exclusive':
            return 'maxExclusive'
        if snake_str == 'min_date':
            return 'minDate'
        if snake_str == 'max_date':
            return 'maxDate'
        if snake_str == 'non_empty':
            return 'nonEmpty'
        if snake_str == 'unique_fields':
            return 'uniqueFields'
        if snake_str == 'min_records':
            return 'minRecords'
        if snake_str == 'max_records':
            return 'maxRecords'
        if snake_str == 'any_of':
            return 'anyOf'
        if snake_str == 'one_of':
            return 'oneOf'
        if snake_str == 'all_of':
            return 'allOf'
        if snake_str == 'none_of':
            return 'noneOf'
        if snake_str == 'init_functions':
            return 'initFunctions'
        if snake_str == 'method_call':
            return 'method_call'
        if snake_str == 'endpoint_call':
            return 'endpoint_call'
        if snake_str == 'record_read':
            return 'record_read'
        if snake_str == 'record_write':
            return 'record_write'

        # Default camelCase conversion
        return components[0] + ''.join(x.title() for x in components[1:])


def to_json(obj: Any, indent: int = 2) -> str:
    """Serialize UIR object to JSON string."""
    return json.dumps(obj, cls=UIREncoder, indent=indent)


def to_json_dict(obj: Any) -> dict:
    """Serialize UIR object to JSON-compatible dict."""
    json_str = to_json(obj)
    return json.loads(json_str)


def marshal_modules(modules: List[ModuleNode]) -> str:
    """Marshal a list of ModuleNodes to JSON."""
    return to_json(modules)
