package uir

import (
	"encoding/json"
	"strings"
	"testing"
)

// FindOptions reaches JSON through Violation.QueryOptions, and a non-nil func field
// fails encoding/json for the entire value — so a caller that set a Filter closure
// lost the whole violation, not just the filter.
func TestFindOptionsWithFilterMarshals(t *testing.T) {
	opts := FindOptions[Node]{
		Include: []NodeType{NodeTypeMethod},
		Depth:   3,
		Filter:  func(Node) bool { return true },
	}

	data, err := json.Marshal(opts)
	if err != nil {
		t.Fatalf("failed to marshal find options carrying a filter: %v", err)
	}
	if strings.Contains(string(data), "Filter") || strings.Contains(string(data), "filter") {
		t.Errorf("filter closure leaked into the encoding: %s", data)
	}
}
