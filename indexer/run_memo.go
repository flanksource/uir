package indexer

import (
	"context"
	"errors"
	"sync"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
)

// runMemo is what one index run has already resolved, so the run does each piece of work once however
// many of its modules need it: the snapshot each module location was published or confirmed as, the
// snapshot each versioned dependency resolved to, each versioned snapshot's closure node, and the
// commit each version tag names. A module run starts one for all its targets; an IndexModules or
// IndexRevision called outside a run starts one for its own duration.
//
// Local entries are facts about sources the run observed. A later publication still verifies every
// local dependency it links against disk and its head inside its transaction, and an index retried
// because its inputs changed forgets every entry, so a dependency whose inputs changed is indexed again.
type runMemo struct {
	mu       sync.Mutex
	modules  map[moduleMemoKey]ModuleResult
	versions map[versionMemoKey]versionOutcome
	closure  map[uuid.UUID]closureNode
	tags     map[tagMemoKey]tagCommit
	// versionCommit resolves a version to a commit of a checkout; tests count calls through it.
	versionCommit func(ctx context.Context, checkout, version string) (string, error)
}

type moduleMemoKey struct {
	location     string
	includeTests bool
}

type versionMemoKey struct {
	modulePath, version string
	includeTests        bool
}

type tagMemoKey struct{ modulePath, version string }

// versionOutcome is a resolved versioned dependency: its snapshot, or why no snapshot is available.
type versionOutcome struct {
	snapshot    storage.ModuleSnapshot
	unavailable error
}

// closureNode is one snapshot of a versioned closure: whether the snapshot itself is complete, and
// the snapshots its dependency edges target.
type closureNode struct {
	complete bool
	targets  []uuid.UUID
}

// tagCommit is the commit a version names in the first registered checkout that has it.
type tagCommit struct {
	commit string
	found  bool
}

type runMemoKey struct{}

func newRunMemo() *runMemo {
	memo := &runMemo{versionCommit: versionCommit}
	memo.forget()
	return memo
}

func withRunMemo(ctx context.Context, memo *runMemo) context.Context {
	return context.WithValue(ctx, runMemoKey{}, memo)
}

// ensureRunMemo is the memo of the run ctx belongs to, or, outside a run, a new memo and the context
// that carries it.
func ensureRunMemo(ctx context.Context) (context.Context, *runMemo) {
	if memo, found := ctx.Value(runMemoKey{}).(*runMemo); found {
		return ctx, memo
	}
	memo := newRunMemo()
	return withRunMemo(ctx, memo), memo
}

// forget drops every entry, for an index that runs again because its inputs changed.
func (memo *runMemo) forget() {
	memo.mu.Lock()
	defer memo.mu.Unlock()
	memo.modules = map[moduleMemoKey]ModuleResult{}
	memo.versions = map[versionMemoKey]versionOutcome{}
	memo.closure = map[uuid.UUID]closureNode{}
	memo.tags = map[tagMemoKey]tagCommit{}
}

func (memo *runMemo) module(location string, includeTests bool) (ModuleResult, bool) {
	memo.mu.Lock()
	defer memo.mu.Unlock()
	result, found := memo.modules[moduleMemoKey{location: location, includeTests: includeTests}]
	return result, found
}

func (memo *runMemo) rememberModules(results []ModuleResult, includeTests bool) {
	memo.mu.Lock()
	defer memo.mu.Unlock()
	for _, result := range results {
		memo.modules[moduleMemoKey{location: result.Location, includeTests: includeTests}] = result
	}
}

// version is the remembered outcome of resolving modulePath at version, as resolve returns it, and
// resolves and remembers it first when the run has not: a snapshot, or an errVersionUnavailable.
// Any other error is returned without being remembered.
func (memo *runMemo) version(key versionMemoKey, resolve func() (storage.ModuleSnapshot, error)) (storage.ModuleSnapshot, error) {
	memo.mu.Lock()
	outcome, found := memo.versions[key]
	memo.mu.Unlock()
	if found {
		return outcome.snapshot, outcome.unavailable
	}
	snapshot, err := resolve()
	if err != nil && !errors.Is(err, errVersionUnavailable) {
		return storage.ModuleSnapshot{}, err
	}
	memo.mu.Lock()
	memo.versions[key] = versionOutcome{snapshot: snapshot, unavailable: err}
	memo.mu.Unlock()
	return snapshot, err
}

func (memo *runMemo) closureNode(id uuid.UUID, load func() (closureNode, error)) (closureNode, error) {
	memo.mu.Lock()
	node, found := memo.closure[id]
	memo.mu.Unlock()
	if found {
		return node, nil
	}
	node, err := load()
	if err != nil {
		return closureNode{}, err
	}
	memo.mu.Lock()
	memo.closure[id] = node
	memo.mu.Unlock()
	return node, nil
}

func (memo *runMemo) tagCommit(key tagMemoKey, resolve func() (tagCommit, error)) (tagCommit, error) {
	memo.mu.Lock()
	tag, found := memo.tags[key]
	memo.mu.Unlock()
	if found {
		return tag, nil
	}
	tag, err := resolve()
	if err != nil {
		return tagCommit{}, err
	}
	memo.mu.Lock()
	memo.tags[key] = tag
	memo.mu.Unlock()
	return tag, nil
}
