package storage

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"
)

// SymbolFingerprints are the shape and body fingerprints a symbol delta stores.
type SymbolFingerprints struct{ Shape, Body int64 }

// SymbolFingerprint is the first eight bytes of a hex SHA-256 (a shape_hash or body_hash) as a signed
// int64, so it fits a portable bigint. Equal hashes give equal fingerprints; unequal hashes collide
// with probability 2^-64 per compared pair.
func SymbolFingerprint(hash string) (int64, error) {
	if len(hash) != 64 {
		return 0, fmt.Errorf("hash %q is not 64 hex characters", hash)
	}
	value, err := strconv.ParseUint(hash[:16], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("hash %q is not hex: %w", hash, err)
	}
	return int64(value), nil
}

// FoldDeclarations combines the fingerprints of every declaration of one canonical id in one snapshot.
// One declaration keeps its fingerprints. Several happen only because every `func init` of a package
// shares one canonical id (gavel TODO 34ea4d17): their distinct pairs are sorted by shape then body, and
// the shape and body fingerprints are each the first eight bytes of the SHA-256 of the big-endian
// shapes, respectively bodies, in that order. The inits of a package share one shape, so editing one
// init's body changes only the folded body fingerprint.
func FoldDeclarations(declarations []SymbolFingerprints) SymbolFingerprints {
	distinct := slices.Clone(declarations)
	slices.SortFunc(distinct, func(left, right SymbolFingerprints) int {
		return cmp.Or(cmp.Compare(left.Shape, right.Shape), cmp.Compare(left.Body, right.Body))
	})
	distinct = slices.Compact(distinct)
	if len(distinct) == 1 {
		return distinct[0]
	}
	var shapes, bodies []byte
	for _, declaration := range distinct {
		shapes = binary.BigEndian.AppendUint64(shapes, uint64(declaration.Shape))
		bodies = binary.BigEndian.AppendUint64(bodies, uint64(declaration.Body))
	}
	shape, body := sha256.Sum256(shapes), sha256.Sum256(bodies)
	return SymbolFingerprints{Shape: int64(binary.BigEndian.Uint64(shape[:8])), Body: int64(binary.BigEndian.Uint64(body[:8]))}
}
