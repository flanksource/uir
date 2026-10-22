package storage

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strconv"
)

// SymbolFingerprints are the public, private, and content fingerprints a symbol delta stores, one per
// layer of the symbol's hashes (public_hash, private_hash, content_hash).
type SymbolFingerprints struct{ Public, Private, Content int64 }

// SymbolFingerprint is the first eight bytes of a hex SHA-256 (a public_hash, private_hash, or
// content_hash) as a signed int64, so it fits a portable bigint. Equal hashes give equal fingerprints;
// unequal hashes collide with probability 2^-64 per compared pair.
func SymbolFingerprint(hash string) (int64, error) {
	if len(hash) != 64 {
		return 0, fmt.Errorf("hash %q is not 64 hex characters", hash)
	}
	value, err := strconv.ParseUint(hash[:16], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("hash %q is not hex: %w", hash, err)
	}
	if value <= math.MaxInt64 {
		return int64(value), nil
	}
	return math.MinInt64 + int64(value&math.MaxInt64), nil
}

// FoldDeclarations combines the fingerprints of every declaration of one canonical id in one snapshot.
// One declaration keeps its fingerprints. Go never declares one id twice (each `func init` has its own
// discriminator), but a producer may declare one id in several files: their distinct triples are sorted
// by public, private, then content, and each layer's fingerprint is the first eight bytes of the SHA-256
// of that layer's big-endian fingerprints, in that order. Editing the body of one declaration that keeps
// its shape changes only the folded content fingerprint.
func FoldDeclarations(declarations []SymbolFingerprints) SymbolFingerprints {
	distinct := slices.Clone(declarations)
	slices.SortFunc(distinct, func(left, right SymbolFingerprints) int {
		return cmp.Or(cmp.Compare(left.Public, right.Public), cmp.Compare(left.Private, right.Private), cmp.Compare(left.Content, right.Content))
	})
	distinct = slices.Compact(distinct)
	if len(distinct) == 1 {
		return distinct[0]
	}
	var public, private, content []byte
	for _, declaration := range distinct {
		public = binary.BigEndian.AppendUint64(public, uint64(declaration.Public))
		private = binary.BigEndian.AppendUint64(private, uint64(declaration.Private))
		content = binary.BigEndian.AppendUint64(content, uint64(declaration.Content))
	}
	return SymbolFingerprints{Public: firstEightBytes(public), Private: firstEightBytes(private), Content: firstEightBytes(content)}
}

func firstEightBytes(encoded []byte) int64 {
	sum := sha256.Sum256(encoded)
	return int64(binary.BigEndian.Uint64(sum[:8]))
}
