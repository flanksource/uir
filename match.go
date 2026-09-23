package uir

// matchItem and its helpers are copied from github.com/flanksource/commons
// v1.59.0 collections/slice.go (MatchItem, matchPattern,
// normalizeMatchPatterns, sortPatterns, isExclusionOnly), Apache-2.0, so the
// model package does not link commons (and through it commons/logger and
// prometheus) into js/wasm builds.

import (
	"net/url"
	"slices"
	"strings"
)

// matchItem reports whether any pattern matches item. A "!" prefix negates a
// pattern and takes precedence over a positive match; "*" matches everything;
// a leading or trailing "*" matches a suffix or prefix.
func matchItem(item string, patterns ...string) (matches, negated bool) {
	if len(patterns) == 0 {
		return true, false
	}

	patterns = normalizeMatchPatterns(patterns)
	if len(patterns) == 0 {
		return false, false
	}

	slices.SortFunc(patterns, sortPatterns)

	for _, p := range patterns {
		if strings.HasPrefix(p, "!") && matchPattern(item, strings.TrimPrefix(p, "!")) {
			return false, true
		}
	}

	for _, pattern := range patterns {
		if matchPattern(item, pattern) {
			return true, false
		}
	}

	// All patterns were exclusions and none excluded the item.
	return isExclusionOnly(patterns), false
}

func matchPattern(item, pattern string) bool {
	if pattern == "*" {
		return true
	}

	itemLower := strings.ToLower(item)
	patternLower := strings.ToLower(pattern)

	if itemLower == patternLower {
		return true
	}

	if strings.HasPrefix(patternLower, "*") && strings.HasSuffix(patternLower, "*") {
		if strings.Contains(itemLower, strings.TrimPrefix(strings.TrimSuffix(patternLower, "*"), "*")) {
			return true
		}
	}

	if strings.HasPrefix(patternLower, "*") && strings.HasSuffix(itemLower, strings.TrimPrefix(patternLower, "*")) {
		return true
	}

	return strings.HasSuffix(patternLower, "*") && strings.HasPrefix(itemLower, strings.TrimSuffix(patternLower, "*"))
}

func normalizeMatchPatterns(patterns []string) []string {
	var normalized []string
	for _, pattern := range patterns {
		parts := strings.Split(pattern, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" && len(parts) > 1 {
				continue
			}

			decoded, err := url.QueryUnescape(part)
			if err != nil {
				continue
			}
			normalized = append(normalized, decoded)
		}
	}
	return normalized
}

func sortPatterns(a, b string) int {
	switch {
	case a == "!*":
		return -1
	case b == "!*":
		return 1
	case strings.HasPrefix(a, "!"):
		return -1
	case strings.HasPrefix(b, "!"):
		return 1
	}
	return 0
}

func isExclusionOnly(patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || !strings.HasPrefix(pattern, "!") {
			return false
		}
	}

	return true
}
