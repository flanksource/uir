package lower

import "encoding/xml"

// DecodeAt decodes the element start opens into v and returns the decoder's
// input offset just past its start tag: the offset a Site is recorded at, which
// ElementStart reads back.
//
// It is the body of an UnmarshalXML that keeps its element's position. v must
// not be the type being unmarshalled, or decoding recurses into the same
// UnmarshalXML; decode into a plain alias of it instead:
//
//	func (q *Query) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
//		type plain Query
//		offset, err := lower.DecodeAt(d, start, (*plain)(q))
//		q.Offset = offset
//		return err
//	}
func DecodeAt(decoder *xml.Decoder, start xml.StartElement, v any) (int64, error) {
	offset := decoder.InputOffset()
	return offset, decoder.DecodeElement(v, &start)
}
