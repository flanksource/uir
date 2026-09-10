package com.flanksource.uir.core;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import java.util.Map;

@JsonInclude(JsonInclude.Include.NON_NULL)
public class Annotation {
    @JsonProperty("module")
    private String module;

    @JsonProperty("package")
    private String packageName;

    @JsonProperty("type")
    private String type;

    @JsonProperty("value")
    private TypedValue value;

    @JsonProperty("values")
    private Map<String, TypedValue> values;

    public Annotation() {}

    public String getModule() { return module; }
    public void setModule(String module) { this.module = module; }

    public String getPackageName() { return packageName; }
    public void setPackageName(String packageName) { this.packageName = packageName; }

    public String getType() { return type; }
    public void setType(String type) { this.type = type; }

    public TypedValue getValue() { return value; }
    public void setValue(TypedValue value) { this.value = value; }

    public Map<String, TypedValue> getValues() { return values; }
    public void setValues(Map<String, TypedValue> values) { this.values = values; }
}
