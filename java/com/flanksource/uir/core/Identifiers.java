package com.flanksource.uir.core;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.flanksource.archunit.uir.enums.UIREnums.EndpointType;
import com.flanksource.archunit.uir.enums.UIREnums.RecordType;

/**
 * Container for all identifier types used in UIR
 */
public class Identifiers {

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class EnvironmentIdentifier {
        @JsonProperty("environment")
        private String environment;

        @JsonProperty("urn")
        private String urn;

        public EnvironmentIdentifier() {}

        public String getEnvironment() { return environment; }
        public void setEnvironment(String environment) { this.environment = environment; }

        public String getUrn() { return urn; }
        public void setUrn(String urn) { this.urn = urn; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ModuleIdentifier {
        @JsonProperty("module")
        private String module;

        public ModuleIdentifier() {}

        public String getModule() { return module; }
        public void setModule(String module) { this.module = module; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class PackageIdentifier {
        @JsonProperty("module")
        private String module;

        @JsonProperty("package")
        private String packageName;

        public PackageIdentifier() {}

        public String getModule() { return module; }
        public void setModule(String module) { this.module = module; }

        public String getPackageName() { return packageName; }
        public void setPackageName(String packageName) { this.packageName = packageName; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class TypeIdentifier {
        @JsonProperty("module")
        private String module;

        @JsonProperty("package")
        private String packageName;

        @JsonProperty("type")
        private String type;

        public TypeIdentifier() {}

        public String getModule() { return module; }
        public void setModule(String module) { this.module = module; }

        public String getPackageName() { return packageName; }
        public void setPackageName(String packageName) { this.packageName = packageName; }

        public String getType() { return type; }
        public void setType(String type) { this.type = type; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class MethodIdentifier {
        @JsonProperty("module")
        private String module;

        @JsonProperty("package")
        private String packageName;

        @JsonProperty("type")
        private String type;

        @JsonProperty("method")
        private String method;

        public MethodIdentifier() {}

        public String getModule() { return module; }
        public void setModule(String module) { this.module = module; }

        public String getPackageName() { return packageName; }
        public void setPackageName(String packageName) { this.packageName = packageName; }

        public String getType() { return type; }
        public void setType(String type) { this.type = type; }

        public String getMethod() { return method; }
        public void setMethod(String method) { this.method = method; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class StaticMethodIdentifier {
        @JsonProperty("module")
        private String module;

        @JsonProperty("package")
        private String packageName;

        @JsonProperty("method")
        private String method;

        public StaticMethodIdentifier() {}

        public String getModule() { return module; }
        public void setModule(String module) { this.module = module; }

        public String getPackageName() { return packageName; }
        public void setPackageName(String packageName) { this.packageName = packageName; }

        public String getMethod() { return method; }
        public void setMethod(String method) { this.method = method; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class EndpointIdentifier {
        @JsonProperty("environment")
        private String environment;

        @JsonProperty("urn")
        private String urn;

        @JsonProperty("endpointType")
        private EndpointType endpointType;

        public EndpointIdentifier() {}

        public String getEnvironment() { return environment; }
        public void setEnvironment(String environment) { this.environment = environment; }

        public String getUrn() { return urn; }
        public void setUrn(String urn) { this.urn = urn; }

        public EndpointType getEndpointType() { return endpointType; }
        public void setEndpointType(EndpointType endpointType) { this.endpointType = endpointType; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class RecordIdentifier {
        @JsonProperty("environment")
        private String environment;

        @JsonProperty("urn")
        private String urn;

        @JsonProperty("recordType")
        private RecordType recordType;

        public RecordIdentifier() {}

        public String getEnvironment() { return environment; }
        public void setEnvironment(String environment) { this.environment = environment; }

        public String getUrn() { return urn; }
        public void setUrn(String urn) { this.urn = urn; }

        public RecordType getRecordType() { return recordType; }
        public void setRecordType(RecordType recordType) { this.recordType = recordType; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ScopedVariableRef {
        @JsonProperty("name")
        private String name;

        @JsonProperty("defaultValue")
        private TypedValue defaultValue;

        public ScopedVariableRef() {}

        public String getName() { return name; }
        public void setName(String name) { this.name = name; }

        public TypedValue getDefaultValue() { return defaultValue; }
        public void setDefaultValue(TypedValue defaultValue) { this.defaultValue = defaultValue; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class PackageVariableRef {
        @JsonProperty("name")
        private String name;

        @JsonProperty("module")
        private String module;

        @JsonProperty("package")
        private String packageName;

        @JsonProperty("defaultValue")
        private TypedValue defaultValue;

        public PackageVariableRef() {}

        public String getName() { return name; }
        public void setName(String name) { this.name = name; }

        public String getModule() { return module; }
        public void setModule(String module) { this.module = module; }

        public String getPackageName() { return packageName; }
        public void setPackageName(String packageName) { this.packageName = packageName; }

        public TypedValue getDefaultValue() { return defaultValue; }
        public void setDefaultValue(TypedValue defaultValue) { this.defaultValue = defaultValue; }
    }
}
