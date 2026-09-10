package com.flanksource.uir.statement;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.flanksource.archunit.uir.enums.UIREnums.ASTStatementType;
import com.flanksource.uir.statement.BaseStatements.*;
import com.flanksource.uir.record.RecordTypes.RecordField;
import com.flanksource.uir.node.NodeTypes.MethodNode;
import java.util.List;

/**
 * Control flow statement types
 */
public class ControlFlowStatements {

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ConditionStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.CONDITION;

        @JsonProperty("expr")
        private ExprStmt expr;

        public ConditionStmt() {}

        public ASTStatementType getType() { return type; }

        public ExprStmt getExpr() { return expr; }
        public void setExpr(ExprStmt expr) { this.expr = expr; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class BlockStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.BLOCK;

        @JsonProperty("variables")
        private List<RecordField> variables;

        @JsonProperty("children")
        private List<Object> children;  // List of Statement types

        public BlockStmt() {}

        public ASTStatementType getType() { return type; }

        public List<RecordField> getVariables() { return variables; }
        public void setVariables(List<RecordField> variables) { this.variables = variables; }

        public List<Object> getChildren() { return children; }
        public void setChildren(List<Object> children) { this.children = children; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class IfStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.IF;

        @JsonProperty("condition")
        private ConditionStmt condition;

        @JsonProperty("then")
        private BlockStmt thenBlock;

        @JsonProperty("else")
        private BlockStmt elseBlock;

        public IfStmt() {}

        public ASTStatementType getType() { return type; }

        public ConditionStmt getCondition() { return condition; }
        public void setCondition(ConditionStmt condition) { this.condition = condition; }

        public BlockStmt getThenBlock() { return thenBlock; }
        public void setThenBlock(BlockStmt thenBlock) { this.thenBlock = thenBlock; }

        public BlockStmt getElseBlock() { return elseBlock; }
        public void setElseBlock(BlockStmt elseBlock) { this.elseBlock = elseBlock; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class SwitchCase {
        @JsonProperty("condition")
        private ConditionStmt condition;

        @JsonProperty("body")
        private BlockStmt body;

        public SwitchCase() {}

        public ConditionStmt getCondition() { return condition; }
        public void setCondition(ConditionStmt condition) { this.condition = condition; }

        public BlockStmt getBody() { return body; }
        public void setBody(BlockStmt body) { this.body = body; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class SwitchStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.SWITCH;

        @JsonProperty("value")
        private ExprStmt value;

        @JsonProperty("cases")
        private List<SwitchCase> cases;

        public SwitchStmt() {}

        public ASTStatementType getType() { return type; }

        public ExprStmt getValue() { return value; }
        public void setValue(ExprStmt value) { this.value = value; }

        public List<SwitchCase> getCases() { return cases; }
        public void setCases(List<SwitchCase> cases) { this.cases = cases; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class AssignmentStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.ASSIGNMENT;

        @JsonProperty("target")
        private com.flanksource.uir.core.Identifiers.PackageVariableRef target;

        @JsonProperty("value")
        private ExprStmt value;

        @JsonProperty("op")
        private com.flanksource.archunit.uir.enums.UIREnums.AssignmentOp op;

        public AssignmentStmt() {}

        public ASTStatementType getType() { return type; }

        public com.flanksource.uir.core.Identifiers.PackageVariableRef getTarget() { return target; }
        public void setTarget(com.flanksource.uir.core.Identifiers.PackageVariableRef target) { this.target = target; }

        public ExprStmt getValue() { return value; }
        public void setValue(ExprStmt value) { this.value = value; }

        public com.flanksource.archunit.uir.enums.UIREnums.AssignmentOp getOp() { return op; }
        public void setOp(com.flanksource.archunit.uir.enums.UIREnums.AssignmentOp op) { this.op = op; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ForStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.LOOP_FOR;

        @JsonProperty("init")
        private AssignmentStmt init;

        @JsonProperty("cond")
        private ConditionStmt cond;

        @JsonProperty("body")
        private BlockStmt body;

        @JsonProperty("update")
        private ExprStmt update;

        public ForStmt() {}

        public ASTStatementType getType() { return type; }

        public AssignmentStmt getInit() { return init; }
        public void setInit(AssignmentStmt init) { this.init = init; }

        public ConditionStmt getCond() { return cond; }
        public void setCond(ConditionStmt cond) { this.cond = cond; }

        public BlockStmt getBody() { return body; }
        public void setBody(BlockStmt body) { this.body = body; }

        public ExprStmt getUpdate() { return update; }
        public void setUpdate(ExprStmt update) { this.update = update; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class WhileStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.LOOP_WHILE;

        @JsonProperty("condition")
        private ConditionStmt condition;

        @JsonProperty("body")
        private BlockStmt body;

        public WhileStmt() {}

        public ASTStatementType getType() { return type; }

        public ConditionStmt getCondition() { return condition; }
        public void setCondition(ConditionStmt condition) { this.condition = condition; }

        public BlockStmt getBody() { return body; }
        public void setBody(BlockStmt body) { this.body = body; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class TryStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.TRY;

        @JsonProperty("body")
        private BlockStmt body;

        @JsonProperty("catch")
        private BlockStmt catchBlock;

        public TryStmt() {}

        public ASTStatementType getType() { return type; }

        public BlockStmt getBody() { return body; }
        public void setBody(BlockStmt body) { this.body = body; }

        public BlockStmt getCatchBlock() { return catchBlock; }
        public void setCatchBlock(BlockStmt catchBlock) { this.catchBlock = catchBlock; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ReturnStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.RETURN;

        @JsonProperty("value")
        private ExprStmt value;

        public ReturnStmt() {}

        public ASTStatementType getType() { return type; }

        public ExprStmt getValue() { return value; }
        public void setValue(ExprStmt value) { this.value = value; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class BreakStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.BREAK;

        public BreakStmt() {}

        public ASTStatementType getType() { return type; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ContinueStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.CONTINUE;

        public ContinueStmt() {}

        public ASTStatementType getType() { return type; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ThrowStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.THROW;

        @JsonProperty("exception")
        private ExprStmt exception;

        public ThrowStmt() {}

        public ASTStatementType getType() { return type; }

        public ExprStmt getException() { return exception; }
        public void setException(ExprStmt exception) { this.exception = exception; }
    }

    /* Temporarily commented out - missing enum values
    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class VariableDeclStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.DECLARE_VARIABLE;

        @JsonProperty("field")
        private RecordField field;

        public VariableDeclStmt() {}

        public ASTStatementType getType() { return type; }

        public RecordField getField() { return field; }
        public void setField(RecordField field) { this.field = field; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class FunctionDeclStmt {
        @JsonProperty("statement_type")
        private ASTStatementType type = ASTStatementType.DECLARE_FUNCTION;

        @JsonProperty("method")
        private MethodNode method;

        public FunctionDeclStmt() {}

        public ASTStatementType getType() { return type; }

        public MethodNode getMethod() { return method; }
        public void setMethod(MethodNode method) { this.method = method; }
    }
    */
}
