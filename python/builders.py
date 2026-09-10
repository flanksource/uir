"""UIR fluent builders for Python."""

from typing import Optional
try:
    from .uir_types import *
    from .enums import *
except ImportError:
    from uir_types import *
    from enums import *


class PackageBuilder:
    def __init__(self, name: str):
        self.node = PackageNode(package=name)

    def with_module(self, module: str) -> 'PackageBuilder':
        self.node.module = module
        return self

    def with_path(self, path: str) -> 'PackageBuilder':
        if not self.node.location:
            self.node.location = Location()
        self.node.location.path = path
        return self

    def with_language(self, lang: str) -> 'PackageBuilder':
        self.node.language = lang
        return self

    def with_variable(self, field: RecordField) -> 'PackageBuilder':
        self.node.variables.append(field)
        return self

    def with_variables(self, *fields: RecordField) -> 'PackageBuilder':
        self.node.variables.extend(fields)
        return self

    def with_function(self, fn: MethodNode) -> 'PackageBuilder':
        self.node.functions.append(fn)
        return self

    def with_type(self, typ: TypedNode) -> 'PackageBuilder':
        self.node.types.append(typ)
        return self

    def with_record(self, rec: ASTRecord) -> 'PackageBuilder':
        self.node.records.append(rec)
        return self

    def with_init_function(self, fn: MethodNode) -> 'PackageBuilder':
        self.node.init_functions.append(fn)
        return self

    def with_annotation(self, annotation: Annotation) -> 'PackageBuilder':
        self.node.annotations.append(annotation)
        return self

    def build(self) -> PackageNode:
        return self.node


class TypeBuilder:
    def __init__(self, name: str):
        self.node = TypedNode(type=name)

    def with_package(self, pkg: str) -> 'TypeBuilder':
        self.node.package = pkg
        return self

    def with_module(self, module: str) -> 'TypeBuilder':
        self.node.module = module
        return self

    def with_visibility(self, vis: Visibility) -> 'TypeBuilder':
        self.node.visibility = vis
        return self

    def with_path(self, path: str) -> 'TypeBuilder':
        if not self.node.location:
            self.node.location = Location()
        self.node.location.path = path
        return self

    def with_variable(self, field: RecordField) -> 'TypeBuilder':
        self.node.variables.append(field)
        return self

    def with_variables(self, *fields: RecordField) -> 'TypeBuilder':
        self.node.variables.extend(fields)
        return self

    def with_method(self, method: MethodNode) -> 'TypeBuilder':
        self.node.methods.append(method)
        return self

    def with_constructor(self, method: MethodNode) -> 'TypeBuilder':
        self.node.constructor = method
        return self

    def with_destructor(self, method: MethodNode) -> 'TypeBuilder':
        self.node.destructor = method
        return self

    def with_inner_type(self, typ: TypedNode) -> 'TypeBuilder':
        self.node.types.append(typ)
        return self

    def with_annotation(self, annotation: Annotation) -> 'TypeBuilder':
        self.node.annotations.append(annotation)
        return self

    def build(self) -> TypedNode:
        return self.node


class MethodBuilder:
    def __init__(self, name: str):
        self.node = MethodNode(method=name)
        self._body_statements = []

    def with_type(self, type_name: str) -> 'MethodBuilder':
        self.node.type = type_name
        return self

    def with_package(self, pkg: str) -> 'MethodBuilder':
        self.node.package = pkg
        return self

    def with_module(self, module: str) -> 'MethodBuilder':
        self.node.module = module
        return self

    def with_visibility(self, vis: Visibility) -> 'MethodBuilder':
        self.node.visibility = vis
        return self

    def with_path(self, path: str) -> 'MethodBuilder':
        if not self.node.location:
            self.node.location = Location()
        self.node.location.path = path
        return self

    def with_param(self, field: RecordField) -> 'MethodBuilder':
        self.node.params.append(field)
        return self

    def with_params(self, *fields: RecordField) -> 'MethodBuilder':
        self.node.params.extend(fields)
        return self

    def with_return(self, field: RecordField) -> 'MethodBuilder':
        self.node.returns.append(field)
        return self

    def with_returns(self, *fields: RecordField) -> 'MethodBuilder':
        self.node.returns.extend(fields)
        return self

    def with_error(self, err: ASTError) -> 'MethodBuilder':
        self.node.errors.append(err)
        return self

    def with_body(self, block: BlockStmt) -> 'MethodBuilder':
        self.node.body = block
        return self

    def with_statement(self, stmt: Statement) -> 'MethodBuilder':
        self._body_statements.append(stmt)
        return self

    def with_annotation(self, annotation: Annotation) -> 'MethodBuilder':
        self.node.annotations.append(annotation)
        return self

    def build(self) -> MethodNode:
        if self._body_statements and not self.node.body:
            self.node.body = BlockStmt(
                type=ASTStatementType.BLOCK,
                children=self._body_statements
            )
        return self.node


class BlockBuilder:
    def __init__(self):
        self.stmt = BlockStmt(type=ASTStatementType.BLOCK)

    def with_statement(self, stmt: Statement) -> 'BlockBuilder':
        self.stmt.children.append(stmt)
        return self

    def with_statements(self, *stmts: Statement) -> 'BlockBuilder':
        self.stmt.children.extend(stmts)
        return self

    def with_variable(self, field: RecordField) -> 'BlockBuilder':
        self.stmt.variables.append(field)
        return self

    def build(self) -> BlockStmt:
        return self.stmt


class IfBuilder:
    def __init__(self, condition: ExprStmt):
        self.stmt = IfStmt(
            type=ASTStatementType.IF,
            condition=ConditionStmt(expr=condition)
        )

    def with_then(self, block: BlockStmt) -> 'IfBuilder':
        self.stmt.then = block
        return self

    def with_else(self, block: BlockStmt) -> 'IfBuilder':
        self.stmt.else_block = block
        return self

    def build(self) -> IfStmt:
        return self.stmt


class AssignmentBuilder:
    def __init__(self, target: str, value: ExprStmt):
        self.stmt = AssignmentStmt(
            type=ASTStatementType.ASSIGNMENT,
            target=PackageVariableRef(name=target),
            value=value,
            op=AssignmentOp.ASSIGN
        )

    def with_op(self, op: AssignmentOp) -> 'AssignmentBuilder':
        self.stmt.op = op
        return self

    def build(self) -> AssignmentStmt:
        return self.stmt


class MethodCallBuilder:
    def __init__(self, method: str):
        self.stmt = MethodCallStmt(
            type=ASTStatementType.CALL,
            method=method
        )

    def with_receiver(self, expr: ExprStmt) -> 'MethodCallBuilder':
        self.stmt.receiver = expr
        return self

    def with_type(self, type_name: str) -> 'MethodCallBuilder':
        self.stmt.type_name = type_name
        return self

    def with_package(self, pkg: str) -> 'MethodCallBuilder':
        self.stmt.package = pkg
        return self

    def with_argument(self, name: str, value: ExprStmt) -> 'MethodCallBuilder':
        self.stmt.arguments.append(Argument(name=name, value=value))
        return self

    def with_positional_arg(self, value: ExprStmt) -> 'MethodCallBuilder':
        self.stmt.arguments.append(Argument(value=value))
        return self

    def build(self) -> MethodCallStmt:
        return self.stmt


class EndpointCallBuilder:
    def __init__(self, endpoint_type: EndpointType, urn: str):
        self.stmt = EndpointCallStmt(
            type=ASTStatementType.CALL_API,
            id=urn,
            endpoint_type=endpoint_type
        )

    def with_environment(self, env: str) -> 'EndpointCallBuilder':
        self.stmt.environment = env
        return self

    def with_argument(self, name: str, value: ExprStmt) -> 'EndpointCallBuilder':
        self.stmt.arguments.append(Argument(name=name, value=value))
        return self

    def with_positional_arg(self, value: ExprStmt) -> 'EndpointCallBuilder':
        self.stmt.arguments.append(Argument(value=value))
        return self

    def build(self) -> EndpointCallStmt:
        return self.stmt


class RecordReadBuilder:
    def __init__(self, record_type: RecordType, id: str):
        self.stmt = RecordReadStmt(
            type=ASTStatementType.RECORD_READ,
            id=id,
            record_type=record_type
        )

    def with_environment(self, env: str) -> 'RecordReadBuilder':
        self.stmt.environment = env
        return self

    def with_expression(self, expr_type: ExpressionType, expr: str) -> 'RecordReadBuilder':
        self.stmt.expression_type = expr_type
        self.stmt.expression = expr
        return self

    def with_argument(self, name: str, value: ExprStmt) -> 'RecordReadBuilder':
        self.stmt.arguments.append(Argument(name=name, value=value))
        return self

    def build(self) -> RecordReadStmt:
        return self.stmt


class RecordWriteBuilder:
    def __init__(self, record_type: RecordType, id: str):
        self.stmt = RecordWriteStmt(
            type=ASTStatementType.RECORD_WRITE,
            id=id,
            record_type=record_type
        )

    def with_environment(self, env: str) -> 'RecordWriteBuilder':
        self.stmt.environment = env
        return self

    def with_argument(self, name: str, value: ExprStmt) -> 'RecordWriteBuilder':
        self.stmt.arguments.append(Argument(name=name, value=value))
        return self

    def build(self) -> RecordWriteStmt:
        return self.stmt


class ForBuilder:
    def __init__(self):
        self.stmt = ForStmt(type=ASTStatementType.LOOP_FOR)

    def with_init(self, init: AssignmentStmt) -> 'ForBuilder':
        self.stmt.init = init
        return self

    def with_condition(self, cond: ConditionStmt) -> 'ForBuilder':
        self.stmt.cond = cond
        return self

    def with_update(self, update: ExprStmt) -> 'ForBuilder':
        self.stmt.update = update
        return self

    def with_body(self, body: BlockStmt) -> 'ForBuilder':
        self.stmt.body = body
        return self

    def build(self) -> ForStmt:
        return self.stmt


class WhileBuilder:
    def __init__(self, condition: ConditionStmt):
        self.stmt = WhileStmt(
            type=ASTStatementType.LOOP_WHILE,
            condition=condition
        )

    def with_body(self, body: BlockStmt) -> 'WhileBuilder':
        self.stmt.body = body
        return self

    def build(self) -> WhileStmt:
        return self.stmt


class SwitchBuilder:
    def __init__(self, value: ExprStmt):
        self.stmt = SwitchStmt(
            type=ASTStatementType.SWITCH,
            value=value
        )

    def with_case(self, condition: ConditionStmt, body: BlockStmt) -> 'SwitchBuilder':
        self.stmt.cases.append(SwitchCase(condition=condition, body=body))
        return self

    def build(self) -> SwitchStmt:
        return self.stmt


class TryBuilder:
    def __init__(self):
        self.stmt = TryStmt(type=ASTStatementType.TRY)

    def with_body(self, body: BlockStmt) -> 'TryBuilder':
        self.stmt.body = body
        return self

    def with_catch(self, catch: BlockStmt) -> 'TryBuilder':
        self.stmt.catch = catch
        return self

    def build(self) -> TryStmt:
        return self.stmt
