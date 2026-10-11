package indexer

import (
	"errors"
	"regexp"
	"strings"

	"github.com/flanksource/uir/storage"
)

// PublishOptions identifies a producer and selects whether publication advances its location head.
// The zero value publishes a head. GitCommit identifies committed input; external source bytes
// remain stored in blobs because producers may combine or transform Git files.
type PublishOptions struct {
	Historical bool   `json:"historical,omitempty"`
	GitCommit  string `json:"git_commit,omitempty"`
	Producer   string `json:"producer,omitempty"`
}

var gitObjectID = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

func (options PublishOptions) validate() error {
	if options.GitCommit != "" && !gitObjectID.MatchString(options.GitCommit) {
		return errors.New("options.git_commit must be a resolved Git object ID")
	}
	if options.Historical && (options.GitCommit == "" || strings.TrimSpace(options.Producer) == "") {
		return errors.New("historical publication requires options.git_commit and options.producer")
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
	digest := newCanonicalHash(externalConfigurationVersion)
	digest.text(ExternalIndexerVersion)
	digest.text(options.Producer)
	return digest.sum()
}
