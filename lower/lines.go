package lower

import (
	"bytes"
	"sort"
)

// Lines indexes the lines of one body, so the lines of a body's many sites cost
// one pass over it rather than one count per site.
type Lines []int

func NewLines(body []byte) Lines {
	starts := Lines{0}
	for i, c := range body {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// Line is the 1-based line offset is on.
func (l Lines) Line(offset int) int {
	return sort.SearchInts(l, offset+1)
}

// ElementStart returns the offset of the '<' that opens the element whose start
// tag ends at offset, and the element's local name (a namespace prefix is not
// part of it); -1 and an empty tag when offset does not follow a start tag in
// body. A raw '<' cannot occur inside a start tag, so the nearest one before the
// offset opens it.
func ElementStart(body []byte, offset int64) (start int, tag string) {
	if offset <= 0 || offset > int64(len(body)) || body[offset-1] != '>' {
		return -1, ""
	}
	start = bytes.LastIndexByte(body[:offset], '<')
	if start < 0 {
		return -1, ""
	}
	name := body[start+1 : offset-1]
	if end := bytes.IndexAny(name, " \t\r\n/"); end >= 0 {
		name = name[:end]
	}
	if prefix := bytes.IndexByte(name, ':'); prefix >= 0 {
		name = name[prefix+1:]
	}
	return start, string(name)
}
