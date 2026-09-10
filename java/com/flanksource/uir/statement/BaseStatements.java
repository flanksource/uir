package com.flanksource.uir.statement;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.flanksource.archunit.uir.enums.UIREnums.*;
import com.flanksource.uir.core.*;
import com.flanksource.uir.core.Identifiers.*;
import com.flanksource.uir.record.RecordTypes.*;
import java.util.List;

/**
 * Base statement types and simple expression statements
 */
public class BaseStatements {

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class Argument {
        @JsonProperty("name")
        private String name;

        @JsonProperty("value")
        private ExprStmt value;

        public Argument() {}

        public String getName() { return name; }
        public void setName(String name) { this.name = name; }

        public ExprStmt getValue() { return value; }
        public void setValue(ExprStmt value) { this.value = value; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class LiteralStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.LITERAL;

        @JsonProperty("value")
        private String value;

        @JsonProperty("fieldType")
        private RecordFieldType fieldType;

        public LiteralStmt() {}

        public ASTStatementType getType() { return type; }
        public String getValue() { return value; }
        public void setValue(String value) { this.value = value; }

        public RecordFieldType getFieldType() { return fieldType; }
        public void setFieldType(RecordFieldType fieldType) { this.fieldType = fieldType; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class VariableStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.ASSIGNMENT;

        @JsonProperty("name")
        private String name;

        public VariableStmt() {}

        public ASTStatementType getType() { return type; }
        public String getName() { return name; }
        public void setName(String name) { this.name = name; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class BinaryStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.BINARY;

        @JsonProperty("left")
        private ExprStmt left;

        @JsonProperty("op")
        private BinaryOp op;

        @JsonProperty("right")
        private ExprStmt right;

        public BinaryStmt() {}

        public ASTStatementType getType() { return type; }

        public ExprStmt getLeft() { return left; }
        public void setLeft(ExprStmt left) { this.left = left; }

        public BinaryOp getOp() { return op; }
        public void setOp(BinaryOp op) { this.op = op; }

        public ExprStmt getRight() { return right; }
        public void setRight(ExprStmt right) { this.right = right; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class CastStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.CAST;

        @JsonProperty("expr")
        private ExprStmt expr;

        @JsonProperty("targetType")
        private String targetType;

        public CastStmt() {}

        public ASTStatementType getType() { return type; }

        public ExprStmt getExpr() { return expr; }
        public void setExpr(ExprStmt expr) { this.expr = expr; }

        public String getTargetType() { return targetType; }
        public void setTargetType(String targetType) { this.targetType = targetType; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class UnaryStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.UNARY;

        @JsonProperty("operator")
        private UnaryOp operator;

        @JsonProperty("operand")
        private ExprStmt operand;

        public UnaryStmt() {}

        public ASTStatementType getType() { return type; }

        public UnaryOp getOperator() { return operator; }
        public void setOperator(UnaryOp operator) { this.operator = operator; }

        public ExprStmt getOperand() { return operand; }
        public void setOperand(ExprStmt operand) { this.operand = operand; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class TupleStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.ASSIGNMENT;

        @JsonProperty("elements")
        private List<ExprStmt> elements;

        public TupleStmt() {}

        public ASTStatementType getType() { return type; }

        public List<ExprStmt> getElements() { return elements; }
        public void setElements(List<ExprStmt> elements) { this.elements = elements; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class MethodCallStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.CALL;

        @JsonProperty("module")
        private String module;

        @JsonProperty("package")
        private String packageName;

        @JsonProperty("method")
        private String method;

        @JsonProperty("receiver")
        private ExprStmt receiver;

        @JsonProperty("arguments")
        private List<Argument> arguments;

        public MethodCallStmt() {}

        public ASTStatementType getType() { return type; }

        public String getModule() { return module; }
        public void setModule(String module) { this.module = module; }

        public String getPackageName() { return packageName; }
        public void setPackageName(String packageName) { this.packageName = packageName; }

        public String getMethod() { return method; }
        public void setMethod(String method) { this.method = method; }

        public ExprStmt getReceiver() { return receiver; }
        public void setReceiver(ExprStmt receiver) { this.receiver = receiver; }

        public List<Argument> getArguments() { return arguments; }
        public void setArguments(List<Argument> arguments) { this.arguments = arguments; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class EndpointCallStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.CALL_API;

        @JsonProperty("environment")
        private String environment;

        @JsonProperty("endpointType")
        private EndpointType endpointType;

        @JsonProperty("arguments")
        private List<Argument> arguments;

        public EndpointCallStmt() {}

        public ASTStatementType getType() { return type; }

        public String getEnvironment() { return environment; }
        public void setEnvironment(String environment) { this.environment = environment; }

        public EndpointType getEndpointType() { return endpointType; }
        public void setEndpointType(EndpointType endpointType) { this.endpointType = endpointType; }

        public List<Argument> getArguments() { return arguments; }
        public void setArguments(List<Argument> arguments) { this.arguments = arguments; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class RecordReadStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.RECORD_READ;

        @JsonProperty("environment")
        private String environment;

        @JsonProperty("recordType")
        private RecordType recordType;

        @JsonProperty("expressionType")
        private ExpressionType expressionType;

        @JsonProperty("expression")
        private String expression;

        @JsonProperty("arguments")
        private List<Argument> arguments;

        public RecordReadStmt() {}

        public ASTStatementType getType() { return type; }

        public String getEnvironment() { return environment; }
        public void setEnvironment(String environment) { this.environment = environment; }

        public RecordType getRecordType() { return recordType; }
        public void setRecordType(RecordType recordType) { this.recordType = recordType; }

        public ExpressionType getExpressionType() { return expressionType; }
        public void setExpressionType(ExpressionType expressionType) { this.expressionType = expressionType; }

        public String getExpression() { return expression; }
        public void setExpression(String expression) { this.expression = expression; }

        public List<Argument> getArguments() { return arguments; }
        public void setArguments(List<Argument> arguments) { this.arguments = arguments; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class RecordWriteStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.RECORD_WRITE;

        @JsonProperty("environment")
        private String environment;

        @JsonProperty("recordType")
        private RecordType recordType;

        @JsonProperty("expressionType")
        private ExpressionType expressionType;

        @JsonProperty("expression")
        private String expression;

        @JsonProperty("arguments")
        private List<Argument> arguments;

        public RecordWriteStmt() {}

        public ASTStatementType getType() { return type; }

        public String getEnvironment() { return environment; }
        public void setEnvironment(String environment) { this.environment = environment; }

        public RecordType getRecordType() { return recordType; }
        public void setRecordType(RecordType recordType) { this.recordType = recordType; }

        public ExpressionType getExpressionType() { return expressionType; }
        public void setExpressionType(ExpressionType expressionType) { this.expressionType = expressionType; }

        public String getExpression() { return expression; }
        public void setExpression(String expression) { this.expression = expression; }

        public List<Argument> getArguments() { return arguments; }
        public void setArguments(List<Argument> arguments) { this.arguments = arguments; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ExprStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.EXPRESSION;

        @JsonProperty("literal")
        private LiteralStmt literal;

        @JsonProperty("variable")
        private PackageVariableRef variable;

        @JsonProperty("binary")
        private BinaryStmt binary;

        @JsonProperty("cast")
        private CastStmt cast;

        @JsonProperty("unary")
        private UnaryStmt unary;

        @JsonProperty("method_call")
        private MethodCallStmt methodCall;

        @JsonProperty("endpoint_call")
        private EndpointCallStmt endpointCall;

        @JsonProperty("record_read")
        private RecordReadStmt recordRead;

        @JsonProperty("tuple")
        private TupleStmt tuple;

        public ExprStmt() {}

        public ASTStatementType getType() { return type; }

        public LiteralStmt getLiteral() { return literal; }
        public void setLiteral(LiteralStmt literal) { this.literal = literal; }

        public PackageVariableRef getVariable() { return variable; }
        public void setVariable(PackageVariableRef variable) { this.variable = variable; }

        public BinaryStmt getBinary() { return binary; }
        public void setBinary(BinaryStmt binary) { this.binary = binary; }

        public CastStmt getCast() { return cast; }
        public void setCast(CastStmt cast) { this.cast = cast; }

        public UnaryStmt getUnary() { return unary; }
        public void setUnary(UnaryStmt unary) { this.unary = unary; }

        public MethodCallStmt getMethodCall() { return methodCall; }
        public void setMethodCall(MethodCallStmt methodCall) { this.methodCall = methodCall; }

        public EndpointCallStmt getEndpointCall() { return endpointCall; }
        public void setEndpointCall(EndpointCallStmt endpointCall) { this.endpointCall = endpointCall; }

        public RecordReadStmt getRecordRead() { return recordRead; }
        public void setRecordRead(RecordReadStmt recordRead) { this.recordRead = recordRead; }

        public TupleStmt getTuple() { return tuple; }
        public void setTuple(TupleStmt tuple) { this.tuple = tuple; }
    }
}
