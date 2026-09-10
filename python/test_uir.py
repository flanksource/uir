#!/usr/bin/env python3
"""Test UIR Python implementation by building a complete program and exporting to JSON."""

import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from builders import *
from helpers import *
from serialization import marshal_modules


def build_test_program() -> ModuleNode:
    """Build the same test program as the Go implementation."""

    # Build GetUser method
    get_user = (
        MethodBuilder("GetUser")
        .with_visibility(Visibility.PUBLIC)
        .with_param(field("userId", RecordFieldType.STRING))
        .with_return(field("user", RecordFieldType.OBJECT))
        .with_return(field("error", RecordFieldType.OBJECT))
        .with_statement(
            IfBuilder(binary_expr(
                var_expr("userId"),
                BinaryOp.EQUAL,
                lit_expr("", RecordFieldType.STRING)
            ))
            .with_then(
                BlockBuilder()
                .with_statement(new_return(lit_expr("null", RecordFieldType.OBJECT)))
                .build()
            )
            .build()
        )
        .with_statement(
            AssignmentBuilder("user", ExprStmt(
                type=ASTStatementType.EXPRESSION,
                record_read=RecordReadBuilder(RecordType.TABLE, "users")
                    .with_expression(ExpressionType.SQL, "SELECT * FROM users WHERE id = ?")
                    .with_argument("id", var_expr("userId"))
                    .build()
            ))
            .build()
        )
        .with_statement(
            IfBuilder(binary_expr(
                var_expr("user"),
                BinaryOp.NOT_EQUAL,
                lit_expr("null", RecordFieldType.OBJECT)
            ))
            .with_then(
                BlockBuilder()
                .with_statement(
                    MethodCallBuilder("cache.Set")
                    .with_positional_arg(var_expr("userId"))
                    .with_positional_arg(var_expr("user"))
                    .build()
                )
                .build()
            )
            .build()
        )
        .with_statement(new_return(var_expr("user")))
        .build()
    )

    # Build CreateUser method
    create_user = (
        MethodBuilder("CreateUser")
        .with_visibility(Visibility.PUBLIC)
        .with_param(field("userData", RecordFieldType.OBJECT))
        .with_return(field("userId", RecordFieldType.STRING))
        .with_return(field("error", RecordFieldType.OBJECT))
        .with_statement(
            TryBuilder()
            .with_body(
                BlockBuilder()
                .with_statement(
                    RecordWriteBuilder(RecordType.TABLE, "users")
                    .with_argument("data", var_expr("userData"))
                    .build()
                )
                .with_statement(
                    EndpointCallBuilder(EndpointType.KAFKA, "user-created")
                    .with_positional_arg(var_expr("userData"))
                    .build()
                )
                .with_statement(new_return(var_expr("userData.id")))
                .build()
            )
            .with_catch(
                BlockBuilder()
                .with_statement(
                    MethodCallBuilder("logger.Error")
                    .with_positional_arg(var_expr("err"))
                    .build()
                )
                .with_statement(new_return(lit_expr("", RecordFieldType.STRING)))
                .build()
            )
            .build()
        )
        .build()
    )

    # Build SearchUsers method - demonstrates new string operators and cast
    search_users = (
        MethodBuilder("SearchUsers")
        .with_visibility(Visibility.PUBLIC)
        .with_param(field("query", RecordFieldType.STRING))
        .with_param(field("maxPrice", RecordFieldType.STRING))
        .with_return(field("users", RecordFieldType.ARRAY))
        .with_return(field("error", RecordFieldType.OBJECT))
        .with_statement(
            AssignmentBuilder("priceInt", cast_expr(var_expr("maxPrice"), "integer"))
            .build()
        )
        .with_statement(
            IfBuilder(binary_expr(
                var_expr("query"),
                BinaryOp.LIKE,
                lit_expr("%john%", RecordFieldType.STRING)
            ))
            .with_then(
                BlockBuilder()
                .with_statement(
                    RecordReadBuilder(RecordType.TABLE, "users")
                    .with_expression(ExpressionType.SQL, "SELECT * FROM users WHERE name LIKE ?")
                    .with_argument("pattern", var_expr("query"))
                    .build()
                )
                .build()
            )
            .build()
        )
        .with_statement(
            IfBuilder(binary_expr(
                var_expr("query"),
                BinaryOp.STARTS_WITH,
                lit_expr("admin", RecordFieldType.STRING)
            ))
            .with_then(
                BlockBuilder()
                .with_statement(
                    RecordReadBuilder(RecordType.TABLE, "users")
                    .with_expression(ExpressionType.SQL, "SELECT * FROM users WHERE role STARTS WITH ?")
                    .with_argument("prefix", var_expr("query"))
                    .build()
                )
                .build()
            )
            .build()
        )
        .with_statement(
            IfBuilder(binary_expr(
                var_expr("query"),
                BinaryOp.CONTAINS,
                lit_expr("@example.com", RecordFieldType.STRING)
            ))
            .with_then(
                BlockBuilder()
                .with_statement(
                    RecordReadBuilder(RecordType.TABLE, "users")
                    .with_expression(ExpressionType.SQL, "SELECT * FROM users WHERE email CONTAINS ?")
                    .with_argument("substring", var_expr("query"))
                    .build()
                )
                .build()
            )
            .build()
        )
        .with_statement(new_return(var_expr("users")))
        .build()
    )

    # Build UserService type
    user_service = (
        TypeBuilder("UserService")
        .with_visibility(Visibility.PUBLIC)
        .with_variable(field("db", RecordFieldType.OBJECT))
        .with_variable(field("cache", RecordFieldType.OBJECT))
        .with_method(get_user)
        .with_method(create_user)
        .with_method(search_users)
        .build()
    )

    # Build package
    pkg = (
        PackageBuilder("com.example.service")
        .with_module("github.com/example/service")
        .with_path("internal/service/user_service.go")
        .with_variable(field("config", RecordFieldType.OBJECT))
        .with_variable(field("logger", RecordFieldType.OBJECT))
        .with_type(user_service)
        .with_function(
            MethodBuilder("init")
            .with_statement(
                AssignmentBuilder("config", lit_expr("config.yaml", RecordFieldType.STRING))
                .build()
            )
            .build()
        )
        .build()
    )

    # Build module
    module = ModuleNode(
        module="github.com/example/service",
        packages=[pkg]
    )

    return module


def main():
    """Main entry point."""
    try:
        module = build_test_program()
        json_output = marshal_modules([module])
        print(json_output)
        return 0
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
