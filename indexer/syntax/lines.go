package syntax

import (
	"sort"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/flanksource/uir/storage"
)

// LineIndex converts zero-based byte offsets into one-based lines and UTF-16 columns.
type LineIndex struct {
	content []byte
	starts  []int
}

// NewLineIndex indexes the line starts of content.
func NewLineIndex(content []byte) LineIndex {
	starts := []int{0}
	for offset, value := range content {
		if value == '\n' {
			starts = append(starts, offset+1)
		}
	}
	return LineIndex{content: content, starts: starts}
}

// LineStart is the byte offset a one-based line opens at, or false when the content has no such line.
func (index LineIndex) LineStart(line int) (int, bool) {
	if line < 1 || line > len(index.starts) {
		return 0, false
	}
	return index.starts[line-1], true
}

func (index LineIndex) position(offset int) (int, int) {
	line := sort.Search(len(index.starts), func(i int) bool { return index.starts[i] > offset })
	column := 1
	for rest := index.content[index.starts[line-1]:offset]; len(rest) > 0; {
		value, size := utf8.DecodeRune(rest)
		column += max(1, utf16.RuneLen(value))
		rest = rest[size:]
	}
	return line, column
}

// RangeOf is the line and UTF-16 column range of a byte span.
func (index LineIndex) RangeOf(span storage.ByteSpan) storage.Range {
	startLine, startColumn := index.position(span[0])
	endLine, endColumn := index.position(span[1])
	return storage.Range{startLine, startColumn, endLine, endColumn}
}
