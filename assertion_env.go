package uir

import (
	"encoding/json"
	"fmt"
	"strings"
)

// celKeyAliases re-exposes JSON keys that collide with CEL reserved words under
// a name a CEL expression can select.
var celKeyAliases = map[string]string{
	"package": "pkg",
	"type":    "clazz",
	"string":  "str",
}

// ToMap returns the UIR as its generic JSON map, with CEL-reserved keys aliased.
func (uir UIR) ToMap() (map[string]any, error) {
	b, err := json.Marshal(uir)
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}
	addCELAliases(m)
	return m, nil
}

// AssertionEnv returns a CEL assertion environment with stable UIR lookup helpers.
func (uir UIR) AssertionEnv() (map[string]any, error) {
	env, err := uir.ToMap()
	if err != nil {
		return nil, err
	}
	AddAssertionIndex(env)
	return env, nil
}

func addCELAliases(v any) {
	switch val := v.(type) {
	case map[string]any:
		for k, child := range val {
			addCELAliases(child)
			if alias, ok := celKeyAliases[k]; ok {
				val[alias] = child
			}
		}
	case []any:
		for _, item := range val {
			addCELAliases(item)
		}
	}
}

// AddAssertionIndex adds path-indexed UIR symbols to env under "symbols" (and
// "__uir_index"), returning the existing index when env already carries one.
func AddAssertionIndex(env map[string]any) map[string]any {
	if existing, ok := env["__uir_index"].(map[string]any); ok {
		return existing
	}

	index := map[string]any{}
	for _, fn := range collectAssertionFunctions(env) {
		if len(fn) == 0 {
			continue
		}

		key := functionAssertionKey(fn)
		if key == "" {
			continue
		}

		params := indexByName(fn["params"], "name", "field")
		returns := indexByName(fn["returns"], "name", "field")
		annotations := indexByName(fn["annotations"], "clazz", "type")
		properties := asMap(fn["properties"])
		statements := collectStatementMaps(fn)
		statementMap := indexStatements(statements)
		calls := indexCalls(statements)

		fn["_params"] = params
		fn["_returns"] = returns
		fn["_annotations"] = annotations
		fn["_properties"] = properties
		fn["_statements"] = statementMap
		fn["_calls"] = calls

		uniqueKey := addIndexedValue(index, key, fn)
		addIndexedParts(index, uniqueKey, params, returns, annotations, properties, statementMap, calls)

		if method := asString(fn["method"]); method != "" {
			uniqueMethodKey := addIndexedValue(index, "method."+method, fn)
			addIndexedParts(index, uniqueMethodKey, params, returns, annotations, properties, statementMap, calls)
		}

		if root := asString(properties["rootElement"]); root != "" {
			uniqueRootKey := addIndexedValue(index, "root."+root, fn)
			addIndexedParts(index, uniqueRootKey, params, returns, annotations, properties, statementMap, calls)
		}
	}

	env["__uir_index"] = index
	env["symbols"] = index
	return index
}

func collectAssertionFunctions(env map[string]any) []map[string]any {
	out := make([]map[string]any, 0)
	collectFunctionMaps(asList(env["functions"]), "", "", &out)
	collectTypeFunctions(asList(env["types"]), "", &out)
	collectPackageFunctions(asList(env["packages"]), &out)
	return out
}

func collectPackageFunctions(packages []any, out *[]map[string]any) {
	for _, raw := range packages {
		pkg := asMap(raw)
		if len(pkg) == 0 {
			continue
		}

		pkgName := firstString(pkg["pkg"], pkg["package"])
		collectFunctionMaps(asList(pkg["functions"]), pkgName, "", out)
		collectTypeFunctions(asList(pkg["types"]), pkgName, out)
	}
}

func collectTypeFunctions(types []any, parentPkg string, out *[]map[string]any) {
	for _, raw := range types {
		typ := asMap(raw)
		if len(typ) == 0 {
			continue
		}

		pkgName := firstString(typ["pkg"], typ["package"], parentPkg)
		typeName := firstString(typ["clazz"], typ["_name"], typ["type"])

		collectFunctionMaps(asList(typ["methods"]), pkgName, typeName, out)
		collectFunctionMap(asMap(typ["constructor"]), pkgName, typeName, out)
		collectFunctionMap(asMap(typ["constructor_"]), pkgName, typeName, out)
		collectFunctionMap(asMap(typ["destructor"]), pkgName, typeName, out)
		collectTypeFunctions(asList(typ["types"]), pkgName, out)
	}
}

func collectFunctionMaps(items []any, pkg, clazz string, out *[]map[string]any) {
	for _, raw := range items {
		collectFunctionMap(asMap(raw), pkg, clazz, out)
	}
}

func collectFunctionMap(fn map[string]any, pkg, clazz string, out *[]map[string]any) {
	if len(fn) == 0 {
		return
	}

	if pkgName := firstString(fn["pkg"], fn["package"], pkg); pkgName != "" {
		if _, ok := fn["package"]; !ok {
			fn["package"] = pkgName
		}
		if _, ok := fn["pkg"]; !ok {
			fn["pkg"] = pkgName
		}
	}

	if typeName := firstString(fn["clazz"], fn["type"], clazz); typeName != "" {
		if _, ok := fn["type"]; !ok {
			fn["type"] = typeName
		}
		if _, ok := fn["clazz"]; !ok {
			fn["clazz"] = typeName
		}
	}

	*out = append(*out, fn)
}

func functionAssertionKey(fn map[string]any) string {
	pkg := firstString(fn["pkg"], fn["package"])
	clazz := firstString(fn["clazz"], fn["type"])
	method := asString(fn["method"])
	if clazz == "" {
		clazz = method
	}
	return strings.Join(nonEmpty(pkg, clazz, method), ".")
}

func addIndexedParts(index map[string]any, key string, params, returns, annotations, properties, statements, calls map[string]any) {
	index[key+"._params"] = params
	index[key+"._returns"] = returns
	index[key+"._annotations"] = annotations
	index[key+"._properties"] = properties
	index[key+"._statements"] = statements
	index[key+"._calls"] = calls
}

func addIndexedValue(index map[string]any, key string, value any) string {
	instancesKey := key + "._instances"
	instances, _ := index[instancesKey].([]any)
	index[instancesKey] = append(instances, value)

	if _, exists := index[key]; !exists {
		index[key] = value
		return key
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s#%d", key, i)
		if _, exists := index[candidate]; !exists {
			index[candidate] = value
			return candidate
		}
	}
}

func indexByName(raw any, names ...string) map[string]any {
	out := map[string]any{}
	for _, item := range asList(raw) {
		m := asMap(item)
		for _, name := range names {
			if key := asString(m[name]); key != "" {
				out[key] = m
				break
			}
		}
	}
	return out
}

func collectStatementMaps(fn map[string]any) []map[string]any {
	var out []map[string]any
	body := asMap(fn["body"])
	collectStatements(asList(body["children"]), &out)
	return out
}

func collectStatements(items []any, out *[]map[string]any) {
	for _, item := range items {
		stmt := asMap(item)
		if len(stmt) == 0 {
			continue
		}
		*out = append(*out, stmt)

		collectStatements(asList(stmt["children"]), out)
		collectBlockStatements(stmt["body"], out)
		collectBlockStatements(stmt["then"], out)
		collectBlockStatements(stmt["else"], out)
		collectBlockStatements(stmt["catch"], out)
		for _, rawCase := range asList(stmt["cases"]) {
			collectBlockStatements(asMap(rawCase)["body"], out)
		}
	}
}

func collectBlockStatements(raw any, out *[]map[string]any) {
	if block := asMap(raw); len(block) > 0 {
		collectStatements(asList(block["children"]), out)
	}
}

func indexStatements(statements []map[string]any) map[string]any {
	out := map[string]any{}
	for i, stmt := range statements {
		key := firstString(stmt["signature"], stmt["statement_type"], stmt["type"])
		if key == "" {
			key = fmt.Sprintf("statement_%d", i)
		}
		addIndexedValue(out, key, stmt)
	}
	return out
}

func indexCalls(statements []map[string]any) map[string]any {
	out := map[string]any{}
	for _, stmt := range statements {
		if asString(stmt["statement_type"]) != "call" {
			continue
		}
		method := asMap(stmt["Method"])
		key := strings.Join(nonEmpty(firstString(method["pkg"], method["package"]), asString(method["method"])), ".")
		if key == "" {
			continue
		}
		addIndexedValue(out, key, stmt)
	}
	return out
}

func asMap(raw any) map[string]any {
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func asList(raw any) []any {
	if list, ok := raw.([]any); ok {
		return list
	}
	return nil
}

func asString(raw any) string {
	if value, ok := raw.(string); ok {
		return value
	}
	return ""
}

func firstString(values ...any) string {
	for _, value := range values {
		if s := asString(value); s != "" {
			return s
		}
	}
	return ""
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
