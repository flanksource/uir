package uir

import (
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
)

// Comment represents a comment in the code
type Comment struct {
	Location `json:",inline"`
	Text     string      `json:"text,omitempty"`
	Type     CommentType `json:"type"`
	Context  string      `json:"context,omitempty"`
}

func (c Comment) Pretty() api.Text {
	return clicky.Text(c.Text, "text-gray-600 italic")
}

func (c Comment) WordCount() int {
	return len(strings.Fields(c.Text))
}
