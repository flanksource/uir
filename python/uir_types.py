"""UIR core types for Python."""

from dataclasses import dataclass, field
from typing import Optional, List, Dict, Any, Union
from datetime import datetime

try:
    from .enums import *
except ImportError:
    from enums import *


@dataclass
class Location:
    path: str = ""
    start_line: int = 0
    end_line: int = 0


@dataclass
class SourceCode:
    location: Optional[Location] = None
    content: Optional[str] = None
    language: Optional[str] = None


@dataclass
class TypedValue:
    string: Optional[str] = None
    float: Optional[float] = None
    int: Optional[int] = None
    bool: Optional[bool] = None
    date: Optional[datetime] = None


@dataclass
class Annotation:
    module: str = ""
    package: str = ""
    type: str = ""
    value: Optional[TypedValue] = None
    values: Dict[str, TypedValue] = field(default_factory=dict)


@dataclass
class Comment:
    text: str = ""
    type: CommentType = CommentType.SINGLE_LINE
    location: Optional[Location] = None


@dataclass
class Metadata:
    annotations: List[Annotation] = field(default_factory=list)
    comments: List[Comment] = field(default_factory=list)
    properties: Dict[str, Any] = field(default_factory=dict)


@dataclass
class EnvironmentIdentifier:
    environment: str = ""
    urn: str = ""


@dataclass
class ModuleIdentifier:
    module: str = ""


@dataclass
class PackageIdentifier:
    module: str = ""
    package: str = ""


@dataclass
class TypeIdentifier:
    module: str = ""
    package: str = ""
    type: str = ""


@dataclass
class MethodIdentifier:
    module: str = ""
    package: str = ""
    type: str = ""
    method: str = ""


@dataclass
class StaticMethodIdentifier:
    module: str = ""
    package: str = ""
    method: str = ""


@dataclass
class EndpointIdentifier:
    environment: str = ""
    urn: str = ""
    id: str = ""
    endpoint_type: Optional[EndpointType] = None


@dataclass
class RecordIdentifier:
    environment: str = ""
    urn: str = ""
    id: str = ""
    record_type: Optional[RecordType] = None


@dataclass
class ScopedVariableRef:
    name: str = ""
    default_value: Optional[TypedValue] = None


@dataclass
class PackageVariableRef:
    name: str = ""
    module: str = ""
    package: str = ""
    default_value: Optional[TypedValue] = None


@dataclass
class Expression:
    expression_type: ExpressionType = ExpressionType.SQL
    expression: str = ""


@dataclass
class ValidationValue:
    string_value: Optional[str] = None
    number_value: Optional[float] = None
    bool_value: Optional[bool] = None
    array_value: List['ValidationValue'] = field(default_factory=list)
    object_value: Dict[str, 'ValidationValue'] = field(default_factory=dict)
    code: Optional[str] = None


@dataclass
class ValidationRuleOptions:
    message: Optional[str] = None
    groups: List[str] = field(default_factory=list)
    always: Optional[bool] = None
    each: Optional[bool] = None
    context: Dict[str, ValidationValue] = field(default_factory=dict)


@dataclass
class ValidationRule:
    provider: str = ""
    name: str = ""
    args: List[ValidationValue] = field(default_factory=list)
    options: Optional[ValidationRuleOptions] = None


@dataclass
class GeneratorHint:
    language: str = ""
    framework: str = ""
    properties: Dict[str, ValidationValue] = field(default_factory=dict)


@dataclass
class FieldValidation:
    min_length: Optional[int] = None
    max_length: Optional[int] = None
    max_items: Optional[int] = None
    min_items: Optional[int] = None
    min_properties: Optional[int] = None
    max_properties: Optional[int] = None
    unique_items: bool = False
    sorted_items: bool = False
    regex: str = ""
    enum: List[str] = field(default_factory=list)
    min: Optional[float] = None
    min_exclusive: Optional[float] = None
    max_exclusive: Optional[float] = None
    max: Optional[float] = None
    min_date: Optional[str] = None
    max_date: Optional[str] = None
    required: bool = False
    unique: bool = False
    non_empty: bool = False
    expressions: List[Expression] = field(default_factory=list)
    rules: List[ValidationRule] = field(default_factory=list)
    hints: List[GeneratorHint] = field(default_factory=list)


@dataclass
class RecordField:
    name: str = ""
    label: str = ""
    field_type: RecordFieldType = RecordFieldType.STRING
    default_value: Optional[TypedValue] = None
    read_only: bool = False
    write_only: bool = False
    validation: FieldValidation = field(default_factory=FieldValidation)
    visibility: Visibility = Visibility.NA
    annotations: List[Annotation] = field(default_factory=list)
    comments: List[Comment] = field(default_factory=list)
    location: Optional[Location] = None


@dataclass
class RecordValidation:
    unique_fields: List[str] = field(default_factory=list)
    min_records: Optional[int] = None
    max_records: Optional[int] = None
    any_of: List[str] = field(default_factory=list)
    one_of: List[str] = field(default_factory=list)
    all_of: List[str] = field(default_factory=list)
    none_of: List[str] = field(default_factory=list)
    expressions: List[Expression] = field(default_factory=list)
    hints: List[GeneratorHint] = field(default_factory=list)


@dataclass
class RecordReference:
    name: str = ""
    mapping: str = ""
    record_reference_type: Optional[RecordReferenceType] = None


@dataclass
class ID:
    id: str = ""
    name: str = ""
    location: Optional[Location] = None


@dataclass
class ASTRecord:
    id: str = ""
    name: str = ""
    extends: List['ASTRecord'] = field(default_factory=list)
    description: str = ""
    record_type: Optional[RecordType] = None
    fields: List[RecordField] = field(default_factory=list)
    examples: List[str] = field(default_factory=list)
    validation: RecordValidation = field(default_factory=RecordValidation)
    references: List[RecordReference] = field(default_factory=list)
    location: Optional[Location] = None


@dataclass
class ASTError:
    id: str = ""
    name: str = ""
    error_type: Optional[ASTErrorType] = None
    description: str = ""
    code: Optional[TypedValue] = None
    location: Optional[Location] = None


@dataclass
class ASTEndpoint:
    id: str = ""
    name: str = ""
    urn: str = ""
    endpoint_type: Optional[EndpointType] = None
    input: Optional[ASTRecord] = None
    output: Optional[ASTRecord] = None
    examples: List[str] = field(default_factory=list)
    errors: List[ASTError] = field(default_factory=list)
    location: Optional[Location] = None


# Forward declarations for statements
Statement = Union[
    'IfStmt', 'ForStmt', 'SwitchStmt', 'WhileStmt', 'AssignmentStmt',
    'BinaryStmt', 'UnaryStmt', 'CastStmt', 'LiteralStmt', 'VariableStmt', 'MethodCallStmt',
    'EndpointCallStmt', 'VariableDeclStmt', 'FunctionDeclStmt', 'RecordReadStmt',
    'RecordWriteStmt', 'ReturnStmt', 'BreakStmt', 'ContinueStmt', 'TupleStmt',
    'ThrowStmt', 'TryStmt', 'ConditionStmt', 'ExprStmt', 'BlockStmt'
]


@dataclass
class LiteralStmt:
    type: ASTStatementType = ASTStatementType.LITERAL
    value: Optional[str] = None
    field_type: RecordFieldType = RecordFieldType.STRING


@dataclass
class VariableStmt:
    type: ASTStatementType = ASTStatementType.VARIABLE
    name: str = ""


@dataclass
class BinaryStmt:
    type: ASTStatementType = ASTStatementType.BINARY
    left: Optional['ExprStmt'] = None
    op: Optional[BinaryOp] = None
    right: Optional['ExprStmt'] = None


@dataclass
class UnaryStmt:
    type: ASTStatementType = ASTStatementType.UNARY
    operator: Optional[UnaryOp] = None
    operand: Optional['ExprStmt'] = None


@dataclass
class CastStmt:
    type: ASTStatementType = ASTStatementType.CAST
    expr: Optional['ExprStmt'] = None
    target_type: Optional[str] = None


@dataclass
class Argument:
    name: Optional[str] = None
    value: Optional['ExprStmt'] = None


@dataclass
class MethodCallStmt:
    type: ASTStatementType = ASTStatementType.CALL
    module: str = ""
    package: str = ""
    type_name: str = ""
    method: str = ""
    receiver: Optional['ExprStmt'] = None
    arguments: List[Argument] = field(default_factory=list)


@dataclass
class EndpointCallStmt:
    type: ASTStatementType = ASTStatementType.CALL_API
    environment: str = ""
    id: str = ""
    endpoint_type: Optional[EndpointType] = None
    arguments: List[Argument] = field(default_factory=list)


@dataclass
class RecordReadStmt:
    type: ASTStatementType = ASTStatementType.RECORD_READ
    environment: str = ""
    id: str = ""
    record_type: Optional[RecordType] = None
    expression_type: Optional[ExpressionType] = None
    expression: str = ""
    arguments: List[Argument] = field(default_factory=list)


@dataclass
class RecordWriteStmt:
    type: ASTStatementType = ASTStatementType.RECORD_WRITE
    environment: str = ""
    id: str = ""
    record_type: Optional[RecordType] = None
    expression_type: Optional[ExpressionType] = None
    expression: str = ""
    arguments: List[Argument] = field(default_factory=list)


@dataclass
class TupleStmt:
    type: ASTStatementType = ASTStatementType.TUPLE
    elements: List['ExprStmt'] = field(default_factory=list)


@dataclass
class ExprStmt:
    type: ASTStatementType = ASTStatementType.EXPRESSION
    literal: Optional[LiteralStmt] = None
    variable: Optional[PackageVariableRef] = None
    binary: Optional[BinaryStmt] = None
    unary: Optional[UnaryStmt] = None
    cast: Optional[CastStmt] = None
    method_call: Optional[MethodCallStmt] = None
    endpoint_call: Optional[EndpointCallStmt] = None
    record_read: Optional[RecordReadStmt] = None
    tuple: Optional[TupleStmt] = None


@dataclass
class ConditionStmt:
    type: ASTStatementType = ASTStatementType.CONDITION
    expr: Optional[ExprStmt] = None


@dataclass
class BlockStmt:
    type: ASTStatementType = ASTStatementType.BLOCK
    variables: List[RecordField] = field(default_factory=list)
    children: List[Statement] = field(default_factory=list)


@dataclass
class IfStmt:
    type: ASTStatementType = ASTStatementType.IF
    condition: Optional[ConditionStmt] = None
    then: Optional[BlockStmt] = None
    else_block: Optional[BlockStmt] = None


@dataclass
class SwitchCase:
    condition: Optional[ConditionStmt] = None
    body: Optional[BlockStmt] = None


@dataclass
class SwitchStmt:
    type: ASTStatementType = ASTStatementType.SWITCH
    value: Optional[ExprStmt] = None
    cases: List[SwitchCase] = field(default_factory=list)


@dataclass
class AssignmentStmt:
    type: ASTStatementType = ASTStatementType.ASSIGNMENT
    target: Optional[PackageVariableRef] = None
    value: Optional[ExprStmt] = None
    op: AssignmentOp = AssignmentOp.ASSIGN


@dataclass
class ForStmt:
    type: ASTStatementType = ASTStatementType.LOOP_FOR
    init: Optional[AssignmentStmt] = None
    cond: Optional[ConditionStmt] = None
    body: Optional[BlockStmt] = None
    update: Optional[ExprStmt] = None


@dataclass
class WhileStmt:
    type: ASTStatementType = ASTStatementType.LOOP_WHILE
    condition: Optional[ConditionStmt] = None
    body: Optional[BlockStmt] = None


@dataclass
class TryStmt:
    type: ASTStatementType = ASTStatementType.TRY
    body: Optional[BlockStmt] = None
    catch: Optional[BlockStmt] = None


@dataclass
class ReturnStmt:
    type: ASTStatementType = ASTStatementType.RETURN
    value: Optional[ExprStmt] = None


@dataclass
class BreakStmt:
    type: ASTStatementType = ASTStatementType.BREAK


@dataclass
class ContinueStmt:
    type: ASTStatementType = ASTStatementType.CONTINUE


@dataclass
class ThrowStmt:
    type: ASTStatementType = ASTStatementType.THROW
    exception: Optional[ExprStmt] = None


@dataclass
class VariableDeclStmt:
    type: ASTStatementType = ASTStatementType.DECLARE_VARIABLE
    field: Optional[RecordField] = None


@dataclass
class FunctionDeclStmt:
    type: ASTStatementType = ASTStatementType.DECLARE_FUNCTION
    method: Optional['MethodNode'] = None


@dataclass
class MethodNode:
    module: str = ""
    package: str = ""
    type: str = ""
    method: str = ""
    visibility: Visibility = Visibility.NA
    params: List[RecordField] = field(default_factory=list)
    returns: List[RecordField] = field(default_factory=list)
    errors: List[ASTError] = field(default_factory=list)
    body: Optional[BlockStmt] = None
    annotations: List[Annotation] = field(default_factory=list)
    comments: List[Comment] = field(default_factory=list)
    location: Optional[Location] = None


@dataclass
class TypedNode:
    module: str = ""
    package: str = ""
    type: str = ""
    visibility: Visibility = Visibility.NA
    variables: List[RecordField] = field(default_factory=list)
    methods: List[MethodNode] = field(default_factory=list)
    types: List['TypedNode'] = field(default_factory=list)
    constructor: Optional[MethodNode] = None
    destructor: Optional[MethodNode] = None
    annotations: List[Annotation] = field(default_factory=list)
    comments: List[Comment] = field(default_factory=list)
    location: Optional[Location] = None


@dataclass
class PackageNode:
    module: str = ""
    package: str = ""
    types: List[TypedNode] = field(default_factory=list)
    records: List[ASTRecord] = field(default_factory=list)
    functions: List[MethodNode] = field(default_factory=list)
    variables: List[RecordField] = field(default_factory=list)
    init_functions: List[MethodNode] = field(default_factory=list)
    annotations: List[Annotation] = field(default_factory=list)
    comments: List[Comment] = field(default_factory=list)
    location: Optional[Location] = None
    language: Optional[str] = None


@dataclass
class ModuleNode:
    module: str = ""
    packages: List[PackageNode] = field(default_factory=list)
    annotations: List[Annotation] = field(default_factory=list)
    comments: List[Comment] = field(default_factory=list)
    location: Optional[Location] = None
    language: Optional[str] = None
