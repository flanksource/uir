package storage

// BuiltinColumnProperties are the properties a builtin column symbol carries:
// sql_type is the column's SQL type as the catalogue or declaration writes it
// (its base name, such as varchar), field_type the portable UIR type (string,
// int, uuid, and so on; absent when the type has no portable mapping), and
// nullable whether it accepts NULL. A rule reads them as symbol.props.sql_type,
// symbol.props.field_type and symbol.props.nullable.
const BuiltinColumnProperties = `{"type":"object","properties":{"sql_type":{"type":"string"},"field_type":{"type":"string"},"nullable":{"type":"boolean"}}}`

// builtinPropertySchemas are the property schemas of builtin kinds. Producers
// cannot register one for a builtin kind, so they are part of every catalogue
// and are never stored.
var builtinPropertySchemas = map[string]JSON{"column": JSON(BuiltinColumnProperties)}
