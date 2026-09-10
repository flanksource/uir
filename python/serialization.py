"""UIR JSON serialization for Python."""

import json
from dataclasses import asdict, is_dataclass
from typing import Any
from datetime import datetime
from enum import Enum

try:
    from .uir_types import *
    from .enums import *
except ImportError:
    from uir_types import *
    from enums import *


class UIREncoder(json.JSONEncoder):
    """Custom JSON encoder for UIR types."""

    def default(self, obj):
        if is_dataclass(obj):
            return self._convert_dataclass(asdict(obj))
        elif isinstance(obj, datetime):
            return obj.isoformat()
        elif isinstance(obj, Enum):
            return obj.value
        return super().default(obj)

    def _convert_dataclass(self, raw_dict: dict) -> dict:
        """Convert a dataclass dict to JSON-compatible dict with correct key names."""
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

            # Recursively convert nested dicts and lists
            value = self._convert_value(value)

            # Convert key name
            json_key = self._to_camel_case(key)

            # Map statement 'type' field to 'statement_type' for Go compatibility
            if key == 'type' and isinstance(value, str) and self._is_statement_type(value):
                json_key = 'statement_type'

            d[json_key] = value
        return d

    def _convert_value(self, value):
        """Recursively convert nested values."""
        if isinstance(value, dict):
            return self._convert_dataclass(value)
        elif isinstance(value, list):
            return [self._convert_value(item) for item in value]
        return value

    # Statement type values that should be serialized as "statement_type" key
    _STATEMENT_TYPE_PREFIXES = frozenset([
        'call', 'decl', 'ref', 'control', 'assignment', 'other', 'doc', 'test',
        'raw', 'foreign_key',
    ])

    @classmethod
    def _is_statement_type(cls, value: str) -> bool:
        """Check if a value is a statement type enum value."""
        prefix = value.split(':')[0] if ':' in value else value
        return prefix in cls._STATEMENT_TYPE_PREFIXES

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
