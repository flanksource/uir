import json
import sys
from dataclasses import is_dataclass

from uir import encode_node, encode_statement, parse_node, parse_statement


if sys.version_info[:2] != (3, 10):
    raise RuntimeError(f"Conformance requires Python 3.10, received {sys.version}")

cases = json.load(sys.stdin)
for case in cases:
    if case["hierarchy"] == "Node":
        decoded = parse_node(case["value"])
        encode = encode_node
    elif case["hierarchy"] == "Statement":
        decoded = parse_statement(case["value"])
        encode = encode_statement
    else:
        raise ValueError(f"Unknown UIR hierarchy: {case['hierarchy']}")
    if not is_dataclass(decoded):
        raise TypeError(f"Expected generated dataclass for {case['name']}")
    case["value"] = encode(decoded)
print(json.dumps(cases, allow_nan=False))
