package uir

import (
	"fmt"
	"strings"

	"github.com/flanksource/uir/internal/source"
)

// Global source reader for on-demand source code retrieval
var globalSourceReader = source.NewReader()

// Type aliases for internal use within uir package
type ASTNode = UIRNode
type ASTStatement = Statement
type ASTQueryOptions struct {
	Name      string
	Type      string
	Package   string
	Query     string
	Languages []string
}

func (a ASTNode) Key() string {
	return a.String()
}

// TableName specifies the table name for ASTNode
func (ASTNode) TableName() string {
	return "ast_nodes"
}

// NodeTypeCategory represents the high-level category of a node type
type NodeTypeCategory int

const (
	NodeTypeCategoryType NodeTypeCategory = iota
	NodeTypeCategoryMethodInbound
	NodeTypeCategoryMethodOutbound
	NodeTypeCategoryField
	NodeTypeCategoryOther
)

// GetNodeTypeCategory returns the category for a given node type
func GetNodeTypeCategory(nt NodeType) NodeTypeCategory {
	// Check for type nodes
	if nt == NodeTypeType || strings.HasPrefix(string(nt), "type_") {
		return NodeTypeCategoryType
	}

	// Default category
	return NodeTypeCategoryOther
}

// RelationshipType constants for relationship types
const (
	RelationshipCall        = "call"
	RelationshipReference   = "reference"
	RelationshipInheritance = "inheritance"
	RelationshipImplements  = "implements"
	RelationshipImport      = "import"
	RelationshipExtends     = "extends"
	RelationshipForeignKey  = "foreign_key"
)

// GetSourceCode retrieves the source code line for this node
func (n *ASTNode) GetSourceCode() (string, error) {
	location := n.GetLocation()
	if location.StartLine == nil {
		return "", fmt.Errorf("no start line information for node")
	}
	return globalSourceReader.GetLine(location.Path, *location.StartLine)
}

// GetSourceCodeLines retrieves a range of source code lines for this node
func (n *ASTNode) GetSourceCodeLines(start, end int) ([]string, error) {
	location := n.GetLocation()
	return globalSourceReader.GetLines(location.Path, start, end)
}

// GetFullSourceCode retrieves all source lines for this node (from StartLine to EndLine)
func (n *ASTNode) GetFullSourceCode() ([]string, error) {
	location := n.GetLocation()
	if location.EndLine == nil || *location.EndLine == 0 {
		// If EndLine is not set, just get the start line
		line, err := n.GetSourceCode()
		if err != nil {
			return nil, err
		}
		return []string{line}, nil
	}
	if location.StartLine == nil {
		return nil, fmt.Errorf("no start line information for node")
	}
	return n.GetSourceCodeLines(*location.StartLine, *location.EndLine)
}
