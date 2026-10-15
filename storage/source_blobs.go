package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// ErrSourceBlobNotStored is the error ReadSourceBlob wraps when no blob is stored under the hash, so a
// caller with another verified source (a checkout, a pinned Git blob) can tell absence from failure.
var ErrSourceBlobNotStored = errors.New("source blob is not stored")

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
	digest := sha256.Sum256(blob.Content)
	if actual := hex.EncodeToString(digest[:]); actual != contentHash {
		return nil, fmt.Errorf("source blob stored under content hash %s has content hash %s", contentHash, actual)
	}
	return blob.Content, nil
}
