package com.flanksource.uir.node;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.flanksource.archunit.uir.enums.UIREnums.Visibility;
import com.flanksource.uir.core.*;
import com.flanksource.uir.record.RecordTypes.*;
import com.flanksource.uir.statement.ControlFlowStatements.BlockStmt;
import java.util.List;

/**
 * Node types for the UIR AST hierarchy
 */
public class NodeTypes {

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class MethodNode {
        @JsonProperty("module")
        private String module;

        @JsonProperty("package")
        private String packageName;

        @JsonProperty("type")
        private String type;

        @JsonProperty("method")
        private String method;

        @JsonProperty("visibility")
        private Visibility visibility;

        @JsonProperty("params")
        private List<RecordField> params;

        @JsonProperty("returns")
        private List<RecordField> returns;

        @JsonProperty("errors")
        private List<ASTError> errors;

        @JsonProperty("body")
        private BlockStmt body;

        @JsonProperty("annotations")
        private List<Annotation> annotations;

        @JsonProperty("comments")
        private List<Comment> comments;

        @JsonProperty("sourceCode")
        private SourceCode sourceCode;

        public MethodNode() {}

        public String getModule() { return module; }
        public void setModule(String module) { this.module = module; }

        public String getPackageName() { return packageName; }
        public void setPackageName(String packageName) { this.packageName = packageName; }

        public String getType() { return type; }
        public void setType(String type) { this.type = type; }

        public String getMethod() { return method; }
        public void setMethod(String method) { this.method = method; }

        public Visibility getVisibility() { return visibility; }
        public void setVisibility(Visibility visibility) { this.visibility = visibility; }

        public List<RecordField> getParams() { return params; }
        public void setParams(List<RecordField> params) { this.params = params; }

        public List<RecordField> getReturns() { return returns; }
        public void setReturns(List<RecordField> returns) { this.returns = returns; }

        public List<ASTError> getErrors() { return errors; }
        public void setErrors(List<ASTError> errors) { this.errors = errors; }

        public BlockStmt getBody() { return body; }
        public void setBody(BlockStmt body) { this.body = body; }

        public List<Annotation> getAnnotations() { return annotations; }
        public void setAnnotations(List<Annotation> annotations) { this.annotations = annotations; }

        public List<Comment> getComments() { return comments; }
        public void setComments(List<Comment> comments) { this.comments = comments; }

        public SourceCode getSourceCode() { return sourceCode; }
        public void setSourceCode(SourceCode sourceCode) { this.sourceCode = sourceCode; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class TypedNode {
        @JsonProperty("module")
        private String module;

        @JsonProperty("package")
        private String packageName;

        @JsonProperty("type")
        private String type;

        @JsonProperty("visibility")
        private Visibility visibility;

        @JsonProperty("variables")
        private List<RecordField> variables;

        @JsonProperty("methods")
        private List<MethodNode> methods;

        @JsonProperty("types")
        private List<TypedNode> types;

        @JsonProperty("constructor")
        private MethodNode constructor;

        @JsonProperty("destructor")
        private MethodNode destructor;

        @JsonProperty("annotations")
        private List<Annotation> annotations;

        @JsonProperty("comments")
        private List<Comment> comments;

        @JsonProperty("sourceCode")
        private SourceCode sourceCode;

        public TypedNode() {}

        public String getModule() { return module; }
        public void setModule(String module) { this.module = module; }

        public String getPackageName() { return packageName; }
        public void setPackageName(String packageName) { this.packageName = packageName; }

        public String getType() { return type; }
        public void setType(String type) { this.type = type; }

        public Visibility getVisibility() { return visibility; }
        public void setVisibility(Visibility visibility) { this.visibility = visibility; }

        public List<RecordField> getVariables() { return variables; }
        public void setVariables(List<RecordField> variables) { this.variables = variables; }

        public List<MethodNode> getMethods() { return methods; }
        public void setMethods(List<MethodNode> methods) { this.methods = methods; }

        public List<TypedNode> getTypes() { return types; }
        public void setTypes(List<TypedNode> types) { this.types = types; }

        public MethodNode getConstructor() { return constructor; }
        public void setConstructor(MethodNode constructor) { this.constructor = constructor; }

        public MethodNode getDestructor() { return destructor; }
        public void setDestructor(MethodNode destructor) { this.destructor = destructor; }

        public List<Annotation> getAnnotations() { return annotations; }
        public void setAnnotations(List<Annotation> annotations) { this.annotations = annotations; }

        public List<Comment> getComments() { return comments; }
        public void setComments(List<Comment> comments) { this.comments = comments; }

        public SourceCode getSourceCode() { return sourceCode; }
        public void setSourceCode(SourceCode sourceCode) { this.sourceCode = sourceCode; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class PackageNode {
        @JsonProperty("module")
        private String module;

        @JsonProperty("package")
        private String packageName;

        @JsonProperty("types")
        private List<TypedNode> types;

        @JsonProperty("records")
        private List<ASTRecord> records;

        @JsonProperty("endpoints")
        private List<ASTEndpoint> endpoints;

        @JsonProperty("functions")
        private List<MethodNode> functions;

        @JsonProperty("variables")
        private List<RecordField> variables;

        @JsonProperty("initFunctions")
        private List<MethodNode> initFunctions;

        @JsonProperty("annotations")
        private List<Annotation> annotations;

        @JsonProperty("comments")
        private List<Comment> comments;

        @JsonProperty("location")
        private Location location;

        @JsonProperty("language")
        private String language;

        public PackageNode() {}

        public String getModule() { return module; }
        public void setModule(String module) { this.module = module; }

        public String getPackageName() { return packageName; }
        public void setPackageName(String packageName) { this.packageName = packageName; }

        public List<TypedNode> getTypes() { return types; }
        public void setTypes(List<TypedNode> types) { this.types = types; }

        public List<ASTRecord> getRecords() { return records; }
        public void setRecords(List<ASTRecord> records) { this.records = records; }

        public List<ASTEndpoint> getEndpoints() { return endpoints; }
        public void setEndpoints(List<ASTEndpoint> endpoints) { this.endpoints = endpoints; }

        public List<MethodNode> getFunctions() { return functions; }
        public void setFunctions(List<MethodNode> functions) { this.functions = functions; }

        public List<RecordField> getVariables() { return variables; }
        public void setVariables(List<RecordField> variables) { this.variables = variables; }

        public List<MethodNode> getInitFunctions() { return initFunctions; }
        public void setInitFunctions(List<MethodNode> initFunctions) { this.initFunctions = initFunctions; }

        public List<Annotation> getAnnotations() { return annotations; }
        public void setAnnotations(List<Annotation> annotations) { this.annotations = annotations; }

        public List<Comment> getComments() { return comments; }
        public void setComments(List<Comment> comments) { this.comments = comments; }

        public Location getLocation() { return location; }
        public void setLocation(Location location) { this.location = location; }

        public String getLanguage() { return language; }
        public void setLanguage(String language) { this.language = language; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ModuleNode {
        @JsonProperty("module")
        private String module;

        @JsonProperty("packages")
        private List<PackageNode> packages;

        @JsonProperty("annotations")
        private List<Annotation> annotations;

        @JsonProperty("comments")
        private List<Comment> comments;

        @JsonProperty("location")
        private Location location;

        @JsonProperty("language")
        private String language;

        public ModuleNode() {}

        public String getModule() { return module; }
        public void setModule(String module) { this.module = module; }

        public List<PackageNode> getPackages() { return packages; }
        public void setPackages(List<PackageNode> packages) { this.packages = packages; }

        public List<Annotation> getAnnotations() { return annotations; }
        public void setAnnotations(List<Annotation> annotations) { this.annotations = annotations; }

        public List<Comment> getComments() { return comments; }
        public void setComments(List<Comment> comments) { this.comments = comments; }

        public Location getLocation() { return location; }
        public void setLocation(Location location) { this.location = location; }

        public String getLanguage() { return language; }
        public void setLanguage(String language) { this.language = language; }
    }
}
