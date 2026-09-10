package com.flanksource.uir.core;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import java.util.Date;

/**
 * Represents a value with its type information
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class TypedValue {
    @JsonProperty("string")
    private String stringValue;

    @JsonProperty("float")
    private Double floatValue;

    @JsonProperty("int")
    private Long intValue;

    @JsonProperty("bool")
    private Boolean boolValue;

    @JsonProperty("date")
    private Date dateValue;

    public TypedValue() {}

    public static TypedValue ofString(String value) {
        TypedValue tv = new TypedValue();
        tv.stringValue = value;
        return tv;
    }

    public static TypedValue ofInt(Long value) {
        TypedValue tv = new TypedValue();
        tv.intValue = value;
        return tv;
    }

    public static TypedValue ofFloat(Double value) {
        TypedValue tv = new TypedValue();
        tv.floatValue = value;
        return tv;
    }

    public static TypedValue ofBool(Boolean value) {
        TypedValue tv = new TypedValue();
        tv.boolValue = value;
        return tv;
    }

    public static TypedValue ofDate(Date value) {
        TypedValue tv = new TypedValue();
        tv.dateValue = value;
        return tv;
    }

    public String getStringValue() { return stringValue; }
    public void setStringValue(String stringValue) { this.stringValue = stringValue; }

    public Double getFloatValue() { return floatValue; }
    public void setFloatValue(Double floatValue) { this.floatValue = floatValue; }

    public Long getIntValue() { return intValue; }
    public void setIntValue(Long intValue) { this.intValue = intValue; }

    public Boolean getBoolValue() { return boolValue; }
    public void setBoolValue(Boolean boolValue) { this.boolValue = boolValue; }

    public Date getDateValue() { return dateValue; }
    public void setDateValue(Date dateValue) { this.dateValue = dateValue; }
}
