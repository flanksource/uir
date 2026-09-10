package uir

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

type Hasher struct {
	hash []byte
}

func NewHasher(kind string) *Hasher {
	h := sha256.Sum256([]byte("uir:" + kind))
	return &Hasher{hash: h[:]}
}

func (h *Hasher) String() string {
	if h == nil {
		return ""
	}
	return hex.EncodeToString(h.hash)
}

func (h *Hasher) add(field, value string) {
	if h == nil {
		return
	}
	sum := sha256.New()
	sum.Write(h.hash)
	sum.Write([]byte{0})
	sum.Write([]byte(field))
	sum.Write([]byte{0})
	sum.Write([]byte(value))
	h.hash = sum.Sum(nil)
}

func (h *Hasher) AddString(field, value string) {
	h.add(field, value)
}

func (h *Hasher) AddBool(field string, value bool) {
	h.add(field, strconv.FormatBool(value))
}

func (h *Hasher) AddInt(field string, value int) {
	h.add(field, strconv.Itoa(value))
}

func (h *Hasher) AddFloat64(field string, value float64) {
	h.add(field, strconv.FormatFloat(value, 'g', -1, 64))
}

func (h *Hasher) AddStringPtr(field string, value *string) {
	if value == nil {
		h.add(field, "<nil>")
		return
	}
	h.add(field, *value)
}

func (h *Hasher) AddNode(field string, node Node) {
	if node == nil {
		h.add(field, "<nil>")
		return
	}
	h.add(field+".type", string(node.GetType()))
	h.add(field+".hash", node.Hash())
}

func (h *Hasher) AddNodeList(field string, nodes []Node) {
	h.AddInt(field+".len", len(nodes))
	for i, node := range nodes {
		h.AddNode(fmt.Sprintf("%s.%d", field, i), node)
	}
}

func (h *Hasher) AddStatements(field string, stmts []Statement) {
	h.AddInt(field+".len", len(stmts))
	for i, stmt := range stmts {
		h.add(fmt.Sprintf("%s.%d", field, i), HashStatement(stmt))
	}
}

func (h *Hasher) AddRecordFields(field string, fields []RecordField) {
	h.AddInt(field+".len", len(fields))
	for i, f := range fields {
		h.add(fmt.Sprintf("%s.%d", field, i), f.Hash())
	}
}

func (h *Hasher) AddMethods(field string, methods []MethodNode) {
	h.AddInt(field+".len", len(methods))
	for i, m := range methods {
		h.add(fmt.Sprintf("%s.%d", field, i), m.Hash())
	}
}

func (h *Hasher) AddTypes(field string, types []TypedNode) {
	h.AddInt(field+".len", len(types))
	for i, t := range types {
		h.add(fmt.Sprintf("%s.%d", field, i), t.Hash())
	}
}

func (h *Hasher) AddRecords(field string, records []ASTRecord) {
	h.AddInt(field+".len", len(records))
	for i, r := range records {
		h.add(fmt.Sprintf("%s.%d", field, i), r.Hash())
	}
}

func (h *Hasher) AddEndpoints(field string, endpoints []ASTEndpoint) {
	h.AddInt(field+".len", len(endpoints))
	for i, e := range endpoints {
		h.add(fmt.Sprintf("%s.%d", field, i), e.Hash())
	}
}

func (h *Hasher) AddStringSlice(field string, values []string) {
	h.AddInt(field+".len", len(values))
	for i, v := range values {
		h.AddString(fmt.Sprintf("%s.%d", field, i), v)
	}
}

func (h *Hasher) AddTypeReference(field string, ref *TypeReference) {
	if ref == nil {
		h.add(field, "<nil>")
		return
	}
	h.add(field, hashTypeReference(*ref))
}

func (h *Hasher) AddTypedValue(field string, value *TypedValue) {
	if value == nil {
		h.add(field, "<nil>")
		return
	}
	h.add(field, hashTypedValue(*value))
}

func (h *Hasher) AddValidationValue(field string, value ValidationValue) {
	h.add(field, hashValidationValue(value))
}
