package main

import (
	"strings"

	"github.com/flanksource/clicky/rpc"
)

// databaseModuleOpenAPI documents the database-module routes UIR serves beside the commons-db
// connection service, which documents its own through connections.AddConnectionsOpenAPI.
func databaseModuleOpenAPI(spec *rpc.OpenAPISpec) {
	stringSchema := &rpc.OpenAPISchema{Type: "string"}
	moduleInput := &rpc.OpenAPISchema{Type: "object", Required: []string{"connection_id", "database_name", "name"}, Properties: map[string]*rpc.OpenAPISchema{
		"connection_id": {Type: "string", Format: "uuid"}, "database_name": stringSchema, "name": stringSchema,
	}}
	moduleView := &rpc.OpenAPISchema{Type: "object", Properties: map[string]*rpc.OpenAPISchema{
		"id": {Type: "string", Format: "uuid"}, "connection_id": {Type: "string", Format: "uuid"},
		"database_name": stringSchema, "name": stringSchema, "root_key": stringSchema, "created_at": {Type: "string", Format: "date-time"},
	}}
	capture := &rpc.OpenAPISchema{Type: "object", Properties: map[string]*rpc.OpenAPISchema{
		"id": {Type: "string", Format: "uuid"}, "started_at": {Type: "string", Format: "date-time"},
		"revision": stringSchema, "ordinal": {Type: "integer", Format: "int64"},
	}}
	diffInput := &rpc.OpenAPISchema{Type: "object", Required: []string{"from", "to"}, Properties: map[string]*rpc.OpenAPISchema{
		"from": {Type: "string", Format: "uuid"}, "to": {Type: "string", Format: "uuid"},
		"from_schema": stringSchema, "to_schema": stringSchema,
	}}
	diffResult := &rpc.OpenAPISchema{Type: "object", Properties: map[string]*rpc.OpenAPISchema{
		"summary": {Type: "object", AdditionalProperties: &rpc.OpenAPISchema{Type: "integer"}},
		"changes": {Type: "array", Items: &rpc.OpenAPISchema{Type: "object"}},
	}}
	object := &rpc.OpenAPISchema{Type: "object"}
	stringList := &rpc.OpenAPISchema{Type: "array", Items: stringSchema}
	moduleList := &rpc.OpenAPISchema{Type: "array", Items: moduleView}
	captureList := &rpc.OpenAPISchema{Type: "array", Items: capture}
	created := &rpc.OpenAPISchema{Type: "object", Properties: map[string]*rpc.OpenAPISchema{
		"module": moduleView, "capture": object, "capture_error": stringSchema,
	}}
	routes := []struct {
		path, method, summary, status string
		request, response             *rpc.OpenAPISchema
	}{
		{"/api/v1/database-modules/connections/{id}/databases", "post", "List the SQL databases a saved connection can reach", "200", nil, stringList},
		{"/api/v1/database-modules", "get", "List database modules", "200", nil, moduleList},
		{"/api/v1/database-modules", "post", "Register and capture a database", "201", moduleInput, created},
		{"/api/v1/database-modules/{id}", "get", "Read a database module", "200", nil, moduleView},
		{"/api/v1/database-modules/{id}", "delete", "Delete a database module and its captures", "204", nil, nil},
		{"/api/v1/database-modules/{id}/capture", "post", "Capture database schema metadata", "200", nil, object},
		{"/api/v1/database-modules/{id}/snapshots", "get", "List schema captures", "200", nil, captureList},
		{"/api/v1/database-modules/snapshots/{snapshot}/schemas", "get", "List captured schemas", "200", nil, stringList},
		{"/api/v1/database-modules/diff", "post", "Compare schema captures", "200", diffInput, diffResult},
	}
	for _, route := range routes {
		operation := rpc.OpenAPIOperation{Tags: []string{"Database modules"}, Summary: route.summary,
			Description: "Available only to local, same-origin requests.",
			Responses:   map[string]rpc.OpenAPIResponse{route.status: {Description: "Success"}}}
		if route.request != nil {
			operation.RequestBody = &rpc.OpenAPIRequestBody{Required: true, Content: map[string]rpc.OpenAPIMediaType{
				"application/json": {Schema: route.request},
			}}
		}
		if route.response != nil {
			operation.Responses[route.status] = rpc.OpenAPIResponse{Description: "Success", Content: map[string]rpc.OpenAPIMediaType{
				"application/json": {Schema: route.response},
			}}
		}
		for _, name := range []string{"id", "snapshot"} {
			if strings.Contains(route.path, "{"+name+"}") {
				operation.Parameters = append(operation.Parameters, rpc.OpenAPIParameter{Name: name, In: "path", Required: true,
					Schema: &rpc.OpenAPISchema{Type: "string", Format: "uuid"}})
			}
		}
		if spec.Paths[route.path] == nil {
			spec.Paths[route.path] = rpc.OpenAPIPath{}
		}
		spec.Paths[route.path][route.method] = operation
	}
}
