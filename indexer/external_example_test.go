package indexer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// updateExamplesEnv regenerates the docs example publications from the fixture when set to 1.
const updateExamplesEnv = "UIR_UPDATE_EXAMPLES"

// examplePublications are the docs example files, docs/examples/import, and the fixture publication
// each one holds.
func examplePublications() map[string]Publication {
	return map[string]Publication{"co-prod.json": prodPublication("r1", "100"), "co-prod-plana.json": planAPublication()}
}

func encodePublication(publication Publication) []byte {
	GinkgoHelper()
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	Expect(encoder.Encode(publication)).To(Succeed())
	return encoded.Bytes()
}

var _ = Describe("external publication docs example", func() {
	It("is the fixture the publication specs publish", func() {
		for file, publication := range examplePublications() {
			path := filepath.Join("..", "docs", "examples", "import", file)
			encoded := encodePublication(publication)
			if os.Getenv(updateExamplesEnv) == "1" {
				Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
				Expect(os.WriteFile(path, encoded, 0o644)).To(Succeed())
			}
			stored, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(stored)).To(Equal(string(encoded)), "%s is stale; regenerate it with %s=1 go test ./indexer", path, updateExamplesEnv)
			var decoded Publication
			Expect(json.Unmarshal(stored, &decoded)).To(Succeed())
			Expect(decoded).To(Equal(publication), "%s decodes to the publication it encodes", path)
		}
	})
})
