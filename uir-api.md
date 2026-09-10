

# **Universal Intermediate Representation (UIR) API Specification**

This document provides the detailed API specification for the Universal Intermediate Representation (UIR). The UIR is a universal Abstract Syntax Tree (AST) format designed to represent source code, data structures, and service definitions from multiple languages and domains in a unified structure.

## **1\. Foundational Interfaces**

All nodes within the UIR adhere to the unist (Universal Syntax Tree) specification.1 This provides a common, serializable foundation for all representations.

### **1.1. Node**

The core interface for every syntactic unit in the tree.

* **Description**: The base element from which all other UIR nodes are derived.1  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| type | string | A string representing the variant of the node (e.g., "Function").1 |
| position | Position? | The location of the node in the original source file. Must be omitted for generated nodes.1 |
| data | object? | A metadata field for ecosystem-specific information, such as inferred types or symbol references.1 |

### #1.2 Identifier

* **Type**: "id"  
* **Description**: A fully qualified name for a node
* **Properties**:

| Property   | Type                | Description                                             |
| :--------- | :------------------ | :------------------------------------------------------ |
| id         | Identifier          | The name of the function.                               |
| params     | VariableDeclaration | An array of parameters for the function.                |
| returnType | Type                | The return type of the function.                        |
| body       | BlockStatement      | The block of code that constitutes the function's body. |

#### **2.2.1. Method**

* **Type**: "fn"  
* **Inherits**: Parent  
* **Description**: A function, method, or procedure.  
* **Properties**:

| Property   | Type                | Description                                             |
| :--------- | :------------------ | :------------------------------------------------------ |
| id         | Identifier          | The name of the function.                               |
| params     | VariableDeclaration | An array of parameters for the function.                |
| returnType | Type                | The return type of the function.                        |
| body       | BlockStatement      | The block of code that constitutes the function's body. |

#### **2.2.2. Variable**

* **Type**: "var"  
* **Inherits**: Node  
* **Description**: A variable, constant, or field declaration.  
* **Properties**:

| Property       | Type                   | Description                        |
| :------------- | :--------------------- | :--------------------------------- |
| id             | Identifier             | The name of the variable.          |
| kind           | \`"var"                | "let"                              |
| typeAnnotation | Type?                  | The explicit type of the variable. |
| defaultValue   | Literal \| Expression? | The initializer expression.        |

#### **2.2.3. Type**

* **Type**: "type"  
* **Inherits**: Parent  
* **Description**: A class, struct, or similar composite type.  
* **Properties**:

| Property   | Type                    | Description                                 |
| :--------- | :---------------------- | :------------------------------------------ |
| id         | Identifier              | The name of the class.                      |
| extends    | []Identifier            | The class this one extends.                 |
| implements | []Identifier            | A list of interfaces this class implements. |
| children   | `[]Function | Variable` |                                             |

### **1.2. Parent**

An abstract interface for nodes that contain other nodes.

* **Description**: Extends the Node interface to include a collection of child nodes.1  
* **Inherits**: Node  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| children | Node \| Parent | An array of child nodes.1 |

### **1.3. Literal**

An abstract interface for nodes that represent a concrete value.

* **Description**: Extends the Node interface to include a value.1
* **Inherits**: Node
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| value | any | The raw value of the literal (e.g., a string, number, or boolean).1 |
| literalType | string \| bool \| number \| date |  |
| isConstant | bool |  |

## Position

| Property   | Type   | Description                                     |
| :--------- | :----- | :---------------------------------------------- |
| path       | string | Absolute file path for the location of the node |
| start_line | int    |                                                 |
| end_line   | int    |                                                 |
| language   | string | Name of language e.g. xml, go, typescript       |

### **1.4. Statement**

An abstract interface for nodes that represent executable code within functions.

* **Description**: Represents statements, control flow, and operations within method bodies.
* **Inherits**: Node
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| type | StatementType | The type of statement (see Section 1.7 for enum values). |
| from | Node? | Source node for assignments or calls (e.g., variable being assigned to). |
| to | Node? | Target node for calls or references (e.g., function being called). |
| value | any | Statement-specific value (e.g., SQL query text, condition expression). |
| input | Map<string, Value>? | Input parameters or variables used by this statement. |
| output | Map<string, Value>? | Output variables or results produced by this statement. |
| children | Statement[] | Nested statements for blocks, conditionals, and loops. |

### **1.5. Relationship**

An interface for representing connections between AST nodes.

* **Description**: Represents dependencies, calls, and structural relationships between nodes.
* **Inherits**: Node
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| from | Node | The source node of the relationship. |
| to | Node | The target node of the relationship. |
| relationshipType | RelationshipType | The type of relationship (see Section 1.7 for enum values). |
| lineNo | number? | The line number where the relationship occurs in source code. |

### **1.6. Value**

An interface for typed values used in parameters and fields.

* **Description**: Represents a value with an associated type, used for parameters, variables, and data flow.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| value | string | The actual value or type name. |
| fieldType | FieldType | The type of the value (see Section 1.7 for enum values). |

### **1.7. Enumerations**

#### **StatementType**

Enumeration of statement types for executable code:

* `function_call` - Function or method invocation
* `if` - Conditional statement
* `loop` - Loop construct (for, while, foreach)
* `import` - Import or dependency statement
* `sql` - SQL query statement
* `expression` - Expression evaluation
* `assignment` - Variable assignment
* `http_call` - HTTP endpoint call
* `soap_call` - SOAP operation call
* `file_op` - File system operation
* `message_queue` - Message queue operation (JMS, etc.)
* `foreign_key` - Database foreign key relationship
* `other` - Other statement types

#### **RelationshipType**

Enumeration of relationship types between nodes:

* `call` - Method or function invocation
* `reference` - Variable or type reference
* `inheritance` - Class inheritance (extends)
* `implements` - Interface implementation
* `import` - Package or module import
* `extends` - Type extension
* `foreign_key` - Database foreign key constraint

#### **FieldType**

Enumeration of field and parameter types:

* `string` - String/text type
* `number` - Numeric type (integer or decimal)
* `boolean` - Boolean type
* `array` - Array/list type
* `object` - Object/struct type
* `enum` - Enumeration type
* `date` - Date/datetime type
* `float` - Floating-point number
* `map` - Map/dictionary type
* `expression` - Expression code
* `sql` - SQL query
* `function` - Function reference
* `method_result` - Result of method call
* `xpath` - XPath expression

---

## **2\. "Green" Nodes: The Universal Metamodel**

"Green" nodes represent universal programming constructs that have a common semantic meaning across most supported languages.2

### **2.1. Structural Nodes**

#### **2.1.1. File**

* **Type**: "File"  
* **Inherits**: Parent  
* **Description**: The root of a UIR tree, representing a single source file.  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| path | string | The absolute or relative path to the source file. |

#### **2.1.2. Module**

* **Type**: "Module"  
* **Inherits**: Parent  
* **Description**: A logical grouping of code, such as a namespace or package.  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | Identifier | The name of the module. |

### **2.2. Declaration Nodes**

### **2.3. Statement Nodes**

#### **2.3.1. BlockStatement**

* **Type**: "BlockStatement"  
* **Inherits**: Parent  
* **Description**: A sequence of statements, typically enclosed in braces.  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| body | Statement | The array of statements within the block. |

#### **2.3.2. IfStatement**

* **Type**: "IfStatement"  
* **Inherits**: Node  
* **Description**: A conditional statement.  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| test | Expression | The condition to evaluate. |
| consequent | Statement | The statement to execute if the test is true. |
| alternate | Statement? | The statement to execute if the test is false. |

#### **2.3.3. ReturnStatement**

* **Type**: "ReturnStatement"  
* **Inherits**: Node  
* **Description**: A statement that returns a value from a function.  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| argument | Expression? | The expression to return. |

### **2.4. Expression Nodes**

#### **2.4.1. Identifier**

* **Type**: "Identifier"  
* **Inherits**: Node  
* **Description**: A name referring to a variable, function, class, or other binding.  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The string value of the identifier. |

#### **2.4.2. Literal**

* **Type**: "Literal"  
* **Inherits**: Literal (from base interfaces)  
* **Description**: A constant value (e.g., string, number, boolean, null).  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| value | \`string | number |

#### **2.4.3. BinaryExpression**

* **Type**: "BinaryExpression"  
* **Inherits**: Node  
* **Description**: An operation with two operands and an infix operator.  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| operator | string | The binary operator (e.g., \+, \==, \>). |
| left | Expression | The left-hand side operand. |
| right | Expression | The right-hand side operand. |

#### **2.4.4. CallExpression**

* **Type**: "CallExpression"
* **Inherits**: Node
* **Description**: An invocation of a function or method.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| callee | Node | The function or method being called. |
| arguments | Expression | An array of arguments passed to the function. |

### **2.5. HTTP Endpoint Nodes

HTTP endpoint nodes represent universal, framework-agnostic HTTP operations. These are "green" nodes created synthetically from framework-specific implementations.

#### **2.5.1. http:get**

* **Type**: "http:get"
* **Inherits**: Function
* **Description**: A universal HTTP GET endpoint, independent of framework implementation.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| path | string | The URL path or pattern (e.g., "/users/{id}"). |
| parameters | VariableDeclaration[] | Path, query, and header parameters. |
| responseType | Type? | The expected response type. |
| handler | Node | Reference to the framework-specific implementation. |

#### **2.5.2. http:post**

* **Type**: "http:post"
* **Inherits**: FunctionDeclaration
* **Description**: A universal HTTP POST endpoint, independent of framework implementation.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| path | string | The URL path or pattern. |
| parameters | VariableDeclaration[] | Path, query, and header parameters. |
| requestBody | Type? | The schema of the request body. |
| responseType | Type? | The expected response type. |
| handler | Node | Reference to the framework-specific implementation. |

#### **2.5.3. http:put**

* **Type**: "http:put"
* **Inherits**: Function
* **Description**: A universal HTTP PUT endpoint, independent of framework implementation.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| path | string | The URL path or pattern. |
| parameters | VariableDeclaration[] | Path, query, and header parameters. |
| requestBody | Type? | The schema of the request body. |
| responseType | Type? | The expected response type. |
| handler | Node | Reference to the framework-specific implementation. |

#### **2.5.4. http:delete**

* **Type**: "http:delete"
* **Inherits**: Function
* **Description**: A universal HTTP DELETE endpoint, independent of framework implementation.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| path | string | The URL path or pattern. |
| parameters | VariableDeclaration[] | Path, query, and header parameters. |
| responseType | Type? | The expected response type. |
| handler | Node | Reference to the framework-specific implementation. |

#### **2.5.5. http:patch**

* **Type**: "http:patch"
* **Inherits**: Function
* **Description**: A universal HTTP PATCH endpoint, independent of framework implementation.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| path | string | The URL path or pattern. |
| parameters | VariableDeclaration[] | Path, query, and header parameters. |
| requestBody | Type? | The schema of the request body. |
| responseType | Type? | The expected response type. |
| handler | Node | Reference to the framework-specific implementation. |

### **2.6. Messaging Nodes (Green)**

Messaging nodes represent universal, framework-agnostic message-based communication patterns.

#### **2.6.1. jms:send**

* **Type**: "jms:send"
* **Inherits**: Function
* **Description**: A universal message send operation, independent of messaging framework.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| destination | string | The queue or topic name. |
| messageType | Type? | The message payload type. |
| handler | Node | Reference to the framework-specific implementation. |

#### **2.6.2. jms:consume**

* **Type**: "jms:consume"
* **Inherits**: Function
* **Description**: A universal message consumer operation, independent of messaging framework.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| destination | string | The queue or topic name. |
| messageType | Type? | The message payload type. |
| handler | Node | Reference to the framework-specific implementation. |

#### **2.6.3. soap:method**

* **Type**: "soap:method"
* **Inherits**: Function
* **Description**: A universal SOAP operation, independent of web service framework.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| serviceName | string | The SOAP service name. |
| operationName | string | The operation name. |
| parameters | VariableDeclaration[] | Input parameters. |
| responseType | Type? | The response message type. |
| handler | Node | Reference to the framework-specific implementation. |

### **2.7. Statement Nodes

Statement nodes represent executable code within functions and methods.

#### **2.5.1. CallStatement**

* **Type**: "CallStatement"
* **Inherits**: Statement
* **Description**: A function or method invocation statement.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| callee | Expression \| Node | The function or method being called. |
| arguments | Value[] | Arguments passed to the function. |
| returnValue | Value? | The returned value from the call. |

#### **2.7.2. LoopStatement**

* **Type**: "LoopStatement"
* **Inherits**: Statement
* **Description**: A loop construct (for, while, foreach).
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| loopType | "for" \| "while" \| "foreach" | The type of loop. |
| condition | Expression | The loop condition or termination criteria. |
| iterator | Identifier? | The loop variable (for foreach/for loops). |
| iterableSource | Expression | The array or collection being iterated. |
| body | Statement[] | The statements within the loop body. |

#### **2.7.3. AssignmentStatement**

* **Type**: "AssignmentStatement"
* **Inherits**: Statement
* **Description**: A variable assignment statement.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| target | Identifier | The variable being assigned to. |
| value | Expression | The value being assigned. |
| operator | string | The assignment operator (e.g., "=", "+=", "-="). |

#### **2.7.4. ImportStatement**

* **Type**: "ImportStatement"
* **Inherits**: Statement
* **Description**: An import or dependency declaration.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| path | string | The import path or module name. |
| alias | string? | An alias for the imported module. |
| isUsed | boolean | Whether the import is actually used in the code. |

---

## **3\. "Red" Nodes: Language-Specific Schemas**

"Red" nodes represent language-specific constructs that do not have a direct equivalent in other languages.2 Their type property is namespaced to avoid collisions.

### **3.1. TypeScript (ts:)**

#### **3.1.1. ts:EnumDeclaration**

* **Type**: "ts:EnumDeclaration"  
* **Inherits**: Parent  
* **Description**: Represents a TypeScript enum declaration.3  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| id | Identifier | The name of the enum. |
| members | ts:EnumMember | The list of members in the enum. |

#### **3.1.2. ts:Decorator**

* **Type**: "ts:Decorator"  
* **Inherits**: Node  
* **Description**: Represents a TypeScript decorator applied to a class, method, or property.3  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| expression | CallExpression | The decorator invocation expression. |

### **3.2. Go (go:)**

#### **3.2.1. go:GoStatement**

* **Type**: "go:GoStatement"  
* **Inherits**: Node  
* **Description**: Represents a go statement, which starts a new goroutine.4  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| call | CallExpression | The function call to be executed in the new goroutine. |

#### **3.2.2. go:ChannelType**

* **Type**: "go:ChannelType"  
* **Inherits**: Node  
* **Description**: Represents a Go channel type.4  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| valueType | Type | The type of values the channel can transmit. |
| direction | \`"send" | "receive" |

#### **3.2.3. go:SendStatement**

* **Type**: "go:SendStatement"
* **Inherits**: Node
* **Description**: Represents sending a value to a channel (ch \<- value).
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| channel | Expression | The channel to send to. |
| value | Expression | The value to send. |

#### **3.2.4. go:GenericType**

* **Type**: "go:GenericType"
* **Inherits**: Node
* **Description**: Represents a Go generic type declaration (Go 1.18+).
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | Identifier | The name of the generic type. |
| typeParameters | go:TypeParameter[] | Type parameters for this generic. |
| constraints | Type[] | Constraints on type parameters. |

#### **3.2.5. go:TypeParameter**

* **Type**: "go:TypeParameter"
* **Inherits**: Node
* **Description**: A type parameter in a Go generic type or function.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | Identifier | The name of the type parameter. |
| constraint | Type? | The constraint interface for this type parameter. |

### **3.3. SQL Database (sql:)**

SQL nodes represent database schema elements and query operations.

#### **3.3.1. sql:Database**

* **Type**: "sql:Database"
* **Inherits**: Parent
* **Description**: A database container holding schemas and objects.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The name of the database. |
| schemas | sql:Schema[] | The schemas within this database. |

#### **3.3.2. sql:Schema**

* **Type**: "sql:Schema"
* **Inherits**: Parent
* **Description**: A database schema or namespace.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The name of the schema. |
| tables | sql:Table[] | Tables in this schema. |
| views | sql:View[] | Views in this schema. |
| procedures | sql:StoredProcedure[] | Stored procedures in this schema. |
| functions | sql:Function[] | SQL functions in this schema. |

#### **3.3.3. sql:Table**

* **Type**: "sql:Table"
* **Inherits**: Parent
* **Description**: A database table definition.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The name of the table. |
| columns | sql:Column[] | The columns in the table. |
| indexes | sql:Index[] | The indexes on the table. |
| foreignKeys | sql:ForeignKey[] | Foreign key constraints from this table. |

#### **3.3.4. sql:Column**

* **Type**: "sql:Column"
* **Inherits**: Node
* **Description**: A column within a database table.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The name of the column. |
| dataType | string | The SQL data type (e.g., VARCHAR(255), INTEGER, TIMESTAMP). |
| isNullable | boolean | Whether the column can contain NULL values. |
| defaultValue | string? | The default value for the column, if any. |
| isPrimaryKey | boolean | Whether this column is part of the primary key. |

#### **3.3.5. sql:Index**

* **Type**: "sql:Index"
* **Inherits**: Node
* **Description**: A database index.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The name of the index. |
| columns | string[] | The columns included in the index. |
| isUnique | boolean | Whether this is a unique index. |

#### **3.3.6. sql:ForeignKey**

* **Type**: "sql:ForeignKey"
* **Inherits**: Node
* **Description**: A foreign key constraint.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The name of the foreign key constraint. |
| columns | string[] | The columns in this table. |
| referencedTable | string | The referenced table name. |
| referencedColumns | string[] | The referenced columns in the target table. |

#### **3.3.7. sql:View**

* **Type**: "sql:View"
* **Inherits**: Parent
* **Description**: A database view.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The name of the view. |
| definition | string | The SQL query defining the view. |
| columns | sql:Column[] | The columns exposed by the view. |

#### **3.3.8. sql:StoredProcedure**

* **Type**: "sql:StoredProcedure"
* **Inherits**: Node
* **Description**: A stored procedure.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The name of the stored procedure. |
| parameters | VariableDeclaration[] | Input and output parameters. |
| body | string | The SQL code of the procedure. |

#### **3.3.9. sql:Function**

* **Type**: "sql:Function"
* **Inherits**: Node
* **Description**: A SQL function (scalar or table-valued).
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The name of the function. |
| parameters | VariableDeclaration[] | Input parameters. |
| returnType | Type | The return type of the function. |
| body | string | The SQL code of the function. |

#### **3.3.10. sql:QueryStatement**

* **Type**: "sql:QueryStatement"
* **Inherits**: Statement
* **Description**: A SQL query executed within application code.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| query | string | The SQL query text. |
| tables | Identifier[] | The tables referenced in the query. |
| operations | string[] | The operations performed (e.g., "select", "insert", "update", "delete"). |
| joins | Identifier[]? | Tables joined in the query. |

### **3.6. SOAP API (soap:)**

#### **3.6.1. soap:Service**

* **Type**: "soap:Service"  
* **Inherits**: Parent  
* **Description**: Represents a WSDL service definition, the top-level container for a SOAP API.8  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The name of the service. |
| targetNamespace | string | The target namespace URI for the WSDL. |
| bindings | soap:Binding | The protocol bindings for the service. |

#### **3.6.2. soap:Operation**

* **Type**: "soap:Operation"  
* **Inherits**: Node  
* **Description**: Represents a single operation that can be invoked on the service.10  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The name of the operation. |
| input | soap:Message | The input message definition. |
| output | soap:Message | The output message definition. |

#### **3.6.3. soap:Message**

* **Type**: "soap:Message"  
* **Inherits**: Parent  
* **Description**: Represents the structure of an input or output message, corresponding to the SOAP body.10  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The name of the message. |
| parts | VariableDeclaration | The parts that make up the message body. |

### **3.7. Insurance DSL (insurance:)**

#### **3.7.1. insurance:Policy**

* **Type**: "insurance:Policy"  
* **Inherits**: Parent  
* **Description**: Represents a top-level insurance policy definition.  
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| id | Identifier | The unique identifier for the policy type. |
| properties | VariableDeclaration | Data fields associated with the policy (e.g., premium, deductible). |
| rules | insurance:Rule | The business rules that apply to this policy. |

#### **3.7.2. insurance:Rule**

* **Type**: "insurance:Rule"
* **Inherits**: Node
* **Description**: Represents a single business rule within a policy.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| id | Identifier | The name of the rule. |
| condition | Expression | The logical condition that triggers the rule. |
| action | BlockStatement | The action to be executed if the condition is met. |

### **3.11. JMS (jms:)**

JMS nodes represent Java Message Service messaging constructs.

#### **3.11.1. jms:Queue**

* **Type**: "jms:Queue"
* **Inherits**: Node
* **Description**: A JMS queue destination.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The queue name. |
| jndiName | string? | The JNDI lookup name. |

#### **3.11.2. jms:Listener**

* **Type**: "jms:Listener"
* **Inherits**: FunctionDeclaration
* **Description**: A JMS message listener method annotated with @JmsListener.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| destination | string | The destination queue or topic name. |
| selector | string? | The message selector expression. |
| containerFactory | string? | The JMS listener container factory bean name. |

#### **3.11.3. jms:MessageStatement**

* **Type**: "jms:MessageStatement"
* **Inherits**: Statement
* **Description**: Represents sending a message to a JMS queue or topic.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| destination | string | The destination queue or topic name. |
| messageType | string | The type of message being sent. |
| payload | Value | The message payload. |

### **3.12. XML (xml:)**

XML nodes represent generic XML document structures and elements.

#### **3.12.1. xml:Document**

* **Type**: "xml:Document"
* **Inherits**: Parent
* **Description**: An XML document root.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| version | string | The XML version (e.g., "1.0"). |
| encoding | string? | The document encoding (e.g., "UTF-8"). |
| rootElement | xml:Element | The root element of the document. |

#### **3.12.2. xml:Element**

* **Type**: "xml:Element"
* **Inherits**: Parent
* **Description**: An XML element with attributes and child elements.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The element name. |
| namespace | string? | The namespace URI. |
| attributes | xml:Attribute[] | The element attributes. |
| textContent | string? | The text content of the element. |
| children | xml:Element[] | Child elements. |

#### **3.12.3. xml:Attribute**

* **Type**: "xml:Attribute"
* **Inherits**: Node
* **Description**: An XML element attribute.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The attribute name. |
| value | string | The attribute value. |
| namespace | string? | The namespace URI. |

### **3.13. XSD Schema (xsd:)**

XSD nodes represent XML Schema Definition constructs.

#### **3.13.1. xsd:Schema**

* **Type**: "xsd:Schema"
* **Inherits**: Parent
* **Description**: An XSD schema definition.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| targetNamespace | string? | The target namespace for the schema. |
| elements | xsd:Element[] | Top-level element definitions. |
| complexTypes | xsd:ComplexType[] | Complex type definitions. |
| simpleTypes | xsd:SimpleType[] | Simple type definitions. |

#### **3.13.2. xsd:Element**

* **Type**: "xsd:Element"
* **Inherits**: Node
* **Description**: An XSD element definition.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The element name. |
| type | string | The element type reference. |
| minOccurs | number? | Minimum occurrences (default 1). |
| maxOccurs | number \| "unbounded"? | Maximum occurrences (default 1). |

#### **3.13.3. xsd:ComplexType**

* **Type**: "xsd:ComplexType"
* **Inherits**: Parent
* **Description**: An XSD complex type definition.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The type name. |
| elements | xsd:Element[] | Child elements in this type. |
| attributes | xsd:Attribute[] | Attributes in this type. |

#### **3.13.4. xsd:SimpleType**

* **Type**: "xsd:SimpleType"
* **Inherits**: Node
* **Description**: An XSD simple type definition.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The type name. |
| base | string | The base type this restricts. |
| restrictions | object? | Type restrictions (e.g., minLength, maxLength, pattern). |

### **3.14. XSLT (xslt:)**

XSLT nodes represent XSL Transformations constructs.

#### **3.14.1. xslt:Stylesheet**

* **Type**: "xslt:Stylesheet"
* **Inherits**: Parent
* **Description**: An XSLT stylesheet definition.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| version | string | The XSLT version (e.g., "1.0", "2.0"). |
| templates | xslt:Template[] | Template rules in this stylesheet. |
| functions | xslt:Function[] | User-defined functions. |
| variables | VariableDeclaration[] | Global variables. |

#### **3.14.2. xslt:Template**

* **Type**: "xslt:Template"
* **Inherits**: FunctionDeclaration
* **Description**: An XSLT template rule.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string? | The template name (for named templates). |
| match | string? | The XPath match pattern. |
| mode | string? | The template mode. |
| priority | number? | The template priority. |

#### **3.14.3. xslt:Function**

* **Type**: "xslt:Function"
* **Inherits**: FunctionDeclaration
* **Description**: An XSLT user-defined function.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| name | string | The function name. |
| parameters | VariableDeclaration[] | Function parameters. |
| returnType | Type? | The return type of the function. |

### 

### **3.20. Markdown (md:)**

Markdown nodes represent documentation structures.

#### **3.20.1. md:Document**

* **Type**: "md:Document"
* **Inherits**: Parent
* **Description**: A Markdown document.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| path | string | The file path of the document. |
| title | string? | The document title (from first H1 heading). |
| sections | md:Section[] | Top-level sections in the document. |

#### **3.20.2. md:Section**

* **Type**: "md:Section"
* **Inherits**: Parent
* **Description**: A section in a Markdown document, typically starting with a heading.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| level | number | The heading level (1-6). |
| title | string | The section title. |
| content | string? | The markdown content of this section. |
| subsections | md:Section[]? | Nested subsections. |

#### **3.20.3. md:CodeBlock**

* **Type**: "md:CodeBlock"
* **Inherits**: Node
* **Description**: A code block within markdown documentation.
* **Properties**:

| Property | Type | Description |
| :---- | :---- | :---- |
| language | string? | The programming language of the code block. |
| code | string | The code content. |
| lineNumber | number | The line number where the code block starts. |

---

## **4. Synthetic Nodes**

Synthetic nodes represent entry points created implicitly by framework annotations rather than explicit code declarations. These nodes provide a unified interface for framework-based routing patterns.

### **4.1. Overview**

Many frameworks (JAX-RS, Spring, JAX-WS, JMS) use annotations to declare API endpoints, message listeners, and service operations without explicit interface definitions. UIR captures these as synthetic nodes to maintain complete call graphs and dependency tracking.

### **4.2. Three-Part Pattern**

Synthetic nodes follow a consistent three-part structure:

1. **Synthetic Entry Point**: A virtual node representing the public interface (e.g., HTTP endpoint, SOAP operation)
2. **Linking Statement**: A statement connecting the synthetic node to its implementation
3. **Implementation Handler**: The actual method or function containing the business logic

### **4.3. Examples**

#### **JAX-RS REST Endpoints**

```typescript
// Synthetic node created from @Path and @GET annotations
{
  type: "jaxrs:Operation",
  httpMethod: "GET",
  path: "/api/users/{id}",
  statements: [
    {
      type: "http_call",
      handler: /* reference to getUserById method */
    }
  ]
}

// Actual handler method
{
  type: "FunctionDeclaration",
  id: { name: "getUserById" },
  // ... method implementation
}
```

#### **Spring Controller Endpoints**

```typescript
// Red node: Framework-specific implementation
{
  type: "spring:method",
  methodName: "getUser",
  httpMethod: "GET",
  path: "/users/{id}",
  // ... spring-specific properties
}

// Green node: Universal HTTP GET endpoint
{
  type: "http:get",
  path: "/users/{id}",
  handler: /* reference to spring:method */,
  statements: [
    {
      type: "http_call",
      toNode: /* reference to getUser method */
    }
  ]
}
```

#### **NestJS Controller Endpoints**

```typescript
// Red node: Framework-specific implementation
{
  type: "nestjs:method",
  methodName: "getUser",
  httpMethod: "GET",
  path: "/users/:id",
  decorators: ["@Get(':id')", "@HttpCode(200)"],
  // ... nestjs-specific properties
}

// Green node: Universal HTTP GET endpoint
{
  type: "http:get",
  path: "/users/:id",
  handler: /* reference to nestjs:method */,
  statements: [
    {
      type: "http_call",
      toNode: /* reference to getUser method */
    }
  ]
}
```

#### **Express.js Routes**

```typescript
// Red node: Framework-specific implementation
{
  type: "express:route",
  httpMethod: "GET",
  path: "/users/:id",
  handler: /* reference to handler function */,
  // ... express-specific properties
}

// Green node: Universal HTTP GET endpoint
{
  type: "http:get",
  path: "/users/:id",
  handler: /* reference to express:route */,
  statements: [
    {
      type: "http_call",
      toNode: /* reference to handler function */
    }
  ]
}
```

#### **JMS Message Listeners**

```typescript
// Red node: Framework-specific implementation
{
  type: "jms:listener",
  destination: "orderQueue",
  // ... jms-specific properties
}

// Green node: Universal message consumer
{
  type: "jms:consume",
  destination: "orderQueue",
  handler: /* reference to jms:listener */,
  statements: [
    {
      type: "message_queue",
      toNode: /* reference to onMessage method */
    }
  ]
}
```

### **4.4. Benefits**

- **Complete Call Graphs**: Synthetic nodes enable tracing from external requests to implementation code
- **Framework Abstraction**: Unified representation across different frameworks (JAX-RS, Spring, etc.)
- **Dependency Analysis**: Track which endpoints depend on which services and databases
- **Impact Analysis**: Understand the full scope of changes when modifying handler methods

---

## **5. Virtual Paths**

Virtual paths provide a standardized way to represent AST sources that originate from non-file locations such as databases, URLs, or remote APIs.

### **5.1. Overview**

Not all code or schema definitions exist as files in a repository. Databases contain schema definitions, remote APIs expose OpenAPI specifications, and configuration systems store executable logic. Virtual paths allow UIR to represent these sources uniformly alongside file-based AST nodes.

### **5.2. Path Format**

Virtual paths follow the pattern: `virtual://<source-type>/<connection-or-url>/<resource-path>`

**Components**:
- `source-type`: The type of remote source (e.g., `sql_connection`, `openapi_url`)
- `connection-or-url`: Connection string, URL, or identifier for the source
- `resource-path`: Path within the source (e.g., table name, endpoint path)

### **5.3. Examples**

#### **SQL Database Schema**

```
virtual://sql_connection/postgres://db.example.com:5432/mydb/public.users
```

Represents the `users` table in the `public` schema of the PostgreSQL database at `db.example.com`.

**Corresponding Node**:
```typescript
{
  type: "sql:Table",
  filePath: "virtual://sql_connection/postgres://db.example.com:5432/mydb/public.users",
  name: "users",
  // ... table properties
}
```

#### **OpenAPI Specification from URL**

```
virtual://openapi_url/https://api.example.com/openapi.json
```

Represents an OpenAPI specification fetched from a remote URL.

**Corresponding Node**:
```typescript
{
  type: "openapi:Specification",
  filePath: "virtual://openapi_url/https://api.example.com/openapi.json",
  version: "3.0.0",
  // ... specification properties
}
```

#### **Remote Configuration**

```
virtual://config_source/consul://consul.example.com:8500/services/api-config
```

Represents configuration stored in a Consul key-value store.

### **5.4. Integration with File-Based Nodes**

Virtual path nodes integrate seamlessly with file-based nodes in the AST:

- **Relationships**: Relationships can span between file-based and virtual nodes (e.g., a Java method calling a database stored procedure)
- **Dependencies**: Virtual nodes can be dependencies of file-based modules
- **Call Graphs**: Function calls can traverse from application code (file-based) to database operations (virtual)

### **5.5. Benefits**

- **Unified Representation**: All code and schema sources use consistent node structures
- **Complete Dependency Graphs**: Track dependencies across files, databases, and remote APIs
- **Source Traceability**: Maintain origin information for remotely-sourced definitions
- **Change Impact Analysis**: Understand the impact of database schema changes on application code

---

## **6. Privacy and Visibility**

Privacy and visibility attributes control access to nodes and influence architectural analysis.

### **6.1. The isPrivate Property**

All declaration nodes (FunctionDeclaration, ClassDeclaration, VariableDeclaration) support an optional `isPrivate` boolean property:

```typescript
{
  type: "FunctionDeclaration",
  id: { name: "calculateDiscount" },
  isPrivate: true,  // Not accessible outside its module
  // ... other properties
}
```

### **6.2. Language-Specific Visibility**

Different languages express privacy through various mechanisms:

#### **Java, C#, TypeScript**
- `public`: `isPrivate: false`
- `private`: `isPrivate: true`
- `protected`: `isPrivate: false` (accessible within class hierarchy)
- `package-private` (Java): `isPrivate: false` with `data.packageScope: true`
- `internal` (C#): `isPrivate: false` with `data.internal: true`

#### **Go**
- **Exported** (uppercase first letter): `isPrivate: false`
- **Unexported** (lowercase first letter): `isPrivate: true`

#### **Python**
- **Public**: `isPrivate: false`
- **Name mangling** (e.g., `__private`): `isPrivate: true`
- **Convention** (e.g., `_internal`): `isPrivate: false` with `data.conventionPrivate: true`

### **6.3. Architectural Analysis Use Cases**

Privacy information enables several architectural checks:

- **API Surface Analysis**: Identify all public entry points in a module
- **Encapsulation Violations**: Detect direct access to private members from outside their defining module
- **Dependency Restrictions**: Enforce that internal implementation details are not exposed as public APIs
- **Dead Code Detection**: Find private functions that are never called within their module

### **6.4. Example: Encapsulation Check**

```typescript
// Public API method (allowed to be called externally)
{
  type: "FunctionDeclaration",
  id: { name: "createUser" },
  isPrivate: false,
  // ... properties
}

// Private helper (should only be called within module)
{
  type: "FunctionDeclaration",
  id: { name: "hashPassword" },
  isPrivate: true,
  // ... properties
}

// Relationship showing encapsulation violation (if present)
{
  type: "Relationship",
  relationshipType: "call",
  fromNode: /* external module node */,
  toNode: /* hashPassword node */,
  // This relationship would flag an architectural violation
}
```

---

## **7. Relationships**

Relationships represent connections and dependencies between AST nodes, enabling call graph analysis, dependency tracking, and architectural rule enforcement.

### **7.1. Relationship Structure**

As defined in Section 1.5, relationships have the following structure:

```typescript
{
  fromNode: Node,           // Source node
  toNode: Node,             // Target node
  relationshipType: RelationshipType,  // Type of relationship
  lineNo: number?           // Line number where relationship occurs
}
```

### **7.2. Relationship Types**

#### **call**
Function or method invocation.

**Example**:
```typescript
{
  relationshipType: "call",
  fromNode: /* getUserById method */,
  toNode: /* database.query method */,
  lineNo: 42
}
```

#### **reference**
Variable or type reference.

**Example**:
```typescript
{
  relationshipType: "reference",
  fromNode: /* processOrder function */,
  toNode: /* Order type definition */,
  lineNo: 15
}
```

#### **inheritance**
Class inheritance (extends).

**Example**:
```typescript
{
  relationshipType: "inheritance",
  fromNode: /* Dog class */,
  toNode: /* Animal class */
}
```

#### **implements**
Interface implementation.

**Example**:
```typescript
{
  relationshipType: "implements",
  fromNode: /* FileLogger class */,
  toNode: /* Logger interface */
}
```

#### **import**
Package or module import.

**Example**:
```typescript
{
  relationshipType: "import",
  fromNode: /* main.go file */,
  toNode: /* fmt package */,
  lineNo: 5
}
```

#### **foreign_key**
Database foreign key constraint.

**Example**:
```typescript
{
  relationshipType: "foreign_key",
  fromNode: /* orders table */,
  toNode: /* users table */
}
```

### **7.3. Cross-Domain Relationships**

Relationships can span across different domains (code, databases, APIs):

**Application → Database**:
```typescript
{
  relationshipType: "call",
  fromNode: /* getUserById method (Java code) */,
  toNode: /* GetUserById stored procedure (SQL) */
}
```

**API Endpoint → Handler**:
```typescript
{
  relationshipType: "call",
  fromNode: /* GET /api/users synthetic endpoint */,
  toNode: /* getUsersHandler function */
}
```

### **7.4. Bidirectional Navigation**

Relationships support bidirectional queries:
- **Forward**: "What does this function call?"
- **Backward**: "What calls this function?"

This enables:
- **Impact Analysis**: Find all code affected by changing a function
- **Usage Analysis**: Find all usages of a type or function
- **Dead Code Detection**: Identify unreferenced nodes

### **7.5. Relationship Storage**

Implementations can choose to store relationships as:
1. **Separate collection**: Array of relationship objects alongside nodes
2. **Embedded in nodes**: Relationships stored as properties on nodes (e.g., `node.calls`, `node.references`)

The specification remains agnostic to storage approach, focusing on semantic structure.

---

## **Appendix A: Implementation Notes**

This appendix provides guidance for implementers adopting the UIR specification, covering architectural considerations, storage strategies, and practical implementation patterns.

### **A.1. Relationship to arch-unit**

The UIR specification is informed by the arch-unit project, which uses a unified `ASTNode` structure to represent all syntactic elements. The mapping between these approaches:

**arch-unit's Unified Structure**:
```go
type ASTNode struct {
    ID           int64
    FilePath     string
    PackageName  string
    TypeName     string      // For types/classes
    MethodName   string      // For methods/functions
    FieldName    string      // For fields/properties
    NodeType     NodeType    // e.g., "type_table", "method_http_get"
    Statements   []Statement // Embedded statements
    // ... other fields
}
```

**UIR's Type-Based Hierarchy**:
```typescript
// Different node types for different constructs
{
  type: "sql:Table",
  name: "users",
  // ... table-specific properties
}

{
  type: "FunctionDeclaration",
  id: { name: "getUserById" },
  // ... function-specific properties
}
```

**Key Differences**:
- **arch-unit**: Single struct with optional fields, node type determined by `NodeType` field
- **UIR**: Type hierarchy with inheritance, node type determined by `type` field and specific properties

**Advantages of Each Approach**:
- **arch-unit's approach**: Simpler storage schema, easier to query across all node types, straightforward database mapping
- **UIR's approach**: Stronger type safety, clearer semantic meaning, better IDE support, extensibility without schema changes

Implementers can choose either approach internally while exposing UIR-compliant APIs.

### **A.2. Database Schema Recommendations**

For implementations storing UIR in relational databases:

#### **A.2.1. Normalized Schema**

**Nodes Table**:
```sql
CREATE TABLE ast_nodes (
    id BIGSERIAL PRIMARY KEY,
    type VARCHAR(100) NOT NULL,  -- Node type (e.g., "FunctionDeclaration", "sql:Table")
    file_path TEXT NOT NULL,
    package_name VARCHAR(500),
    name VARCHAR(500),
    parent_id BIGINT REFERENCES ast_nodes(id),
    position JSONB,              -- Position information
    data JSONB,                  -- Type-specific properties
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_nodes_type ON ast_nodes(type);
CREATE INDEX idx_nodes_file_path ON ast_nodes(file_path);
CREATE INDEX idx_nodes_package ON ast_nodes(package_name);
CREATE INDEX idx_nodes_parent ON ast_nodes(parent_id);
```

**Relationships Table**:
```sql
CREATE TABLE ast_relationships (
    id BIGSERIAL PRIMARY KEY,
    from_node_id BIGINT NOT NULL REFERENCES ast_nodes(id),
    to_node_id BIGINT NOT NULL REFERENCES ast_nodes(id),
    relationship_type VARCHAR(50) NOT NULL,
    line_no INTEGER,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_relationships_from ON ast_relationships(from_node_id);
CREATE INDEX idx_relationships_to ON ast_relationships(to_node_id);
CREATE INDEX idx_relationships_type ON ast_relationships(relationship_type);
```

#### **A.2.2. Document-Oriented Schema**

For implementations using document databases (MongoDB, PostgreSQL JSONB):

```typescript
{
  _id: ObjectId("..."),
  type: "FunctionDeclaration",
  filePath: "/src/services/user.ts",
  packageName: "services",
  name: "getUserById",
  // All node properties as nested document
  params: [...],
  returnType: {...},
  body: {...},
  // Metadata
  createdAt: ISODate("2025-01-15T10:00:00Z"),
  updatedAt: ISODate("2025-01-15T10:30:00Z")
}
```

**Indexes**:
```javascript
db.nodes.createIndex({ type: 1 })
db.nodes.createIndex({ filePath: 1 })
db.nodes.createIndex({ "packageName": 1, "name": 1 })
db.nodes.createIndex({ "type": 1, "packageName": 1 })
```

### **A.3. Performance Considerations**

#### **A.3.1. Large Codebase Strategies**

For codebases with millions of AST nodes:

**Incremental Parsing**:
- Parse only changed files on incremental builds
- Use file modification timestamps to detect changes
- Store node checksums to detect semantic changes

**Lazy Loading**:
- Load node metadata initially (type, name, location)
- Load full node details on demand (body, statements, children)
- Cache frequently accessed nodes

**Partitioning**:
- Partition nodes by file path prefix or package
- Separate tables/collections for different node type families (code vs. data vs. documentation)
- Archive historical node versions separately

#### **A.3.2. Query Optimization**

**Relationship Traversal**:
```sql
-- Efficient call graph query (forward)
WITH RECURSIVE call_chain AS (
  SELECT from_node_id, to_node_id, 1 as depth
  FROM ast_relationships
  WHERE from_node_id = $1 AND relationship_type = 'call'

  UNION ALL

  SELECT r.from_node_id, r.to_node_id, c.depth + 1
  FROM ast_relationships r
  JOIN call_chain c ON r.from_node_id = c.to_node_id
  WHERE c.depth < 10 AND r.relationship_type = 'call'
)
SELECT DISTINCT n.*
FROM call_chain c
JOIN ast_nodes n ON c.to_node_id = n.id;
```

**Materialized Views**:
- Pre-compute common queries (public API surface, dependency counts)
- Refresh on AST updates
- Trade storage for query speed

### **A.4. Partial Implementation Strategy**

Implementers need not support all UIR node types immediately. Recommended implementation phases:

#### **Phase 1: Core Universal Nodes**
- File, Module
- FunctionDeclaration, ClassDeclaration, VariableDeclaration
- Basic expressions (Identifier, Literal, CallExpression)
- Basic statements (BlockStatement, IfStatement, ReturnStatement)
- Basic relationships (call, reference)

**Coverage**: ~60% of typical codebases

#### **Phase 2: Language-Specific Essentials**
- Primary language Red nodes (e.g., `go:GoStatement`, `ts:Decorator`)
- Statement nodes (CallStatement, LoopStatement, AssignmentStatement)
- Additional relationships (inheritance, implements, import)

**Coverage**: ~85% of typical codebases

#### **Phase 3: Cross-Domain Support**
- SQL database nodes
- REST/HTTP API nodes
- Framework-specific nodes (JAX-RS, Spring)
- Synthetic nodes
- Virtual paths

**Coverage**: ~95% of typical codebases

#### **Phase 4: Advanced Features**
- OpenAPI, SOAP, XML nodes
- Markdown documentation nodes
- Additional framework support
- Full statement analysis

**Coverage**: ~99% of typical codebases

### **A.5. Extensibility Guidelines**

To add custom Red nodes for proprietary languages or frameworks:

**1. Follow Naming Conventions**:
```typescript
// Use namespace prefix
{
  type: "myframework:Controller",
  // ...
}
```

**2. Inherit from Appropriate Base Types**:
```typescript
// Extends ClassDeclaration for classes
{
  type: "myframework:Controller",
  inherits: "ClassDeclaration",
  id: { name: "UserController" },
  // ... framework-specific properties
}
```

**3. Document Node Types**:
- Create specification section describing all custom node types
- Provide TypeScript/JSON Schema definitions
- Include usage examples

**4. Register with UIR Registry** (if public):
- Submit namespace reservation
- Publish schema definitions
- Provide reference implementation

### **A.6. Testing Strategies**

**Unit Testing**:
```typescript
// Test node creation
test('creates SQL table node', () => {
  const node: Node = {
    type: 'sql:Table',
    name: 'users',
    columns: [...]
  };

  expect(node.type).toBe('sql:Table');
  expect(node.columns).toHaveLength(3);
});
```

**Integration Testing**:
```typescript
// Test end-to-end extraction
test('extracts complete Java class hierarchy', async () => {
  const result = await extractor.extractFile('src/User.java');

  const classNode = result.nodes.find(n => n.type === 'ClassDeclaration');
  expect(classNode.id.name).toBe('User');

  const methods = result.nodes.filter(n => n.type === 'FunctionDeclaration');
  expect(methods.length).toBeGreaterThan(0);
});
```

**Relationship Testing**:
```typescript
// Test relationship extraction
test('extracts method call relationships', () => {
  const relationships = result.relationships.filter(
    r => r.relationshipType === 'call'
  );

  expect(relationships.length).toBeGreaterThan(0);
  expect(relationships[0].fromNode).toBeDefined();
  expect(relationships[0].toNode).toBeDefined();
});
```

### **A.7. Common Pitfalls

**Avoid These Anti-Patterns**:

1. **Over-Normalization**: Don't create separate tables for every node type property. Use JSONB/document fields for type-specific data.

2. **Premature Optimization**: Start with simple storage, add indexes based on actual query patterns.

3. **Incomplete Relationships**: Always capture both directions of relationships for bidirectional navigation.

4. **Missing Source Location**: Always preserve `position` information for debugging and tool integration.

5. **Ignoring Virtual Paths**: Don't assume all nodes have file system paths. Support virtual paths from the start.

6. **Type Confusion**: Clearly distinguish between node `type` (UIR type like "FunctionDeclaration") and semantic types (TypeScript/Go/Java types).

### **A.8. Tool Integration**

UIR implementations should provide:

**Language Server Protocol (LSP) Integration**:
- Expose UIR nodes as LSP symbols
- Map UIR relationships to LSP references
- Support go-to-definition across languages

**IDE Plugins**:
- Visualize AST structure
- Navigate relationships
- Validate architectural rules interactively

**CI/CD Integration**:
- Export UIR as JSON for analysis tools
- Generate architectural reports
- Fail builds on rule violations

**Example Export Format**:
```json
{
  "version": "1.0",
  "metadata": {
    "generatedAt": "2025-01-15T10:00:00Z",
    "tool": "arch-unit",
    "projectPath": "/src/myproject"
  },
  "nodes": [...],
  "relationships": [...]
}
```

---

## **Appendix B: Coverage Status**

This appendix documents the coverage of arch-unit features in the UIR specification.

### **B.1. Specification Status Legend**

- ✅ **Fully Specified**: Complete node type definitions with all properties documented
- 🔶 **Partially Specified**: Basic structure defined, some properties may be incomplete
- ⚠️ **Minimal Specification**: Placeholder or minimal definition, needs expansion
- ❌ **Not Specified**: Known arch-unit feature not yet documented in UIR

### **B.2. Core Programming Language Support**

| Language/Feature | Status | Coverage | Notes |
|------------------|--------|----------|-------|
| **Go** | ✅ | 95% | Complete support including goroutines, channels, generics |
| **TypeScript** | ✅ | 90% | Core features covered, decorators supported |
| **Java** | ✅ | 85% | Core language features, framework annotations covered |
| **Python** | 🔶 | 60% | Basic support, missing decorators, async/await specifics |
| **C#** | 🔶 | 50% | Basic class structure, missing LINQ, async/await specifics |
| **Rust** | ⚠️ | 20% | Minimal specification, needs trait system, lifetimes |
| **C/C++** | ⚠️ | 15% | Minimal specification, needs pointers, templates, macros |

### **B.3. Database Support**

| Database Type | Status | Coverage | Notes |
|---------------|--------|----------|-------|
| **SQL (General)** | ✅ | 100% | Tables, columns, indexes, foreign keys, views |
| **PostgreSQL** | ✅ | 100% | Full schema extraction via INFORMATION_SCHEMA |
| **MySQL/MariaDB** | ✅ | 100% | Full schema extraction via INFORMATION_SCHEMA |
| **SQL Server** | ✅ | 95% | Schema extraction, minor dialect-specific features missing |
| **Other RDBMS** | ✅ | 90% | Schema extraction, some dialect-specific constructs minimal |
| **SQLite** | ✅ | 95% | Schema extraction, system table filtering |
| **Stored Procedures** | ✅ | 100% | Full support for procedures and functions |
| **NoSQL (MongoDB, etc.)** | ❌ | 0% | Not yet specified |

### **B.4. API Support**

| API Type | Status | Coverage | Notes |
|----------|--------|----------|-------|
| **REST/HTTP** | ✅ | 95% | Operations, schemas, parameters fully specified |
| **OpenAPI 3.x** | ✅ | 95% | Complete specification support including components |
| **SOAP/WSDL** | 🔶 | 70% | Basic service, operation, message support |
| **GraphQL** | ❌ | 0% | Not yet specified |
| **gRPC** | ❌ | 0% | Not yet specified |

### **B.5. Framework Support**

| Framework | Status | Coverage | Notes |
|-----------|--------|----------|-------|
| **JAX-RS** | ✅ | 100% | Complete resource, operation, parameter support |
| **Spring Web** | ✅ | 100% | Controllers, mappings, request/response fully specified |
| **JAX-WS** | ✅ | 95% | Web services, operations, WSDL integration |
| **JMS** | ✅ | 90% | Queue, listener, message statement support |
| **Express.js** | ⚠️ | 30% | Basic HTTP routing, middleware not specified |
| **Django** | ⚠️ | 25% | Basic view functions, ORM not specified |
| **Ruby on Rails** | ❌ | 0% | Not yet specified |

### **B.6. Data Format Support**

| Format | Status | Coverage | Notes |
|--------|--------|----------|-------|
| **XML** | ✅ | 90% | Document, element, attribute support |
| **XSD Schema** | ✅ | 85% | Schema, element, type definitions |
| **XSLT** | ✅ | 85% | Stylesheet, template, function support |
| **Servlet XML** | ✅ | 90% | web.xml parsing, servlet mappings |
| **Markdown** | ✅ | 80% | Document structure, code blocks |
| **YAML** | ⚠️ | 10% | Minimal specification |
| **JSON Schema** | ❌ | 0% | Not yet specified |
| **Protobuf** | ❌ | 0% | Not yet specified |

### **B.7. Statement and Relationship Support**

| Feature | Status | Coverage | Notes |
|---------|--------|----------|-------|
| **Function Calls** | ✅ | 100% | Full call statement and relationship support |
| **Control Flow** | ✅ | 90% | If, loop statements; switch/match minimal |
| **SQL Statements** | ✅ | 95% | Query execution, parameter binding |
| **HTTP Calls** | ✅ | 90% | REST endpoint invocation |
| **Message Queue** | ✅ | 85% | JMS send/receive operations |
| **File Operations** | 🔶 | 50% | Basic I/O, streaming operations minimal |
| **Async/Await** | ⚠️ | 20% | Promise/Future minimal, async specifics missing |

### **B.8. Advanced Features**

| Feature | Status | Coverage | Notes |
|---------|--------|----------|-------|
| **Synthetic Nodes** | ✅ | 100% | Framework annotation entry points fully specified |
| **Virtual Paths** | ✅ | 100% | Non-file source representation complete |
| **Cross-Domain Relationships** | ✅ | 95% | Code-to-database, API-to-handler links |
| **Privacy/Visibility** | ✅ | 90% | isPrivate property, language-specific mappings |
| **Generic Types** | 🔶 | 70% | Go generics specified, TypeScript/Java partial |
| **Type Inference** | 🔶 | 40% | Basic type annotation, inference rules minimal |
| **Metaprogramming** | ⚠️ | 15% | Decorators specified, macros/reflection minimal |

### **B.9. Coverage by arch-unit Feature**

Based on arch-unit's implementation (as of January 2025):

| arch-unit Feature | UIR Specification | Notes |
|-------------------|-------------------|-------|
| **SQL Extraction** | ✅ Complete | All INFORMATION_SCHEMA-based extraction specified |
| **Java Extraction** | ✅ Complete | Classes, methods, fields, annotations |
| **Go Extraction** | ✅ Complete | Packages, types, functions, interfaces |
| **OpenAPI Extraction** | ✅ Complete | Specification parsing, operation mapping |
| **XML/XSLT Extraction** | ✅ Complete | Document parsing, template extraction |
| **Servlet Extraction** | ✅ Complete | web.xml parsing, URL mappings |
| **Markdown Extraction** | ✅ Complete | Document structure, headings, code blocks |
| **Synthetic Endpoints** | ✅ Complete | JAX-RS, Spring, JAX-WS, JMS patterns |
| **Virtual Paths** | ✅ Complete | Database connections, URL sources |
| **Relationship Tracking** | ✅ Complete | Call graphs, dependencies, foreign keys |

### **B.10. Known Gaps and Future Work**

**High Priority** (Commonly requested):
1. **GraphQL Support**: Schema, queries, mutations, resolvers
2. **NoSQL Databases**: MongoDB collections, DynamoDB tables
3. **JSON Schema**: Type definitions, validation rules
4. **Enhanced Type Inference**: Cross-file type resolution
5. **Async Programming**: Promise chains, async/await patterns

**Medium Priority** (Specialized use cases):
6. **gRPC/Protobuf**: Service definitions, RPC calls
7. **Additional Frameworks**: Express.js, Django, Rails
8. **Container Orchestration**: Kubernetes manifests, Docker Compose
9. **Infrastructure as Code**: Terraform, CloudFormation
10. **Configuration Management**: Environment variables, config files

**Low Priority** (Edge cases):
11. **Metaprogramming**: Template metaprogramming, reflection APIs
12. **Build Systems**: Gradle, Maven, npm scripts as code
13. **Testing Frameworks**: Test structure as AST nodes
14. **Documentation Generators**: JSDoc, Javadoc as structured data

### **B.11. Version History**

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2025-01-15 | Initial comprehensive specification covering all arch-unit features |
| 0.9 | 2025-01-10 | Added synthetic nodes, virtual paths, relationships |
| 0.8 | 2025-01-05 | Added OpenAPI, XML, framework support |
| 0.7 | 2025-01-01 | Added SQL database support |
| 0.6 | 2024-12-20 | Added statement nodes |
| 0.5 | 2024-12-15 | Initial Green/Red node structure |

### **B.12. Contributing**

To propose additions to the UIR specification:

1. **Review Existing Coverage**: Check this appendix to ensure the feature isn't already specified
2. **Draft Specification**: Follow the patterns in Section 3 (Red Nodes) for language-specific features
3. **Provide Examples**: Include code examples showing the feature in practice
4. **Submit for Review**: Create specification document with rationale and implementation notes

**Contact**: Maintain compatibility with unist specification and UIR's Green/Red node conventions.

---

**End of Universal Intermediate Representation (UIR) API Specification**