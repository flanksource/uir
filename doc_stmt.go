package uir

import (
	"fmt"

	"github.com/flanksource/clicky/api"
)

type DocType string

const (
	DocTypeH1         DocType = "h1"
	DocTypeH2         DocType = "h2"
	DocTypeH3         DocType = "h3"
	DocTypeH4         DocType = "h4"
	DocTypeH5         DocType = "h5"
	DocTypeH6         DocType = "h6"
	DocTypeComment    DocType = "comment"
	DocTypeParagraph  DocType = "paragraph"
	DocTypeDocument   DocType = "document"
	DocTypeBlockquote DocType = "blockquote"
	DockTypeSnippet   DocType = "snippet"
	DocTypeList       DocType = "list"
	DocTypeListItem   DocType = "list_item"
	DocTypeCodeBlock  DocType = "code_block"
)

func (d DocType) GetStatementType() StatementType {
	return ASTStatementTypeDoc
}

type DocStmt struct {
	statementBase
	DocType  DocType   `json:"doc_type,omitempty"`
	Content  string    `json:"content,omitempty"`
	Style    string    `json:"style,omitempty"`
	Children []DocStmt `json:"children,omitempty" gorm:"serializer:json"`
}

func (d DocStmt) GetStatementType() StatementType {
	return ASTStatementTypeDoc
}

func NewDoc(content string) *DocStmt {
	d := &DocStmt{

		DocType: DocTypeParagraph,
		Content: content,
	}
	d.Type = ASTStatementTypeDoc
	return d
}

func (d *DocStmt) WithHeading(level int) *DocStmt {
	d.DocType = DocType("h" + fmt.Sprintf("%d", level))
	return d
}

func (d DocStmt) Pretty() api.Text { return api.Text{Content: d.Content} }

func (d *DocStmt) GetChildren() []Statement {
	statements := make([]Statement, len(d.Children))
	for i, child := range d.Children {
		statements[i] = &child
	}
	return statements
}
