package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/flanksource/uir"
)

// Cache stores built graphs under their params: the canonical encoding of a
// request and of every input its graph reads (Params.Encode).
type Cache interface {
	// Load serves the graph stored under params through decode, or builds,
	// stores and decodes one, reporting whether the graph was stored. The
	// request that builds a graph decodes the bytes it stored, so it answers
	// exactly what every later hit will. A stored graph that does not decode
	// was written by another codec: it is replaced by a build.
	Load(ctx context.Context, params []byte, decode func([]byte) error, build func() ([]byte, error)) (bool, error)
}

// Params are what a stored graph is keyed by: two requests whose params encode
// alike build the same graph.
type Params struct {
	// Codec names the encoding of the stored result; bump it when the result
	// changes shape, so a graph another codec stored is keyed apart.
	Codec     string    `json:"codec"`
	Roots     []string  `json:"roots"`
	Direction Direction `json:"direction"`
	// Depth and Limit are as Build is given them, defaults applied.
	Depth         int                    `json:"depth"`
	Limit         int                    `json:"limit"`
	ExcludeKinds  []string               `json:"excludeKinds"`
	ExcludeGroups []string               `json:"excludeGroups"`
	Access        []uir.RelationshipType `json:"access"`
	Columns       bool                   `json:"columns"`
	Guards        bool                   `json:"guards"`
	// Inputs are the digests of everything else the graph reads, by name: the
	// generation of the index its callers come from, the schema its names are
	// spelled by, the scope its names are dispatched from.
	Inputs map[string]string `json:"inputs"`
}

// Encode is the canonical encoding of p: both exclusion lists lowercased,
// since ExcludeKindsGroups matches them case-insensitively, and sorted.
func (p Params) Encode() ([]byte, error) {
	p.ExcludeKinds, p.ExcludeGroups = lowerSorted(p.ExcludeKinds), lowerSorted(p.ExcludeGroups)
	return json.Marshal(p)
}

func lowerSorted(values []string) []string {
	lowered := make([]string, 0, len(values))
	for _, value := range values {
		lowered = append(lowered, strings.ToLower(value))
	}
	return slices.Sorted(slices.Values(lowered))
}

// ParamsKey is the key a cache stores params under: the hex SHA-256 of their
// encoding.
func ParamsKey(params []byte) string {
	sum := sha256.Sum256(params)
	return hex.EncodeToString(sum[:])
}

// Cached serves the result stored under params in cache, or builds one with
// build and stores it, as JSON, reporting whether it was stored. Without a
// cache every call builds.
func Cached[T any](ctx context.Context, cache Cache, params []byte, build func() (T, error)) (T, bool, error) {
	if cache == nil {
		result, err := build()
		return result, false, err
	}
	var result T
	decode := func(raw []byte) error {
		var zero T
		result = zero
		return json.Unmarshal(raw, &result)
	}
	encoded := func() ([]byte, error) {
		built, err := build()
		if err != nil {
			return nil, err
		}
		return json.Marshal(built)
	}
	hit, err := cache.Load(ctx, params, decode, encoded)
	return result, hit, err
}
