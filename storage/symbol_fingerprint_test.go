package storage_test

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strings"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// firstEight is the first eight bytes of the SHA-256 of the big-endian values, as an int64.
func firstEight(values ...int64) int64 {
	var encoded []byte
	for _, value := range values {
		encoded = binary.BigEndian.AppendUint64(encoded, uint64(value))
	}
	sum := sha256.Sum256(encoded)
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

var _ = Describe("symbol fingerprints", func() {
	DescribeTable("read the first eight bytes of a hex SHA-256 as a signed int64",
		func(hash string, expected int64) {
			value, err := storage.SymbolFingerprint(hash)
			Expect(err).ToNot(HaveOccurred())
			Expect(value).To(Equal(expected))
		},
		Entry("a small prefix", "0123456789abcdef"+strings.Repeat("0", 48), int64(0x0123456789abcdef)),
		Entry("the largest signed prefix", "7fffffffffffffff"+strings.Repeat("0", 48), int64(math.MaxInt64)),
		Entry("only the sign bit set", "8000000000000000"+strings.Repeat("0", 48), int64(math.MinInt64)),
		Entry("the sign bit set", strings.Repeat("f", 64), int64(-1)),
	)

	DescribeTable("reject what is not a 64-character hex hash",
		func(hash string) {
			_, err := storage.SymbolFingerprint(hash)
			Expect(err).To(MatchError(ContainSubstring("hash")))
		},
		Entry("empty", ""),
		Entry("short", strings.Repeat("a", 63)),
		Entry("not hex", "zz"+strings.Repeat("a", 62)),
	)

	It("keeps a single declaration's fingerprints unchanged", func() {
		single := storage.SymbolFingerprints{Shape: 7, Body: -3}
		Expect(storage.FoldDeclarations([]storage.SymbolFingerprints{single})).To(Equal(single))
	})

	It("folds several declarations of one id order-independently, shape and body apart", func() {
		first, second := storage.SymbolFingerprints{Shape: 5, Body: 9}, storage.SymbolFingerprints{Shape: 5, Body: 2}
		folded := storage.FoldDeclarations([]storage.SymbolFingerprints{first, second, first})
		Expect(folded).To(Equal(storage.SymbolFingerprints{Shape: firstEight(5, 5), Body: firstEight(2, 9)}),
			"distinct pairs sorted by shape then body; every func init shares one shape, so a body edit keeps the folded shape")
		Expect(storage.FoldDeclarations([]storage.SymbolFingerprints{second, first})).To(Equal(folded))
		edited := storage.FoldDeclarations([]storage.SymbolFingerprints{first, {Shape: 5, Body: 4}})
		Expect(edited.Shape).To(Equal(folded.Shape))
		Expect(edited.Body).ToNot(Equal(folded.Body))
	})
})
