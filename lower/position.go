package lower

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/flanksource/uir"
)

// Source says where a set of recorded references came from: the entity whose
// body holds them, the path reported for that body, and the exact bytes the
// body was decoded from, which is what turns a decode offset into a line.
type Source struct {
	From uir.Node
	Path string
	Body []byte
	// Fragment is set when Body is a fragment re-encoded out of another body:
	// the references are then positioned in that body.
	Fragment *Fragment
}

// Fragment places a fragment re-encoded out of a body back in that body. A
// re-encoded copy says nothing about the body's lines, so Starts carries the
// offset just past each start tag of the copy to the offset just past the same
// tag in Body.
type Fragment struct {
	Body   []byte
	Starts map[int64]int64
}

// Locate returns the body a site recorded at offset in body is positioned in,
// and its offset there. An offset that follows no start tag of the fragment
// comes back as 0, which no element sits at. A nil Fragment locates offset in
// body itself.
func (f *Fragment) Locate(body []byte, offset int64) ([]byte, int64) {
	if f == nil {
		return body, offset
	}
	return f.Body, f.Starts[offset]
}

// Attribute re-homes relationships onto this source: each runs from From (none
// when From is nil) and, unless Path is empty, sits in Path.
func (s Source) Attribute(rels []uir.UIRRelationship) []uir.UIRRelationship {
	for i := range rels {
		rels[i].From = nil
		if s.From != nil {
			from := s.From
			rels[i].From = &from
		}
		if s.Path != "" {
			rels[i].Path = s.Path
		}
	}
	return rels
}

// PositionError reports a recorded reference that cannot be placed on a line of
// its source body: its element was built in code, or decoded from bytes other
// than the ones handed to Relationships. It is a defect in the lowering, never a
// property of the source.
type PositionError struct {
	Kind   string
	Name   string
	Path   string
	Offset int64
	// Expected are the tags the referencing element may carry; Found is the
	// element actually at Offset, empty when the offset follows no start tag.
	Expected []string
	Found    string
}

func (e *PositionError) Error() string {
	return fmt.Sprintf("%s reference %q in %s has no position: decode offset %d should follow <%s> but follows %q",
		e.Kind, e.Name, e.Path, e.Offset, strings.Join(e.Expected, "|"), e.Found)
}

// Relationships resolves every recorded site against the body it was decoded
// from, in document order, as relationships from source.From to a reference
// to the site's name. Each sits on the line of its element (StartLine and
// EndLine), carries the site's Kind, Via (the element's tag when the site names
// none), Text as Content and guards. A site whose offset does not follow one of
// its Elements fails with a *PositionError.
func (r *Recorder) Relationships(source Source) ([]uir.UIRRelationship, error) {
	if len(r.Sites()) == 0 {
		return nil, nil
	}
	sites := slices.Clone(r.sites)
	sort.SliceStable(sites, func(i, j int) bool { return sites[i].Offset < sites[j].Offset })

	located, _ := source.Fragment.Locate(source.Body, 0)
	lines := NewLines(located)
	rels := make([]uir.UIRRelationship, 0, len(sites))
	for _, site := range sites {
		start, tag := ElementStart(source.Fragment.Locate(source.Body, site.Offset))
		if !slices.Contains(site.Elements, tag) {
			return nil, &PositionError{
				Kind: site.Kind, Name: site.Name, Path: source.Path, Offset: site.Offset,
				Expected: site.Elements, Found: tag,
			}
		}
		line := 0
		if start >= 0 {
			line = lines.Line(start)
		}
		rels = append(rels, site.relationship(cmp.Or(site.Via, tag), line))
	}
	return source.Attribute(rels), nil
}

func (s Site) relationship(via string, line int) uir.UIRRelationship {
	builder := uir.NewRelationship(s.Type, nil, uir.NewRef(uir.Identifier{Type: s.Name})).
		Kind(s.Kind).Via(via).Source("", line, line)
	if s.Text != "" {
		builder = builder.Text(s.Text)
	}
	rel := builder.Build()
	rel.Guards = s.Guards
	return rel
}

// ShiftLines moves relationships resolved against a body decoded with its
// leading lines trimmed back onto the lines of the body as it was stored, delta
// lines further down.
func ShiftLines(rels []uir.UIRRelationship, delta int) []uir.UIRRelationship {
	if delta == 0 {
		return rels
	}
	for i := range rels {
		if rels[i].StartLine != nil {
			rels[i].StartLine = new(*rels[i].StartLine + delta)
		}
		if rels[i].EndLine != nil {
			rels[i].EndLine = new(*rels[i].EndLine + delta)
		}
	}
	return rels
}

// InDocumentOrder merges the relationships of a body's parts, each already in
// document order, into one list ordered by line; relationships on one line keep
// the order of their parts.
func InDocumentOrder(parts ...[]uir.UIRRelationship) []uir.UIRRelationship {
	line := func(rel uir.UIRRelationship) int {
		if rel.StartLine == nil {
			return 0
		}
		return *rel.StartLine
	}
	merged := slices.Concat(parts...)
	sort.SliceStable(merged, func(i, j int) bool { return line(merged[i]) < line(merged[j]) })
	return merged
}
