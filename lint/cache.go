package lint

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/internal/canonicalhash"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"gorm.io/gorm"
)

const (
	// closureVersion versions the closure fingerprint encoding.
	closureVersion = "uir-lint-closure-v1"
	// ruleVersionEncoding versions the rule version encoding.
	ruleVersionEncoding = "uir-lint-rule-v1"
)

// errUnattributed marks an evaluation whose rows a subject or file closure cannot attribute to the
// subjects it evaluated, so the rule falls back to the snapshot closure.
var errUnattributed = errors.New("rows not attributable to evaluated subjects")

// finding is a stored finding: what a rule reported, located relative to its root, so the checkout it is
// shown in can differ from the one it was found in.
type finding struct {
	Root    string         `json:"root"`
	Path    string         `json:"path,omitempty"`
	Line    *int           `json:"line,omitempty"`
	Column  *int           `json:"column,omitempty"`
	Message string         `json:"message"`
	Symbol  *findingSymbol `json:"symbol,omitempty"`
}

// findingSymbol is the symbol owning a finding's location, for grouping by symbol.
type findingSymbol struct {
	ID             string `json:"id"`
	QueryName      string `json:"query_name"`
	DeclarationKey string `json:"declaration_key,omitempty"`
}

// produced is a finding an evaluation reported, with the snapshot and witness symbol of its row.
type produced struct {
	snapshot string
	witness  string
	finding  finding
}

// ruleRun lints one rule over one scope: it reuses the stored findings of every subject whose closure is
// unchanged, evaluates the rest, and stores what it evaluated.
type ruleRun struct {
	database *gorm.DB
	pipeline *query.Pipeline
	scope    *lintScope
	rule     Rule
	noCache  bool
	now      time.Time
	stats    RuleCache
	pairs    []string
	stored   *time.Time
}

// version digests what decides a rule's findings: its query, message template and declared reads.
func (rule Rule) ruleVersion() string {
	digest := canonicalhash.New(ruleVersionEncoding)
	digest.Text(rule.Query)
	digest.Text(rule.Message)
	digest.Text(rule.Reads)
	return digest.Sum()
}

func (run *ruleRun) header(source storage.LintClosure) canonicalhash.Hash {
	digest := canonicalhash.New(closureVersion)
	digest.Text(run.rule.version)
	digest.Text(indexer.IndexerVersion)
	digest.Text(strconv.Itoa(storage.DocumentFormatVersion))
	digest.Text(string(source))
	return digest
}

// lint returns the rule's findings over the scope, by its closure.
func (run *ruleRun) lint(ctx context.Context) ([]finding, error) {
	run.stats = RuleCache{ID: run.rule.ID, Closure: run.rule.requirements.Closure.Kind}
	var findings []finding
	var err error
	closure := run.rule.requirements.Closure.Kind
	if closure == query.SQLClosureSubject && run.rule.requirements.Mode == storage.AnalysisSyntax {
		// Syntax analysis gives declarations no canonical ids, so its subjects are the files declaring them.
		closure = query.SQLClosureFile
		run.stats.Closure = closure
	}
	switch closure {
	case query.SQLClosureSubject:
		findings, err = run.subjects(ctx)
	case query.SQLClosureFile:
		findings, err = run.files(ctx)
	case query.SQLClosureSnapshot:
		return run.snapshot(ctx)
	default:
		evaluated, err := run.evaluate(ctx, nil)
		run.stats.Evaluated++
		run.pairs = append(run.pairs, "live")
		return findingsOf(evaluated), err
	}
	if errors.Is(err, errUnattributed) {
		run.stats = RuleCache{ID: run.rule.ID, Closure: query.SQLClosureSnapshot}
		run.pairs, run.stored = nil, nil
		return run.snapshot(ctx)
	}
	slices.SortStableFunc(findings, func(a, b finding) int {
		return cmp.Or(cmp.Compare(a.Root, b.Root), cmp.Compare(a.Path, b.Path), cmp.Compare(position(a.Line), position(b.Line)),
			cmp.Compare(position(a.Column), position(b.Column)), cmp.Compare(a.Message, b.Message))
	})
	return findings, err
}

func position(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func findingsOf(evaluated []produced) []finding {
	findings := make([]finding, len(evaluated))
	for i, row := range evaluated {
		findings[i] = row.finding
	}
	return findings
}

// evaluate runs the rule's query over the scope, restricted to subjects when they are given.
func (run *ruleRun) evaluate(ctx context.Context, subjects *query.SQLSubjects) ([]produced, error) {
	var rows []produced
	_, err := run.pipeline.VisitSQLSubjects(ctx, run.rule.Query, run.scope.query, subjects, func(row query.SQLRow) error {
		var message strings.Builder
		if err := run.rule.message.Execute(&message, row.Values); err != nil {
			return err
		}
		result := produced{snapshot: row.Match.SnapshotID, finding: finding{Root: row.Match.RootKey, Path: row.Match.Path, Message: message.String()}}
		if row.Match.Line != nil {
			result.finding.Line = new(*row.Match.Line)
		}
		if row.Match.Column != nil {
			result.finding.Column = new(*row.Match.Column)
		}
		if row.WitnessSymbol != nil {
			result.witness = row.WitnessSymbol.ID
			result.finding.Symbol = &findingSymbol{ID: row.WitnessSymbol.ID, QueryName: row.WitnessSymbol.QueryName, DeclarationKey: row.WitnessSymbol.DeclarationKey}
		}
		rows = append(rows, result)
		return nil
	})
	return rows, err
}

// reusable reports whether a stored row still holds for its rule version and closure fingerprint.
func (run *ruleRun) reusable(row storage.LintFinding, found bool, source storage.LintClosure, fingerprint string) bool {
	return found && !run.noCache && row.RuleVersion == run.rule.version && row.ClosureSource == source &&
		row.ClosureFingerprint == fingerprint && (row.ExpiresAt == nil || row.ExpiresAt.After(run.now))
}

// reuse records a reused row and decodes its findings.
func (run *ruleRun) reuse(row storage.LintFinding) ([]finding, error) {
	var findings []finding
	if err := json.Unmarshal(row.Findings, &findings); err != nil {
		return nil, fmt.Errorf("decode the stored findings of rule %s: %w", run.rule.ID, err)
	}
	run.stats.Reused++
	run.pair(row)
	if run.stored == nil || row.ProducedAt.Before(*run.stored) {
		run.stored = new(row.ProducedAt)
	}
	return findings, nil
}

// row builds a row for an evaluated subject.
func (run *ruleRun) row(subject storage.LintSubject, source storage.LintClosure, fingerprint string, findings []finding) (storage.LintFinding, error) {
	if findings == nil {
		findings = []finding{}
	}
	encoded, err := json.Marshal(findings)
	if err != nil {
		return storage.LintFinding{}, err
	}
	row := storage.LintFinding{RuleID: run.rule.ID, RuleVersion: run.rule.version, SubjectHandle: subject.Handle, SubjectKey: subject.Key,
		ClosureSource: source, ClosureFingerprint: fingerprint, Findings: storage.JSON(encoded), ProducedAt: run.now}
	run.stats.Evaluated++
	run.pair(row)
	return row, nil
}

func (run *ruleRun) pair(row storage.LintFinding) {
	run.pairs = append(run.pairs, fmt.Sprintf("%s\x00%d\x00%s\x00%s", run.rule.version, row.SubjectHandle, row.SubjectKey, row.ClosureFingerprint))
}

// snapshot lints the rule as one pair over the whole scope: its closure is every snapshot of the scope,
// by content set, context, configuration and dependency set, and their ids when the rule reads facts of
// the snapshots themselves.
func (run *ruleRun) snapshot(ctx context.Context) ([]finding, error) {
	roots := canonicalhash.New(closureVersion)
	for _, id := range run.scope.rootIDs() {
		roots.Text(id.String())
	}
	subject := storage.LintSubject{Key: "scope:" + roots.Sum()}
	digest := run.header(storage.LintClosureSnapshot)
	snapshots := slices.Clone(run.scope.snapshots)
	slices.SortFunc(snapshots, func(a, b *scopeSnapshot) int {
		return cmp.Compare(a.snapshot.RootID.String(), b.snapshot.RootID.String())
	})
	digest.Count(len(snapshots))
	for _, snapshot := range snapshots {
		dependencies := ""
		if snapshot.snapshot.DependencySetHash != nil {
			dependencies = *snapshot.snapshot.DependencySetHash
		}
		for _, text := range []string{snapshot.snapshot.RootID.String(), string(snapshot.snapshot.AnalysisMode), snapshot.snapshot.ContentSetHash,
			snapshot.snapshot.ContextHash, snapshot.snapshot.ConfigurationHash, dependencies} {
			digest.Text(text)
		}
		if run.rule.requirements.Closure.Snapshots {
			digest.Text(snapshot.snapshot.ID.String())
		}
	}
	fingerprint := digest.Sum()
	stored, err := storage.LintFindings(ctx, run.database, run.rule.ID, []storage.LintSubject{subject})
	if err != nil {
		return nil, err
	}
	if row, found := stored[subject]; run.reusable(row, found, storage.LintClosureSnapshot, fingerprint) {
		return run.reuse(row)
	}
	evaluated, err := run.evaluate(ctx, nil)
	if err != nil {
		return nil, err
	}
	findings := findingsOf(evaluated)
	row, err := run.row(subject, storage.LintClosureSnapshot, fingerprint, findings)
	if err != nil {
		return nil, err
	}
	return findings, storage.SaveLintFindings(ctx, run.database, []storage.LintFinding{row})
}

// files lints a files() rule, or a subject rule over syntax analysis, per file: a file's closure is its
// document, which holds everything such a row reads (its source revision, coverage and declarations).
func (run *ruleRun) files(ctx context.Context) ([]finding, error) {
	type file struct {
		snapshot    *scopeSnapshot
		path        string
		fingerprint string
	}
	byKey := map[string]file{}
	var subjects []storage.LintSubject
	for _, snapshot := range run.scope.snapshots {
		documents, err := snapshot.activeDocuments(ctx, run.database)
		if err != nil {
			return nil, err
		}
		for _, path := range slices.Sorted(maps.Keys(documents)) {
			digest := run.header(storage.LintClosureFile)
			digest.Text(snapshot.snapshot.RootID.String())
			digest.Text(path)
			digest.Text(strconv.FormatInt(documents[path], 10))
			key := snapshot.snapshot.RootID.String() + ":" + path
			if _, duplicate := byKey[key]; duplicate {
				return nil, errUnattributed
			}
			byKey[key] = file{snapshot: snapshot, path: path, fingerprint: digest.Sum()}
			subjects = append(subjects, storage.LintSubject{Key: key})
		}
	}
	stored, err := storage.LintFindings(ctx, run.database, run.rule.ID, subjects)
	if err != nil {
		return nil, err
	}
	var findings []finding
	stale := map[string]bool{}
	for _, subject := range subjects {
		current := byKey[subject.Key]
		if row, found := stored[subject]; run.reusable(row, found, storage.LintClosureFile, current.fingerprint) {
			reused, err := run.reuse(row)
			if err != nil {
				return nil, err
			}
			findings = append(findings, reused...)
			continue
		}
		stale[current.snapshot.snapshot.ID.String()+"\x00"+current.path] = true
	}
	if len(stale) == 0 {
		return findings, nil
	}
	evaluated, err := run.evaluate(ctx, &query.SQLSubjects{Files: stale})
	if err != nil {
		return nil, err
	}
	byFile := map[string][]finding{}
	for _, result := range evaluated {
		key := result.snapshot + "\x00" + result.finding.Path
		if result.finding.Path == "" || !stale[key] {
			return nil, errUnattributed
		}
		byFile[key] = append(byFile[key], result.finding)
	}
	var rows []storage.LintFinding
	for _, subject := range subjects {
		current := byKey[subject.Key]
		key := current.snapshot.snapshot.ID.String() + "\x00" + current.path
		if !stale[key] {
			continue
		}
		row, err := run.row(subject, storage.LintClosureFile, current.fingerprint, byFile[key])
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
		findings = append(findings, byFile[key]...)
	}
	return findings, storage.SaveLintFindings(ctx, run.database, rows)
}

// symbolLayer is the fingerprint a symbol subject contributes: every layer of a callable, whose
// producers disagree on which layer holds the signature, and otherwise the public layer, or the private
// layer for a rule declaring reads: private.
func (run *ruleRun) symbolLayer(digest canonicalhash.Hash, symbol storage.EffectiveSymbol, callable bool) {
	text := func(value *int64) string {
		if value == nil {
			return ""
		}
		return strconv.FormatInt(*value, 10)
	}
	switch {
	case callable:
		digest.Text("all")
		digest.Text(text(symbol.PublicFP))
		digest.Text(text(symbol.PrivateFP))
		digest.Text(text(symbol.ContentFP))
	case run.rule.Reads == ReadsPrivate:
		digest.Text(ReadsPrivate)
		digest.Text(text(symbol.PrivateFP))
	default:
		digest.Text(ReadsPublic)
		digest.Text(text(symbol.PublicFP))
	}
}

// selectorKinds are the handle kinds the rule's selector selects, by code.
func (run *ruleRun) selectorKinds(ctx context.Context) (map[symbolhandle.Kind]bool, error) {
	codes, err := run.pipeline.SelectorKinds(ctx, run.rule.requirements.Closure.Selector)
	if err != nil {
		return nil, err
	}
	kinds := map[symbolhandle.Kind]bool{}
	for _, code := range codes {
		kinds[code] = true
	}
	return kinds, nil
}
