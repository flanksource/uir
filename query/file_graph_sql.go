package query

import (
	"fmt"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	sqlsource "github.com/flanksource/uir/sql"
)

// lowerSQLFile reads a SQL file's routines or anonymous T-SQL script and the calls, table reads and
// table writes its statements make, each located at the line its statement starts on. A call names a
// routine the file declares regardless of case, as SQL does; every other target is one a lookup in
// an index may place.
func lowerSQLFile(file GraphFile) (*loweredFile, error) {
	lowered, err := sqlsource.LowerFile(file.Path, file.Content, file.Dialect)
	if err != nil {
		return nil, &InvalidQueryError{Message: err.Error(), Hint: "Give a nonempty T-SQL script or a SQL file defining CREATE PROCEDURE or CREATE FUNCTION in the selected dialect.", Cause: err}
	}
	result := newLoweredFile(file.Path, fileLanguageSQL)
	routines := map[string]uir.Identifier{}
	for _, routine := range lowered.Routines {
		function, err := sqlFunction(file.Path, routine)
		if err != nil {
			return nil, err
		}
		result.declare(function)
		routines[strings.ToLower(function.name)] = function.identifier
	}
	for _, routine := range lowered.Routines {
		for _, relationship := range uir.CollectRelationships(routine) {
			target := relationship.To.GetIdentifier()
			if declared, found := routines[strings.ToLower(identifierQueryName(target))]; found && relationship.RelationshipType == uir.RelationshipTypeCall {
				relationship.To = declared.AsRef()
			} else {
				result.resolvable[target.IdentityKey()] = true
				result.labels[target.IdentityKey()] = identifierQueryName(target)
			}
			result.relationships = append(result.relationships, relationship)
		}
	}
	for _, diagnostic := range lowered.Diagnostics {
		result.diagnostics = append(result.diagnostics, FileDiagnostic{Path: file.Path, Line: diagnostic.Line, Message: diagnostic.Message})
	}
	return result, nil
}

func sqlFunction(path string, routine uir.MethodNode) (fileFunction, error) {
	if routine.StartLine == nil {
		return fileFunction{}, fmt.Errorf("routine %s in %s has no start line", routine.Method, path)
	}
	kind, _ := routine.Properties["routine_type"].(string)
	if kind == "" {
		return fileFunction{}, fmt.Errorf("routine %s in %s has no routine_type", routine.Method, path)
	}
	identifier := routine.GetIdentifier()
	return fileFunction{identifier: identifier, name: identifierQueryName(identifier), node: graph.Node{
		Kind: kind, Label: identifier.Method, Group: identifier.Package,
		Location:   &graph.Location{Path: path, Line: *routine.StartLine},
		Properties: map[string]string{GraphPropertySymbolKind: kind},
	}}, nil
}
