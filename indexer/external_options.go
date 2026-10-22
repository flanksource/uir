package indexer

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/flanksource/uir/internal/canonicalhash"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
)

// PublishOptions identifies a producer and selects whether publication advances its location head.
// The zero value publishes a head. GitCommit identifies committed input. A document
// with GitPath binds original bytes; other external sources are stored in blobs.
type PublishOptions struct {
	Historical bool   `json:"historical,omitempty"`
	GitCommit  string `json:"git_commit,omitempty"`
	Producer   string `json:"producer,omitempty"`
	// BaseSnapshotID pins the storage base of a historical publication, including
	// when its location has no head or the module's primary head has moved.
	BaseSnapshotID string `json:"base_snapshot_id,omitempty"`
}

var gitObjectID = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

func (options PublishOptions) validate() error {
	if options.GitCommit != "" && !gitObjectID.MatchString(options.GitCommit) {
		return errors.New("options.git_commit must be a resolved Git object ID")
	}
	if options.Historical && (options.GitCommit == "" || strings.TrimSpace(options.Producer) == "") {
		return errors.New("historical publication requires options.git_commit and options.producer")
	}
	if options.BaseSnapshotID != "" {
		if !options.Historical {
			return errors.New("options.base_snapshot_id requires historical publication")
		}
		if _, err := uuid.Parse(options.BaseSnapshotID); err != nil {
			return fmt.Errorf("options.base_snapshot_id: %w", err)
		}
	}
	return nil
}

func (options PublishOptions) reason() storage.SnapshotReason {
	if options.Historical {
		return storage.ReasonHistorical
	}
	return storage.ReasonImport
}

func (options PublishOptions) configurationHash() string {
	digest := canonicalhash.New(externalConfigurationVersion)
	digest.Text(ExternalIndexerVersion)
	digest.Text(options.Producer)
	return digest.Sum()
}
