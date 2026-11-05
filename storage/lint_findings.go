package storage

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LintClosure says what a lint finding's closure holds; see 25_lint_findings.hcl.
type LintClosure string

const (
	LintClosureSubject    LintClosure = "subject"
	LintClosureUnselected LintClosure = "unselected"
	LintClosureFile       LintClosure = "file"
	LintClosureSnapshot   LintClosure = "snapshot"
)

// LintSubject names what a lint finding is about: a symbol handle, or a file or scope key with handle 0.
type LintSubject struct {
	Handle int64
	Key    string
}

// LintFinding is what one rule found for one subject; see 25_lint_findings.hcl.
type LintFinding struct {
	RuleID             string      `gorm:"column:rule_id;primaryKey"`
	RuleVersion        string      `gorm:"column:rule_version"`
	SubjectHandle      int64       `gorm:"column:subject_handle;primaryKey;autoIncrement:false"`
	SubjectKey         string      `gorm:"column:subject_key;primaryKey"`
	ClosureSource      LintClosure `gorm:"column:closure_source"`
	ClosureFingerprint string      `gorm:"column:closure_fingerprint"`
	ContentGeneration  *int64      `gorm:"column:content_generation"`
	DocumentOrdinal    *int64      `gorm:"column:document_ordinal"`
	Findings           JSON        `gorm:"column:findings"`
	ProducedAt         time.Time   `gorm:"column:produced_at"`
	ExpiresAt          *time.Time  `gorm:"column:expires_at"`
}

func (LintFinding) TableName() string { return "lint_findings" }

// Subject is the subject the row is about.
func (finding LintFinding) Subject() LintSubject {
	return LintSubject{Handle: finding.SubjectHandle, Key: finding.SubjectKey}
}

// LintFindings loads the stored rows of rule for subjects, by subject; a subject without a row is absent.
// It reads IN batches, so its cost follows the subjects asked about.
func LintFindings(ctx context.Context, database *gorm.DB, ruleID string, subjects []LintSubject) (map[LintSubject]LintFinding, error) {
	var handles []int64
	var keys []string
	for _, subject := range subjects {
		if subject.Handle != 0 {
			handles = append(handles, subject.Handle)
		} else {
			keys = append(keys, subject.Key)
		}
	}
	found := make(map[LintSubject]LintFinding, len(subjects))
	load := func(condition string, values any) error {
		var rows []LintFinding
		if err := database.WithContext(ctx).Where("rule_id = ? AND "+condition, ruleID, values).Find(&rows).Error; err != nil {
			return fmt.Errorf("load lint findings of rule %s: %w", ruleID, err)
		}
		for _, row := range rows {
			found[row.Subject()] = row
		}
		return nil
	}
	for start := 0; start < len(handles); start += surrogateLookupBatch {
		if err := load("subject_handle IN ? AND subject_key = ''", handles[start:min(start+surrogateLookupBatch, len(handles))]); err != nil {
			return nil, err
		}
	}
	for start := 0; start < len(keys); start += surrogateLookupBatch {
		if err := load("subject_handle = 0 AND subject_key IN ?", keys[start:min(start+surrogateLookupBatch, len(keys))]); err != nil {
			return nil, err
		}
	}
	return found, nil
}

// SaveLintFindings writes rows in one transaction, each replacing the stored row of its rule and subject.
func SaveLintFindings(ctx context.Context, database *gorm.DB, rows []LintFinding) error {
	if len(rows) == 0 {
		return nil
	}
	return database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(rows, surrogateLookupBatch).Error; err != nil {
			return fmt.Errorf("save %d lint findings: %w", len(rows), err)
		}
		return nil
	})
}
