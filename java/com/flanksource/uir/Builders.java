package com.flanksource.uir;

import com.flanksource.archunit.uir.enums.UIREnums.*;
import com.flanksource.uir.core.*;
import com.flanksource.uir.core.Identifiers.*;
import com.flanksource.uir.statement.BaseStatements.*;
import com.flanksource.uir.statement.ControlFlowStatements.*;
import com.flanksource.uir.record.RecordTypes.*;
import com.flanksource.uir.node.NodeTypes.*;
import java.util.ArrayList;
import java.util.List;

/**
 * Fluent builder API for constructing UIR structures
 */
public class Builders {

    // Core builder methods for expressions
    public static LiteralStmt literal(String value, RecordFieldType fieldType) {
        LiteralStmt stmt = new LiteralStmt();
        stmt.setValue(value);
        stmt.setFieldType(fieldType);
        return stmt;
    }

    public static ExprStmt expr(LiteralStmt literal) {
        ExprStmt expr = new ExprStmt();
        expr.setLiteral(literal);
        return expr;
    }

    public static ExprStmt expr(PackageVariableRef variable) {
        ExprStmt expr = new ExprStmt();
        expr.setVariable(variable);
        return expr;
    }

    public static PackageVariableRef variable(String name) {
        PackageVariableRef ref = new PackageVariableRef();
        ref.setName(name);
        return ref;
    }

    public static BinaryStmt binaryOp(ExprStmt left, BinaryOp op, ExprStmt right) {
        BinaryStmt stmt = new BinaryStmt();
        stmt.setLeft(left);
        stmt.setOp(op);
        stmt.setRight(right);
        return stmt;
    }

    public static UnaryStmt unaryOp(UnaryOp op, ExprStmt operand) {
        UnaryStmt stmt = new UnaryStmt();
        stmt.setOperator(op);
        stmt.setOperand(operand);
        return stmt;
    }

    // Control flow builders
    public static IfStmt ifStmt(ConditionStmt condition, BlockStmt thenBlock) {
        IfStmt stmt = new IfStmt();
        stmt.setCondition(condition);
        stmt.setThenBlock(thenBlock);
        return stmt;
    }

    public static IfStmt ifStmt(ConditionStmt condition, BlockStmt thenBlock, BlockStmt elseBlock) {
        IfStmt stmt = ifStmt(condition, thenBlock);
        stmt.setElseBlock(elseBlock);
        return stmt;
    }

    public static ConditionStmt condition(ExprStmt expr) {
        ConditionStmt stmt = new ConditionStmt();
        stmt.setExpr(expr);
        return stmt;
    }

    public static BlockStmt block(Object... children) {
        BlockStmt block = new BlockStmt();
        List<Object> childList = new ArrayList<>();
        for (Object child : children) {
            childList.add(child);
        }
        block.setChildren(childList);
        return block;
    }

    public static AssignmentStmt assign(PackageVariableRef target, ExprStmt value) {
        AssignmentStmt stmt = new AssignmentStmt();
        stmt.setTarget(target);
        stmt.setValue(value);
        stmt.setOp(AssignmentOp.ASSIGN);
        return stmt;
    }

    public static ForStmt forLoop(AssignmentStmt init, ConditionStmt cond, ExprStmt update, BlockStmt body) {
        ForStmt stmt = new ForStmt();
        stmt.setInit(init);
        stmt.setCond(cond);
        stmt.setUpdate(update);
        stmt.setBody(body);
        return stmt;
    }

    public static WhileStmt whileLoop(ConditionStmt condition, BlockStmt body) {
        WhileStmt stmt = new WhileStmt();
        stmt.setCondition(condition);
        stmt.setBody(body);
        return stmt;
    }

    public static ReturnStmt returnStmt(ExprStmt value) {
        ReturnStmt stmt = new ReturnStmt();
        stmt.setValue(value);
        return stmt;
    }

    public static BreakStmt breakStmt() {
        return new BreakStmt();
    }

    public static ContinueStmt continueStmt() {
        return new ContinueStmt();
    }

    // Method call builders
    public static MethodCallStmt methodCall(String method) {
        MethodCallStmt stmt = new MethodCallStmt();
        stmt.setMethod(method);
        return stmt;
    }

    public static MethodCallStmt methodCall(String method, List<Argument> arguments) {
        MethodCallStmt stmt = methodCall(method);
        stmt.setArguments(arguments);
        return stmt;
    }

    public static Argument arg(String name, ExprStmt value) {
        Argument arg = new Argument();
        arg.setName(name);
        arg.setValue(value);
        return arg;
    }

    // Node builders
    public static MethodNode method(String methodName) {
        MethodNode node = new MethodNode();
        node.setMethod(methodName);
        return node;
    }

    public static TypedNode type(String typeName) {
        TypedNode node = new TypedNode();
        node.setType(typeName);
        return node;
    }

    public static PackageNode packageNode(String packageName) {
        PackageNode node = new PackageNode();
        node.setPackageName(packageName);
        return node;
    }

    public static ModuleNode module(String moduleName) {
        ModuleNode node = new ModuleNode();
        node.setModule(moduleName);
        return node;
    }

    // Record builders
    public static RecordField field(String name, RecordFieldType fieldType) {
        RecordField field = new RecordField();
        field.setName(name);
        field.setFieldType(fieldType);
        return field;
    }

    public static ASTRecord record(String name, RecordType recordType) {
        ASTRecord record = new ASTRecord();
        record.setName(name);
        record.setRecordType(recordType);
        return record;
    }

    // Location builder
    public static Location location(String path, int startLine, int endLine) {
        return new Location(path, startLine, endLine);
    }
}
