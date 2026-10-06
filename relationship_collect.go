package uir

import "fmt"

// CollectRelationships returns what method's body calls, reads and writes, in
// walk order. Each relationship runs from the method, carries the source of the
// statement that makes it, and the guards that must hold to reach it.
//
// A method call and an endpoint call yield one call; a dispatch call yields one
// call to its declared target plus one dispatch per candidate; a record read or
// write yields one read or write. A statement that names no target (a nil
// Method, Endpoint or Record) has no relationship to report and yields none. A
// nil dispatch candidate panics. A method without a body returns nil.
func CollectRelationships(method MethodNode) []UIRRelationship {
	if method.Body == nil {
		return nil
	}
	var from Node = method
	var relationships []UIRRelationship
	WalkStatements(method.Body.Children, func(stmt Statement, scope StatementScope) bool {
		source, targets := statementTargets(stmt)
		for _, target := range targets {
			rel := NewRelationship(target.relationship, from, target.node).Build()
			rel.SourceCode = source
			rel.Guards = scope.Guards
			relationships = append(relationships, rel)
		}
		return true
	})
	return relationships
}

type relationshipTarget struct {
	relationship RelationshipType
	node         Node
}

// statementTargets returns the statement's own source and the nodes it relates
// to. A record statement's source is its statementBase, not the Expression that
// shadows it.
func statementTargets(stmt Statement) (SourceCode, []relationshipTarget) {
	switch s := stmt.(type) {
	case MethodCallStmt:
		return s.SourceCode, namedTargets(RelationshipTypeCall, s.Method)
	case DispatchCallStmt:
		targets := namedTargets(RelationshipTypeCall, s.Method)
		for i, candidate := range s.Candidates {
			if candidate == nil {
				panic(fmt.Sprintf("uir: CollectRelationships: dispatch call at %q has a nil candidate at index %d", s.GetLocation(), i))
			}
			targets = append(targets, relationshipTarget{RelationshipTypeDispatch, candidate})
		}
		return s.SourceCode, targets
	case EndpointCallStmt:
		return s.SourceCode, namedTargets(RelationshipTypeCall, s.Endpoint)
	case RecordReadStmt:
		return s.statementBase.SourceCode, namedTargets(RelationshipTypeRead, s.Record)
	case RecordWriteStmt:
		return s.statementBase.SourceCode, namedTargets(RelationshipTypeWrite, s.Record)
	}
	return SourceCode{}, nil
}

func namedTargets(relationship RelationshipType, node Node) []relationshipTarget {
	if node == nil {
		return nil
	}
	return []relationshipTarget{{relationship, node}}
}
