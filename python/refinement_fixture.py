#!/usr/bin/env python3
"""Refinement fixtures for cross_language_refinement_test.go.

With no argument, print statements whose type is refined past their registered
kind, each beside the type it was built with, for Go to decode. With "verdicts",
read [{"kind", "refinement"}] pairs from stdin and print whether the encoder
accepts each refinement, for Go to compare with its own registry."""

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from uir_types import *
from enums import *
from serialization import to_json_dict, _check_refinement

STATEMENTS = [
    MethodCallStmt(type=ASTStatementType.CALL_PACKAGE, package="billing", method="Invoice"),
    MethodCallStmt(type=ASTStatementType.FILE_READ, method="Open"),
    MethodCallStmt(method="Unrefined"),
    EndpointCallStmt(type=ASTStatementType.HTTP_CALL, id="getUser"),
    RecordReadStmt(id="users", expression="id = 1"),
    BlockStmt(type=ASTStatementType.DOC),
]

# A type that does not refine the statement's kind cannot be decoded as that
# statement, so the encoder refuses it. assignment:object is registered in Go
# (ObjectLiteralStmt) though no dataclass here models it, so it is a kind of its
# own rather than a refinement of assignment.
REFUSED = [
    MethodCallStmt(type=ASTStatementType.IF),
    MethodCallStmt(type=ASTStatementType.HTTP_CALL),
    AssignmentStmt(type=ASTStatementType.OBJECT_LITERAL),
]


def verdicts(pairs):
    """Whether the encoder accepts each (kind, refinement) pair."""
    out = []
    for pair in pairs:
        try:
            _check_refinement(pair["kind"], pair["refinement"])
        except ValueError:
            out.append(False)
            continue
        out.append(True)
    return out


def main():
    if sys.argv[1:] == ["verdicts"]:
        print(json.dumps(verdicts(json.load(sys.stdin))))
        return
    if sys.argv[1:]:
        sys.exit(f"usage: {sys.argv[0]} [verdicts]")
    for stmt in REFUSED:
        try:
            to_json_dict(stmt)
        except ValueError:
            continue
        sys.exit(f"encoding {stmt!r} did not refuse its type")
    cases = [{"want": stmt.type.value, "statement": to_json_dict(stmt)} for stmt in STATEMENTS]
    print(json.dumps(cases, indent=2))


if __name__ == "__main__":
    main()
