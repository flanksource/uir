package symboldiff

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/diff/myers"
)

// fileSegments partitions one side of a file: the text of each outermost symbol extent, keyed by
// symbol index, and everything else concatenated into the file-scope gap.
type fileSegments struct {
	owned map[int]string
	gap   string
}

// lineAttribution is the line diff of every pairing whose segments could be read, each file's
// file-scope diff, and the reason each unreadable file failed.
type lineAttribution struct {
	pairs        map[*pairing]LineCount
	pairFailures map[*pairing]string
	fileScope    map[string]LineCount
	failures     map[string]string
}

// blobSource reads one commit's files from a registered checkout that contains it.
type blobSource struct {
	commit   string
	checkout string
	err      error
}

func newBlobSource(ctx context.Context, scope rootScope, commit string) blobSource {
	checkout, err := scope.checkoutFor(ctx, commit)
	return blobSource{commit: commit, checkout: checkout, err: err}
}

// checkoutBytes is the file at the commit as a checkout writes it: the blob after the checkout's
// attributes and filters (eol, ident, LFS) are applied, which is what the indexer hashed.
func (source blobSource) checkoutBytes(ctx context.Context, path string) ([]byte, error) {
	return git(ctx, source.checkout, "cat-file", "--filters", source.commit+":./"+path)
}

// read returns the file's checkout bytes at the commit, verified against the source revision's
// content hash.
func (source blobSource) read(ctx context.Context, side *fileSide) ([]byte, error) {
	if source.err != nil {
		return nil, source.err
	}
	content, err := source.checkoutBytes(ctx, side.path)
	if err != nil {
		return nil, fmt.Errorf("read %s at %s: %w", side.path, source.commit, err)
	}
	if hash, expected := hashBytes(content), side.active.Source.ContentHash; hash != expected {
		return nil, fmt.Errorf("%s at %s has content hash %s, which does not match content hash %s of source revision %s",
			side.path, source.commit, hash, expected, side.active.Source.ID)
	}
	return content, nil
}

func hashBytes(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

// attributeLines reads both sides of every changed file, partitions them into symbol segments, and
// line-diffs each pairing's segments and each file's gap independently.
func attributeLines(ctx context.Context, files []changedFile, pairings []*pairing, before, after blobSource) lineAttribution {
	attribution := lineAttribution{
		pairs: map[*pairing]LineCount{}, pairFailures: map[*pairing]string{},
		fileScope: map[string]LineCount{}, failures: map[string]string{},
	}
	segments := map[*fileSide]fileSegments{}
	failed := map[*fileSide]string{}
	for _, file := range files {
		var reasons []string
		for _, side := range []struct {
			document *fileSide
			source   blobSource
		}{{file.before, before}, {file.after, after}} {
			if side.document == nil {
				continue
			}
			parts, err := readSegments(ctx, side.source, side.document)
			if err != nil {
				failed[side.document] = err.Error()
				reasons = append(reasons, err.Error())
				continue
			}
			segments[side.document] = parts
		}
		if len(reasons) > 0 {
			attribution.failures[file.path] = strings.Join(reasons, "; ")
			continue
		}
		attribution.fileScope[file.path] = countLines(segments[file.before].gap, segments[file.after].gap)
	}
	for _, pair := range pairings {
		attribution.attribute(pair, segments, failed)
	}
	return attribution
}

func (attribution lineAttribution) attribute(pair *pairing, segments map[*fileSide]fileSegments, failed map[*fileSide]string) {
	var texts [2]*string
	for index, side := range []*entry{pair.before, pair.after} {
		if side == nil || !side.owner() {
			continue
		}
		if reason, isFailed := failed[side.file]; isFailed {
			owner, name := ownerAndName(pair.reported().symbol.Identifier)
			attribution.pairFailures[pair] = fmt.Sprintf("line counts of %s need %s: %s", strings.TrimPrefix(owner+"."+name, "."), side.file.path, reason)
			return
		}
		text := segments[side.file].owned[side.index]
		texts[index] = &text
	}
	if texts[0] == nil && texts[1] == nil {
		return
	}
	var old, current string
	if texts[0] != nil {
		old = *texts[0]
	}
	if texts[1] != nil {
		current = *texts[1]
	}
	attribution.pairs[pair] = countLines(old, current)
}

func readSegments(ctx context.Context, source blobSource, side *fileSide) (fileSegments, error) {
	content, err := source.read(ctx, side)
	if err != nil {
		return fileSegments{}, err
	}
	return segmentFile(content, side)
}

// segmentFile cuts each outermost symbol extent out of the file, widened to whole lines when only
// whitespace precedes it on its first line and no other segment starts before the end of its last.
func segmentFile(content []byte, side *fileSide) (fileSegments, error) {
	owners := make([]int, 0, len(side.owners))
	for index := range side.owners {
		owners = append(owners, index)
	}
	sort.Ints(owners)
	segments := fileSegments{owned: make(map[int]string, len(owners))}
	var gap strings.Builder
	cursor := 0
	for position, index := range owners {
		start, end := side.content.Symbols[index].ExtentBytes[0], side.content.Symbols[index].ExtentBytes[1]
		if start < cursor || end > len(content) {
			return fileSegments{}, fmt.Errorf("%s: extent %v of %s does not fit the %d-byte file after byte %d", side.path, side.content.Symbols[index].ExtentBytes, side.content.Symbols[index].Key, len(content), cursor)
		}
		if lineStart := bytes.LastIndexByte(content[:start], '\n') + 1; lineStart >= cursor && len(bytes.TrimSpace(content[lineStart:start])) == 0 {
			start = lineStart
		}
		next := len(content)
		if position+1 < len(owners) {
			next = side.content.Symbols[owners[position+1]].ExtentBytes[0]
		}
		if newline := bytes.IndexByte(content[end:], '\n'); newline >= 0 && end+newline+1 <= next {
			end += newline + 1
		}
		gap.Write(content[cursor:start])
		segments.owned[index] = string(content[start:end])
		cursor = end
	}
	gap.Write(content[cursor:])
	segments.gap = gap.String()
	return segments, nil
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// countLines is the Myers line diff's insertions and deletions from before to after.
func countLines(before, after string) LineCount {
	script := myers.Diff(context.Background(), exactLines{before: splitLines(before), after: splitLines(after)})
	added, removed := script.Stat()
	return LineCount{Added: added, Removed: removed}
}

type exactLines struct{ before, after []string }

func (pair exactLines) LenA() int           { return len(pair.before) }
func (pair exactLines) LenB() int           { return len(pair.after) }
func (pair exactLines) Equal(a, b int) bool { return pair.before[a] == pair.after[b] }
