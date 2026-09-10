# UIR Java Implementation

Complete Java implementation of the Universal Intermediate Representation (UIR) for code analysis.

## Package Structure

```
com/flanksource/uir/
├── Uir.java                           # Root UIR class
├── Converter.java                     # JSON serialization/deserialization
├── Builders.java                      # Fluent builder API
├── UIRBasicTest.java                  # Basic usage examples
├── enums/
│   └── UIREnums.java                  # All enum types
├── core/
│   ├── Location.java                  # File location
│   ├── SourceCode.java                # Source code reference
│   ├── TypedValue.java                # Typed values
│   ├── Annotation.java                # Code annotations
│   ├── Comment.java                   # Code comments
│   ├── Metadata.java                  # Metadata container
│   ├── Identifiers.java               # All identifier types
│   └── Expression.java                # Expression wrapper
├── record/
│   └── RecordTypes.java               # Record/field types
├── statement/
│   ├── BaseStatements.java            # Expression statements
│   └── ControlFlowStatements.java     # Control flow statements
└── node/
    └── NodeTypes.java                 # AST node types
```

## Dependencies

```xml
<dependency>
    <groupId>com.fasterxml.jackson.core</groupId>
    <artifactId>jackson-databind</artifactId>
    <version>2.9.0</version>
</dependency>
<dependency>
    <groupId>com.fasterxml.jackson.datatype</groupId>
    <artifactId>jackson-datatype-jsr310</artifactId>
    <version>2.9.0</version>
</dependency>
```

## Usage

### Basic Example

```java
import com.flanksource.uir.*;
import com.flanksource.uir.node.NodeTypes.*;
import static com.flanksource.uir.Builders.*;

// Create a simple method
MethodNode method = method("add");
method.setVisibility(Visibility.PUBLIC);

// Add parameters
RecordField param1 = field("a", RecordFieldType.NUMBER);
RecordField param2 = field("b", RecordFieldType.NUMBER);
method.setParams(Arrays.asList(param1, param2));

// Serialize to JSON
String json = Converter.toJsonString(method);
```

### Using Builders

```java
// Create if statement: if (x > 5) { return x; }
PackageVariableRef x = variable("x");
LiteralStmt five = literal("5", RecordFieldType.NUMBER);

BinaryStmt comparison = binaryOp(expr(x), BinaryOp.GREATER, expr(five));
ConditionStmt condition = condition(expr(x));
BlockStmt thenBlock = block(returnStmt(expr(x)));

IfStmt ifStmt = ifStmt(condition, thenBlock);
```

### Complete UIR Structure

```java
// Create UIR with module -> package -> type -> method hierarchy
Uir uir = new Uir();

ModuleNode module = module("my-module");
PackageNode pkg = packageNode("com.example");
TypedNode type = type("Calculator");
MethodNode method = method("calculate");

type.setMethods(Arrays.asList(method));
pkg.setTypes(Arrays.asList(type));
module.setPackages(Arrays.asList(pkg));
uir.setModules(Arrays.asList(module));

// Serialize
String json = Converter.toJsonString(uir);

// Deserialize
Uir deserialized = Converter.fromJsonString(json);
```

## Running Tests

```bash
# Compile and run basic test
cd models/uir/java
javac -cp .:path/to/jackson-jars/* com/flanksource/uir/UIRBasicTest.java
java -cp .:path/to/jackson-jars/* com.flanksource.uir.UIRBasicTest
```

## Cross-Language Compatibility

This Java implementation is designed to be 100% compatible with:
- Go implementation in `models/uir/`
- TypeScript implementation in `plugins/typescript/src/uir/`
- Python implementation in `models/uir/python/`

All implementations produce identical JSON output for the same UIR structures.

## Key Features

- ✅ Complete UIR type system
- ✅ Jackson-based JSON serialization
- ✅ Fluent builder API
- ✅ Comprehensive enum support
- ✅ Polymorphic statement types
- ✅ Cross-language compatible
- ✅ Null-safe with `@JsonInclude`
- ✅ Well-documented classes

## Implementation Notes

- Uses Jackson for JSON serialization
- Follows Java naming conventions (camelCase)
- Consolidated related types in single files to reduce file count
- All nullable fields use `@JsonInclude(JsonInclude.Include.NON_NULL)`
- Supports full AST representation with statements, expressions, and control flow
- Compatible with Java 7+ (uses standard Java types)
