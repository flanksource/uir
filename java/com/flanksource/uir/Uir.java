package com.flanksource.uir;

import com.fasterxml.jackson.annotation.*;
import com.flanksource.uir.node.NodeTypes.*;
import com.flanksource.uir.record.RecordTypes.*;
import java.util.List;

/**
 * Universal Intermediate Representation for code analysis
 * Root structure containing all top-level modules, packages, types, records, and functions
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class Uir {
    @JsonProperty("modules")
    private List<ModuleNode> modules;

    @JsonProperty("packages")
    private List<PackageNode> packages;

    @JsonProperty("types")
    private List<TypedNode> types;

    @JsonProperty("records")
    private List<ASTRecord> records;

    @JsonProperty("functions")
    private List<MethodNode> functions;

    public Uir() {}

    public List<ModuleNode> getModules() { return modules; }
    public void setModules(List<ModuleNode> modules) { this.modules = modules; }

    public List<PackageNode> getPackages() { return packages; }
    public void setPackages(List<PackageNode> packages) { this.packages = packages; }

    public List<TypedNode> getTypes() { return types; }
    public void setTypes(List<TypedNode> types) { this.types = types; }

    public List<ASTRecord> getRecords() { return records; }
    public void setRecords(List<ASTRecord> records) { this.records = records; }

    public List<MethodNode> getFunctions() { return functions; }
    public void setFunctions(List<MethodNode> functions) { this.functions = functions; }
}
