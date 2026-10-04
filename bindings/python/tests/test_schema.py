import pytest
from jsonschema import Draft202012Validator, FormatChecker

from uir._schema import SCHEMA, check_vocabulary, matches


@pytest.mark.parametrize("branch", SCHEMA["$defs"]["Statement"]["oneOf"])
@pytest.mark.parametrize("refinement", [
    "call", "call:custom", "call:api", "call:api:custom", "call:read:record:custom",
    "assignment", "assignment:literal:custom", "ref:var", "ref:var:custom",
    "control:block", "control:block:custom", "dispatch_call:custom", "unknown",
    "call\n", "call:custom\n", False, None, "",
])
def test_refinements_match_independent_json_schema_validation(branch, refinement):
    definition = SCHEMA["$defs"][branch["$ref"].rsplit("/", 1)[1]]
    constraint = definition["properties"]["statement_refinement"]
    expected = Draft202012Validator(constraint).is_valid(refinement)
    assert matches(refinement, constraint) == expected


@pytest.mark.parametrize("constraint,values", [
    ({"type": "integer"}, [False, 0, 1.0, 1.5, "1", None]),
    ({"type": "number"}, [False, 0, 1.5, "1", None]),
    ({"type": "string", "format": "uuid"}, [
        "00000000-0000-0000-0000-000000000000", "not-a-uuid", "00000000000000000000000000000000",
    ]),
    ({"type": "string", "format": "date-time"}, [
        "2026-10-04T12:00:00Z", "2026-10-04T12:00:00.123456789+02:00",
        "2026-10-04t12:00:00z", "2026-02-30T12:00:00Z", "2026-10-04", "2026-10-04T12:00:00",
    ]),
])
def test_scalar_constraints_match_independent_json_schema_validation(constraint, values):
    validator = Draft202012Validator(constraint, format_checker=FormatChecker())
    for value in values:
        assert matches(value, constraint) == validator.is_valid(value), repr(value)


@pytest.mark.parametrize("constraint", [
    {"minimum": 1}, {"format": "email"}, {"$ref": "https://example.org/schema"},
])
def test_rejects_unsupported_schema_vocabulary(constraint):
    with pytest.raises(RuntimeError, match="Unsupported UIR schema"):
        check_vocabulary(constraint)
