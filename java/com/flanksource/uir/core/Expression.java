package com.flanksource.uir.core;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.flanksource.archunit.uir.enums.UIREnums.ExpressionType;

@JsonInclude(JsonInclude.Include.NON_NULL)
public class Expression {
    @JsonProperty("expressionType")
    private ExpressionType expressionType;

    @JsonProperty("expression")
    private String expression;

    public Expression() {}

    public Expression(ExpressionType expressionType, String expression) {
        this.expressionType = expressionType;
        this.expression = expression;
    }

    public ExpressionType getExpressionType() { return expressionType; }
    public void setExpressionType(ExpressionType expressionType) { this.expressionType = expressionType; }

    public String getExpression() { return expression; }
    public void setExpression(String expression) { this.expression = expression; }
}
