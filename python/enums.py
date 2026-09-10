"""UIR enumerations for Python."""

from enum import Enum


class Visibility(str, Enum):
    PUBLIC = "public"
    PRIVATE = "private"
    PROTECTED = "protected"
    INTERNAL = "internal"
    PACKAGE = "package"
    NA = ""


class AssignmentOp(str, Enum):
    ASSIGN = "="
    ADD = "+="
    SUBTRACT = "-="
    MULTIPLY = "*="
    DIVIDE = "/="
    MODULUS = "%="
    AND = "&="
    OR = "|="


class BinaryOp(str, Enum):
    AND = "&&"
    OR = "||"
    EQUAL = "=="
    NOT_EQUAL = "!="
    LESS = "<"
    LESS_EQUAL = "<="
    GREATER = ">"
    GREATER_EQUAL = ">="
    ADD = "+"
    SUBTRACT = "-"
    MULTIPLY = "*"
    DIVIDE = "/"
    MODULUS = "%"
    LIKE = "like"
    ILIKE = "ilike"
    REGEX = "regex"
    STARTS_WITH = "startsWith"
    ENDS_WITH = "endsWith"
    CONTAINS = "contains"


class UnaryOp(str, Enum):
    NOT = "!"
    NEG = "-"
    PLUS = "+"
    INCR = "++"
    DECR = "--"
    DEREF = "*"
    ADDR = "&"


class RecordFieldType(str, Enum):
    STRING = "string"
    NUMBER = "number"
    BOOLEAN = "boolean"
    ARRAY = "array"
    OBJECT = "object"
    ENUM = "enum"
    DATE = "date"
    FLOAT = "float"
    INT = "int"
    IP = "ip"
    CIDR = "cidr"
    URL = "url"
    EMAIL = "email"
    UUID = "uuid"
    JSON = "json"
    JSONB = "jsonb"
    YAML = "yaml"
    XML = "xml"
    CSV = "csv"
    PHONE = "phone"
    TEXT = "text"
    SECRET = "secret"
    MAP = "map"


class RecordReferenceType(str, Enum):
    FOREIGN_KEY = "foreignKey"
    SOFT_KEY = "softKey"


class RecordType(str, Enum):
    FUNCTION_INPUT = "input"
    FUNCTION_OUTPUT = "output"
    TABLE = "sql:table"
    VIEW = "sql:view"
    STORED_PROC = "sql:stored_proc"
    FUNCTION = "sql:function"
    CACHE = "cache"
    YAML = "file:yaml"
    JSON = "file:json"
    XML = "file:xml"
    CSV = "file:csv"
    OTHER = "other"


class ExpressionType(str, Enum):
    SQL = "sql"
    XPATH = "xpath"
    JSONPATH = "jsonpath"
    REGEX = "regex"
    JQ = "jq"
    YQ = "yq"
    JMESPATH = "jmespath"
    CEL = "cel"
    SPEL = "SpEL"
    OGNL = "ognl"


class CommentType(str, Enum):
    SINGLE_LINE = "single_line"
    MULTI_LINE = "multi_line"
    DOCUMENTATION = "documentation"


class NodeType(str, Enum):
    PACKAGE = "package"
    TYPE = "class"
    METHOD = "method"
    DEPENDENCY = "dependency"
    MODULE = "module"
    RECORD = "record"
    ENDPOINT = "endpoint"


class FlowType(str, Enum):
    INBOUND = "inbound"
    OUTBOUND = "outbound"
    PROXY = "proxy"
    NA = ""


class ASTStatementType(str, Enum):
    # Call types
    CALL = "call"
    CALL_PACKAGE = "call:package"
    REMOTE_READ = "call:read"
    REMOTE_WRITE = "call:write"
    RECORD_READ = "call:read:record"
    RECORD_WRITE = "call:write:record"
    CALL_API = "call:api"
    HTTP_CALL = "call:api:http"
    SOAP_CALL = "call:api:soap"
    GRPC_CALL = "call:api:grpc"
    GRAPHQL_CALL = "call:api:graphql"
    TCP_CALL = "call:api:tcp"

    # Declaration types
    DECL = "decl"
    IMPORT = "decl:import"
    DECLARE_VARIABLE = "decl:var"
    DECLARE_CONSTANT = "decl:const"
    DECLARE_FUNCTION = "decl:func"
    DECLARE_TYPE = "decl:type"
    EXPORT = "decl:export"

    # Reference types
    REF = "ref"
    REF_SCOPE_VAR = "ref:var"
    REF_PACKAGE_VAR = "ref:package"
    REF_ENDPOINT = "ref:endpoint"
    REF_RECORD = "ref:record"

    # File operations
    FILE_READ = "call:read:file"
    FILE_WRITE = "call:write:file"

    # Messaging
    KAFKA_CONSUMER = "call:read:kafka"
    KAFKA_PRODUCER = "call:write:kafka"
    JMS_CONSUMER = "call:read:jms"
    JMS_PRODUCER = "call:write:jms"

    # Control flow
    IF = "control:if"
    LOOP = "control:loop"
    LOOP_FOR = "control:loop:for"
    LOOP_WHILE = "control:loop:while"
    SWITCH = "control:switch"
    THROW = "control:throw"
    TRY = "control:try"
    BLOCK = "control:block"
    CONDITION = "control:condition"
    RETURN = "control:return"
    BREAK = "control:break"
    CONTINUE = "control:continue"

    # Data assignment
    EXPRESSION = "assignment:expression"
    ASSIGNMENT = "assignment"
    LITERAL = "assignment:literal"
    TUPLE = "assignment:tuple"
    VARIABLE = "assignment:variable"
    ARRAY = "assignment:array"
    BINARY = "assignment:binary"
    UNARY = "assignment:unary"
    CAST = "assignment:cast"

    # Other
    OTHER = "other"


class EndpointType(str, Enum):
    SOAP = "soap"
    JMS = "jms"
    KAFKA = "kafka"
    TCP = "tcp"
    HTTP = "http"
    GRPC = "grpc"
    GRAPHQL = "graphql"


class FunctionType(str, Enum):
    STATIC = "static"
    ANONYMOUS = "anonymous"
    INSTANCE = "instance"


class ASTErrorType(str, Enum):
    SYNTAX = "syntax"
    VALIDATION = "validation"
    COMPILATION = "compilation"
    RUNTIME = "runtime"
    CONFIGURATION = "configuration"
    NETWORK = "network"
    AUTH = "auth"
    AUTHZ = "authz"
    TIMEOUT = "timeout"
