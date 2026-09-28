package indexer

import "time"

func dirtyRevision(commit string, modifiedAt time.Time) string {
	return "git-" + commit + "-dirty-" + modifiedAt.UTC().Format("20060102T150405.000000000Z")
}
