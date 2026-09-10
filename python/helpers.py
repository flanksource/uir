"""UIR helper functions for Python."""

try:
    from .uir_types import *
    from .enums import *
except ImportError:
    from uir_types import *
    from enums import *


def var(name: str) -> PackageVariableRef:
    """Create a variable reference."""
    return PackageVariableRef(name=name)


def var_expr(name: str) -> ExprStmt:
    """Create a variable reference expression."""
    v = var(name)
    return ExprStmt(
        type=ASTStatementType.EXPRESSION,
        variable=v
    )


def literal(value: str, field_type: RecordFieldType) -> LiteralStmt:
    """Create a literal statement."""
    return LiteralStmt(
        type=ASTStatementType.LITERAL,
        value=value,
        field_type=field_type
    )


def string_lit(s: str) -> LiteralStmt:
    """Create a string literal."""
    return literal(s, RecordFieldType.STRING)


def int_lit(i: int) -> LiteralStmt:
    """Create an integer literal."""
    return literal(str(i), RecordFieldType.NUMBER)


def bool_lit(b: bool) -> LiteralStmt:
    """Create a boolean literal."""
    return literal("true" if b else "false", RecordFieldType.BOOLEAN)


def lit_expr(value: str, field_type: RecordFieldType) -> ExprStmt:
    """Create a literal expression."""
    lit = literal(value, field_type)
    return ExprStmt(
        type=ASTStatementType.EXPRESSION,
        literal=lit
    )


def field(name: str, field_type: RecordFieldType) -> RecordField:
    """Create a record field."""
    return RecordField(
        name=name,
        field_type=field_type
    )


def new_condition(expr: ExprStmt) -> ConditionStmt:
    """Create a condition statement."""
    return ConditionStmt(
        type=ASTStatementType.CONDITION,
        expr=expr
    )


def binary_expr(left: ExprStmt, op: BinaryOp, right: ExprStmt) -> ExprStmt:
    """Create a binary expression."""
    binary = BinaryStmt(
        type=ASTStatementType.BINARY,
        left=left,
        op=op,
        right=right
    )
    return ExprStmt(
        type=ASTStatementType.EXPRESSION,
        binary=binary
    )


def unary_expr(op: UnaryOp, operand: ExprStmt) -> ExprStmt:
    """Create a unary expression."""
    unary = UnaryStmt(
        type=ASTStatementType.UNARY,
        operator=op,
        operand=operand
    )
    return ExprStmt(
        type=ASTStatementType.EXPRESSION,
        unary=unary
    )


def new_return(value: ExprStmt) -> ReturnStmt:
    """Create a return statement."""
    return ReturnStmt(
        type=ASTStatementType.RETURN,
        value=value
    )


def new_break() -> BreakStmt:
    """Create a break statement."""
    return BreakStmt(type=ASTStatementType.BREAK)


def new_continue() -> ContinueStmt:
    """Create a continue statement."""
    return ContinueStmt(type=ASTStatementType.CONTINUE)


def new_throw(exception: ExprStmt) -> ThrowStmt:
    """Create a throw statement."""
    return ThrowStmt(
        type=ASTStatementType.THROW,
        exception=exception
    )


def cast_expr(expr: ExprStmt, target_type: str) -> ExprStmt:
    """Create a cast expression."""
    cast = CastStmt(
        type=ASTStatementType.CAST,
        expr=expr,
        target_type=target_type
    )
    return ExprStmt(
        type=ASTStatementType.EXPRESSION,
        cast=cast
    )
