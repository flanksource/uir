// Package lower is the scaffolding a language's lowering shares when it records
// the references a source body makes: a Recorder that keeps each reference by
// its decode offset and the guards in force, resolves the offsets to lines of
// the exact bytes decoded and returns them as uir relationships, and DecodeAt,
// which decodes an encoding/xml element keeping that offset. What a reference
// is, which elements carry one and how a condition is built stay with the
// language.
package lower

import (
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir"
)

// Site is one place the lowering met a reference to something else.
type Site struct {
	// Type is the relationship the reference is drawn as.
	Type uir.RelationshipType
	// Kind is the producer's name for the reference (uir.UIRRelationship.Kind).
	Kind string
	// Via names the construct the reference is written in. Empty means the
	// referencing element's own tag.
	Via string
	// Name is the referenced name as authored.
	Name string
	// Text is the source excerpt the reference was read from, carried as the
	// relationship's Content; empty for none.
	Text string
	// Elements are the tags the referencing element may carry. The element found
	// at Offset must be one of them, which is what catches an offset resolved
	// against a body it was not decoded from.
	Elements []string
	// Offset is the decode offset just past the referencing element's start
	// tag, in the body the element was decoded from (DecodeAt).
	Offset int64
	// Guards are the conditions in force where the site was recorded, outermost
	// first. Record sets them from the guard stack.
	Guards []uir.ConditionStmt
}

type siteKey struct {
	kind, name string
	offset     int64
}

// RecorderOptions say what a Recorder keeps about each site.
type RecorderOptions struct {
	// Guards stamps each site with the conditions in force where it is recorded.
	Guards bool
}

// Recorder accumulates the references lowered from one body. Sites keep a
// decode offset rather than a line: the recorder is reachable from code that
// never sees the body, and an offset is only meaningful against the exact bytes
// that were decoded, which Relationships is handed.
//
// Every method of a nil Recorder is a no-op that records nothing, so a lowering
// not wired for references can share the code that records them.
type Recorder struct {
	opts  RecorderOptions
	sites []Site
	seen  map[siteKey]struct{}
	// guards is the stack of conditions in force where the lowering is now,
	// outermost first; only kept when opts.Guards.
	guards []uir.ConditionStmt
}

func NewRecorder(opts RecorderOptions) *Recorder {
	return &Recorder{opts: opts}
}

// Record appends one site, its name trimmed and stamped with the guards in
// force. A site that names nothing is not recorded. A walk may visit one
// element twice; its offset identifies it, so a second site of the same kind
// and name at the same offset is dropped rather than reported twice. A site
// without an offset is never dropped.
func (r *Recorder) Record(site Site) {
	site.Name = strings.TrimSpace(site.Name)
	if r == nil || site.Name == "" {
		return
	}
	if site.Offset > 0 {
		if r.seen == nil {
			r.seen = map[siteKey]struct{}{}
		}
		key := siteKey{kind: site.Kind, name: site.Name, offset: site.Offset}
		if _, dup := r.seen[key]; dup {
			return
		}
		r.seen[key] = struct{}{}
	}
	site.Guards = nil
	if r.Guarding() && len(r.guards) > 0 {
		site.Guards = slices.Clone(r.guards)
	}
	r.sites = append(r.sites, site)
}

// Sites are the sites recorded, in recording order.
func (r *Recorder) Sites() []Site {
	if r == nil {
		return nil
	}
	return r.sites
}

// Names returns the names recorded for one kind, in recording order.
func (r *Recorder) Names(kind string) []string {
	var out []string
	for _, site := range r.Sites() {
		if site.Kind == kind {
			out = append(out, site.Name)
		}
	}
	return out
}

// Guarding reports whether sites are stamped with their guards. A lowering
// skips building a condition only a guard would use when it is false.
func (r *Recorder) Guarding() bool {
	return r != nil && r.opts.Guards
}

func popNothing() {}

// Guard puts conditions in force, outermost first, for the sites recorded until
// the returned pop runs. Unless the recorder is Guarding it pushes nothing.
//
// Pops must run innermost first: a pop that runs while a later push is still in
// force, or after an earlier pop already removed its conditions, panics, since
// every site recorded after it would carry the wrong guards.
func (r *Recorder) Guard(conditions ...uir.ExprStmt) (pop func()) {
	if !r.Guarding() || len(conditions) == 0 {
		return popNothing
	}
	depth := len(r.guards)
	for _, condition := range conditions {
		r.guards = append(r.guards, uir.NewCondition(condition))
	}
	top := depth + len(conditions)
	return func() {
		if len(r.guards) != top {
			panic(fmt.Sprintf("lower: guard popped out of order: it pushed %d condition(s) onto a stack of %d, which now holds %d",
				len(conditions), depth, len(r.guards)))
		}
		r.guards = r.guards[:depth]
	}
}
