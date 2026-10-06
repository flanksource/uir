from dataclasses import is_dataclass

import pytest

from uir import encode_node, encode_statement, encode_uir, parse_node, parse_statement, parse_uir
from uir.model import NodeNodeRef, StatementMethodCallStmt


@pytest.mark.parametrize("parse,encode,model,value", [
    (parse_node, encode_node, NodeNodeRef, {"node_kind": "ref", "method": "Run"}),
    (parse_statement, encode_statement, StatementMethodCallStmt, {
        "statement_type": "call", "Method": {"node_kind": "ref"}, "statement_refinement": "call:custom",
        "properties": {"null": None, "empty": "", "false": False, "list": []},
    }),
])
def test_roundtrip_uses_generated_dataclasses(parse, encode, model, value):
    decoded = parse(value)
    assert is_dataclass(decoded)
    assert isinstance(decoded, model)
    assert encode(decoded) == value


@pytest.mark.parametrize("value", [
    {},
    {"statement_type": "unknown"},
    {"statement_type": "call", "Method": {}, "statement_refinement": "unknown"},
    {"statement_type": "call", "Method": {}, "statement_refinement": "call:api"},
    {"statement_type": "call", "Method": {}, "statement_refinement": False},
    {"statement_type": "call", "Method": {}, "unexpected": True},
    {"statement_type": "call", "Method": {"node_kind": "unknown"}},
    {"statement_type": "raw"},
])
def test_rejects_invalid_wire_values(value):
    with pytest.raises(ValueError, match="Invalid UIR"):
        parse_statement(value)


def test_rejects_invalid_model_state_on_encode():
    with pytest.raises(ValueError, match="Invalid UIR"):
        encode_node(NodeNodeRef(node_kind="unknown"))


@pytest.mark.parametrize("value", [
    {"statement_type": "call", "arguments": [{"name": "input", "value": {"content": "text"}}]},
    {"statement_type": "control:switch", "cases": [{"body": {"children": []}}]},
])
def test_inline_objects_decode_to_generated_dataclasses(value):
    decoded = parse_statement(value)
    children = getattr(decoded, "arguments" if "arguments" in value else "cases")
    assert is_dataclass(children[0])
    assert encode_statement(decoded) == value


@pytest.mark.parametrize("value", [float("nan"), float("inf"), {1: "integer key"}, {"nested": object()}, ("tuple",)])
def test_rejects_non_json_metadata(value):
    with pytest.raises(ValueError, match="Invalid UIR"):
        parse_node({"node_kind": "method", "properties": {"invalid": value}})


def test_keyword_alias_required_null_and_timestamp_roundtrip():
    value = {
        "statement_type": "control:if",
        "else": {"children": [{"statement_type": "assignment:template", "strings": None}]},
        "last_modified": "2026-10-04T12:00:00.123456789+02:00",
    }
    decoded = parse_statement(value)
    assert is_dataclass(decoded.else_)
    assert encode_statement(decoded) == value


@pytest.mark.parametrize("value", [
    {"functions": [{"method": "Run"}]},
    [{"node_kind": "ref", "method": "Run"}],
])
def test_document_roundtrip(value):
    assert encode_uir(parse_uir(value)) == value
