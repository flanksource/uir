package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// documentDigestVersion versions the encoding DocumentDigest hashes.
const documentDigestVersion = "uir-document-digest-v1"

// DocumentDigest is a document's key within its root and path: the SHA-256 of a length-delimited
// encoding of its source revision, package path, indexer version, coverage, and canonical content. The
// content is canonicalized by decoding it and encoding it again with sorted object keys and numbers kept
// as written, so a digest computed from the bytes a producer rendered equals one computed from the bytes
// PostgreSQL's jsonb returns. The package input hash is not part of it: a file whose rendering did not
// change keeps its document when its package's inputs do.
func DocumentDigest(document Document) (string, error) {
	canonical, err := canonicalJSON(document.Content)
	if err != nil {
		return "", fmt.Errorf("digest document for %q: canonicalize content: %w", document.PathKey, err)
	}
	digest := sha256.New()
	for _, field := range [][]byte{
		[]byte(documentDigestVersion), document.SourceRevisionID[:], []byte(document.PackagePath),
		[]byte(document.IndexerVersion), []byte(document.Coverage), canonical,
	} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write(field)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func canonicalJSON(content JSON) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("content holds more than one JSON value")
	}
	return json.Marshal(value)
}
