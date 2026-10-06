package lower_test

import (
	"bytes"
	"encoding/xml"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir/lower"
)

// positioned is an element that keeps where it was decoded, through DecodeAt.
type positioned struct {
	Name   string `xml:"NAME,attr"`
	Body   string `xml:",chardata"`
	Offset int64  `xml:"-"`
}

func (p *positioned) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	type plain positioned
	offset, err := lower.DecodeAt(decoder, start, (*plain)(p))
	p.Offset = offset
	return err
}

type positionedDoc struct {
	Items []positioned `xml:"Query"`
}

var _ = Describe("DecodeAt", func() {
	It("decodes each element and keeps the offset just past its start tag, which ElementStart reads back", func() {
		body := "<Doc>\n  <Query NAME=\"a\">SELECT 1</Query>\n  <Query NAME=\"b\">SELECT 2</Query>\n</Doc>"
		var doc positionedDoc
		Expect(xml.NewDecoder(bytes.NewReader([]byte(body))).Decode(&doc)).To(Succeed())

		lines := lower.NewLines([]byte(body))
		var got []string
		for _, item := range doc.Items {
			start, tag := lower.ElementStart([]byte(body), item.Offset)
			got = append(got, fmt.Sprintf("%s %s %s line %d", item.Name, item.Body, tag, lines.Line(start)))
		}
		Expect(got).To(Equal([]string{"a SELECT 1 Query line 2", "b SELECT 2 Query line 3"}))
	})

	It("returns the decode error of a malformed element", func() {
		var doc positionedDoc
		err := xml.NewDecoder(bytes.NewReader([]byte(`<Doc><Query NAME="a">SELECT 1</Doc>`))).Decode(&doc)
		Expect(err).To(MatchError(ContainSubstring("element <Query> closed by </Doc>")))
	})
})

var _ = Describe("ElementStart", func() {
	DescribeTable("names the element whose start tag ends at an offset",
		func(body string, offset int64, wantStart int, wantTag string) {
			start, tag := lower.ElementStart([]byte(body), offset)
			Expect([]any{start, tag}).To(Equal([]any{wantStart, wantTag}))
		},
		Entry("a plain start tag", "<A><B>x</B></A>", int64(6), 3, "B"),
		Entry("a start tag with attributes", `<A><B ID="1">x</B></A>`, int64(13), 3, "B"),
		Entry("a self-closing tag", `<A><B/></A>`, int64(7), 3, "B"),
		Entry("a prefixed tag, by its local name", `<A><x:B>y</x:B></A>`, int64(8), 3, "B"),
		Entry("an offset that follows no tag", "<A>text</A>", int64(5), -1, ""),
		Entry("an offset of zero", "<A/>", int64(0), -1, ""),
		Entry("an offset past the body", "<A/>", int64(9), -1, ""),
	)
})

var _ = Describe("Lines", func() {
	It("numbers the lines an offset is on from 1", func() {
		lines := lower.NewLines([]byte("ab\ncd\n\nef"))
		Expect([]int{lines.Line(0), lines.Line(2), lines.Line(3), lines.Line(6), lines.Line(7), lines.Line(8)}).To(Equal([]int{1, 1, 2, 3, 4, 4}))
	})
})
