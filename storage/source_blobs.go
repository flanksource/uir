package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"

	"gorm.io/gorm"
)

// ErrSourceBlobNotStored is the error ReadSourceBlob wraps when no blob is stored under the hash, so a
// caller with another verified source (a checkout, a pinned Git blob) can tell absence from failure.
var ErrSourceBlobNotStored = errors.New("source blob is not stored")

// ReadSourceBlobs reads and verifies generated sources in bounded SQL batches.
func ReadSourceBlobs(ctx context.Context, database *gorm.DB, hashes []string) (map[string][]byte, error) {
	if database == nil {
		return nil, errors.New("source blob batch requires a database")
	}
	hashes = slices.Clone(hashes)
	sort.Strings(hashes)
	hashes = slices.Compact(hashes)
	contents := make(map[string][]byte, len(hashes))
	const batchSize = 256
	for start := 0; start < len(hashes); start += batchSize {
		var blobs []SourceBlob
		if err := database.WithContext(ctx).Where("content_hash IN ?", hashes[start:min(start+batchSize, len(hashes))]).Find(&blobs).Error; err != nil {
			return nil, fmt.Errorf("load source blob batch: %w", err)
		}
		for _, blob := range blobs {
			content, err := verifiedBlob(blob)
			if err != nil {
				return nil, err
			}
			contents[blob.ContentHash] = content
		}
	}
	for _, hash := range hashes {
		if _, found := contents[hash]; !found {
			return nil, fmt.Errorf("content hash %s: %w", hash, ErrSourceBlobNotStored)
		}
	}
	return contents, ctx.Err()
}

// ReadSourceBlob returns the bytes stored under a source revision's content hash, and fails unless
// their SHA-256 is that hash.
func ReadSourceBlob(ctx context.Context, database *gorm.DB, contentHash string) ([]byte, error) {
	var blob SourceBlob
	err := database.WithContext(ctx).Where("content_hash = ?", contentHash).Take(&blob).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("content hash %s: %w", contentHash, ErrSourceBlobNotStored)
	}
	if err != nil {
		return nil, fmt.Errorf("load source blob %s: %w", contentHash, err)
	}
	return verifiedBlob(blob)
}

func verifiedBlob(blob SourceBlob) ([]byte, error) {
	digest := sha256.Sum256(blob.Content)
	if actual := hex.EncodeToString(digest[:]); actual != blob.ContentHash {
		return nil, fmt.Errorf("source blob stored under content hash %s has content hash %s", blob.ContentHash, actual)
	}
	if len(blob.Content) == 0 {
		return []byte{}, nil
	}
	return blob.Content, nil
}
