package storage_test

import (
	"encoding/json"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("JSON", func() {
	It("preserves JSON syntax instead of base64-encoding the byte slice", func() {
		value := struct {
			Payload storage.JSON `json:"payload"`
		}{Payload: storage.JSON(`{"kind":"record"}`)}

		encoded, err := json.Marshal(value)
		Expect(err).ToNot(HaveOccurred())
		Expect(encoded).To(MatchJSON(`{"payload":{"kind":"record"}}`))

		var decoded struct {
			Payload storage.JSON `json:"payload"`
		}
		Expect(json.Unmarshal(encoded, &decoded)).To(Succeed())
		Expect(decoded.Payload).To(MatchJSON(`{"kind":"record"}`))
	})

	It("rejects malformed values before they reach a database driver", func() {
		value := storage.JSON(`{"unterminated":`)
		_, err := value.Value()
		Expect(err).To(MatchError("invalid UIR JSON (16 bytes)"))
	})
})
