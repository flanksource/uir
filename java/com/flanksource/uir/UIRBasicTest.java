package com.flanksource.uir;

import com.flanksource.archunit.uir.enums.UIREnums.*;
import com.flanksource.uir.core.*;
import com.flanksource.uir.statement.BaseStatements.*;
import com.flanksource.uir.statement.ControlFlowStatements.*;
import com.flanksource.uir.record.RecordTypes.*;
import com.flanksource.uir.node.NodeTypes.*;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

import static com.flanksource.uir.Builders.*;

/**
 * Basic test demonstrating UIR usage and serialization
 */
public class UIRBasicTest {

    public static void main(String[] args) {
        try {
            System.out.println("=== UIR Basic Test ===\n");

            // Test 1: Create a simple method with if statement
            System.out.println("Test 1: Creating simple method with control flow");
            MethodNode method = createSimpleMethod();
            String methodJson = Converter.toJsonString(method);
            System.out.println("Method JSON:\n" + methodJson + "\n");

            // Test 2: Create a package with types
            System.out.println("Test 2: Creating package with types");
            PackageNode pkg = createSimplePackage();
            String packageJson = Converter.toJsonString(pkg);
            System.out.println("Package JSON:\n" + packageJson + "\n");

            // Test 3: Create complete UIR structure
            System.out.println("Test 3: Creating complete UIR");
            Uir uir = createSimpleUIR();
            String uirJson = Converter.toJsonString(uir);
            System.out.println("UIR JSON:\n" + uirJson + "\n");

            // Test 4: Deserialization round-trip
            System.out.println("Test 4: Testing round-trip serialization");
            Uir deserialized = Converter.fromJsonString(uirJson);
            System.out.println("Deserialization successful!");
            System.out.println("Modules: " + (deserialized.getModules() != null ? deserialized.getModules().size() : 0));
            System.out.println("Packages: " + (deserialized.getPackages() != null ? deserialized.getPackages().size() : 0));

            System.out.println("\n=== All tests passed! ===");

        } catch (Exception e) {
            System.err.println("Test failed with error:");
            e.printStackTrace();
            System.exit(1);
        }
    }

    private static MethodNode createSimpleMethod() {
        // Create: if (x > 5) { return x; } else { return 0; }
        PackageVariableRef x = variable("x");
        LiteralStmt five = literal("5", RecordFieldType.NUMBER);
        LiteralStmt zero = literal("0", RecordFieldType.NUMBER);

        BinaryStmt comparison = binaryOp(expr(x), BinaryOp.GREATER, expr(five));
        ConditionStmt condition = condition(expr(x));  // Simplified condition

        BlockStmt thenBlock = block(returnStmt(expr(x)));
        BlockStmt elseBlock = block(returnStmt(expr(zero)));

        IfStmt ifStmt = ifStmt(condition, thenBlock, elseBlock);

        MethodNode method = method("checkValue");
        method.setVisibility(Visibility.PUBLIC);
        method.setPackageName("com.example");

        // Create method body
        BlockStmt body = new BlockStmt();
        List<Object> children = new ArrayList<>();
        children.add(ifStmt);
        body.setChildren(children);
        method.setBody(body);

        // Add parameters
        RecordField param = field("x", RecordFieldType.NUMBER);
        method.setParams(Arrays.asList(param));

        // Add return type
        RecordField returnType = field("", RecordFieldType.NUMBER);
        method.setReturns(Arrays.asList(returnType));

        return method;
    }

    private static PackageNode createSimplePackage() {
        PackageNode pkg = packageNode("com.example");
        pkg.setModule("example-module");
        pkg.setLanguage("java");

        // Create a simple type
        TypedNode type = type("Calculator");
        type.setVisibility(Visibility.PUBLIC);
        type.setPackageName("com.example");

        // Add a method to the type
        MethodNode method = method("add");
        method.setVisibility(Visibility.PUBLIC);
        RecordField param1 = field("a", RecordFieldType.NUMBER);
        RecordField param2 = field("b", RecordFieldType.NUMBER);
        method.setParams(Arrays.asList(param1, param2));

        RecordField returnType = field("", RecordFieldType.NUMBER);
        method.setReturns(Arrays.asList(returnType));

        type.setMethods(Arrays.asList(method));
        pkg.setTypes(Arrays.asList(type));

        return pkg;
    }

    private static Uir createSimpleUIR() {
        Uir uir = new Uir();

        // Create a module
        ModuleNode module = module("example-module");
        module.setLanguage("java");

        // Create a package
        PackageNode pkg = createSimplePackage();
        module.setPackages(Arrays.asList(pkg));

        uir.setModules(Arrays.asList(module));

        return uir;
    }
}
