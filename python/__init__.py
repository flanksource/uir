"""Universal Intermediate Representation (UIR) for Python."""

from .uir_types import *
from .enums import *
from .builders import *
from .helpers import *
from .serialization import marshal_modules, to_json, to_json_dict

__all__ = [
    # Main functions
    'marshal_modules',
    'to_json',
    'to_json_dict',

    # Types
    'ModuleNode',
    'PackageNode',
    'TypedNode',
    'MethodNode',
    'RecordField',
    'ASTRecord',
    'ASTEndpoint',
    'ASTError',

    # Statements
    'Statement',
    'BlockStmt',
    'IfStmt',
    'ForStmt',
    'WhileStmt',
    'SwitchStmt',
    'TryStmt',
    'AssignmentStmt',
    'ReturnStmt',
    'BreakStmt',
    'ContinueStmt',
    'ThrowStmt',
    'ExprStmt',
    'ConditionStmt',
    'MethodCallStmt',
    'EndpointCallStmt',
    'RecordReadStmt',
    'RecordWriteStmt',

    # Builders
    'PackageBuilder',
    'TypeBuilder',
    'MethodBuilder',
    'BlockBuilder',
    'IfBuilder',
    'AssignmentBuilder',
    'MethodCallBuilder',
    'EndpointCallBuilder',
    'RecordReadBuilder',
    'RecordWriteBuilder',
    'ForBuilder',
    'WhileBuilder',
    'SwitchBuilder',
    'TryBuilder',

    # Helpers
    'var',
    'var_expr',
    'literal',
    'string_lit',
    'int_lit',
    'bool_lit',
    'lit_expr',
    'field',
    'new_condition',
    'binary_expr',
    'unary_expr',
    'new_return',
    'new_break',
    'new_continue',
    'new_throw',

    # Enums
    'Visibility',
    'AssignmentOp',
    'BinaryOp',
    'UnaryOp',
    'RecordFieldType',
    'RecordType',
    'ExpressionType',
    'EndpointType',
    'ASTStatementType',
]
