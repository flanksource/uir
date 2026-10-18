// Package canonicalhash is the indexer's one hash encoding, shared by the indexer and its syntax
// extractor so the content hash a syntax document records equals the one the typed path records.
package canonicalhash

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
)

// Hash digests a versioned, length-delimited encoding, so no two field sequences share bytes.
type Hash struct{ digest hash.Hash }

func New(version string) Hash {
	value := Hash{digest: sha256.New()}
	value.Text(version)
	return value
}

func (value Hash) Count(count int) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(count))
	_, _ = value.digest.Write(encoded[:])
}

func (value Hash) Text(text string) {
	value.Count(len(text))
	_, _ = value.digest.Write([]byte(text))
}

func (value Hash) Sum() string { return hex.EncodeToString(value.digest.Sum(nil)) }
