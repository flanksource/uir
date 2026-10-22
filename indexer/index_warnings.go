package indexer

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/flanksource/gavel/models"
	"github.com/flanksource/uir/storage"
)

// The rules of the warnings an index reports, in the shape lint reports findings.
const (
	// RuleIndex is a file whose document failed and was published degraded.
	RuleIndex = "uir/index"
	// RuleCoverage is a load or type error of a package that is not fully indexed.
	RuleCoverage = "uir/coverage"
)

// CoverageViolations are the warnings of one package_coverage.diagnostics value, each path joined to
// location when location is not empty.
func CoverageViolations(location string, diagnostics json.RawMessage) ([]models.Violation, error) {
	var decoded []packageDiagnostic
	if err := json.Unmarshal(diagnostics, &decoded); err != nil {
		return nil, fmt.Errorf("decode package diagnostics: %w", err)
	}
	return diagnosticViolations(location, decoded), nil
}

// snapshotWarnings are the warnings of a snapshot's diagnostics, the per-package summary
// snapshotDiagnostics encodes, with root-relative paths.
func snapshotWarnings(diagnostics storage.JSON) ([]models.Violation, error) {
	var summaries []struct {
		Diagnostics []packageDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(diagnostics, &summaries); err != nil {
		return nil, fmt.Errorf("decode snapshot diagnostics: %w", err)
	}
	var warnings []models.Violation
	for _, summary := range summaries {
		warnings = append(warnings, diagnosticViolations("", summary.Diagnostics)...)
	}
	return warnings, nil
}

// diagnosticViolations is the one conversion of persisted package diagnostics into warnings: a
// diagnostic without a rule is a load or type error, uir/coverage.
func diagnosticViolations(location string, diagnostics []packageDiagnostic) []models.Violation {
	violations := make([]models.Violation, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		rule := diagnostic.Rule
		if rule == "" {
			rule = RuleCoverage
		}
		message := diagnostic.Message
		violation := models.Violation{
			File: diagnostic.Path, Message: &message, Source: "uir", Severity: models.SeverityWarning,
			Rule: &models.Rule{Type: models.RuleTypeDeny, Package: "uir", Method: rule},
		}
		if location != "" && diagnostic.Path != "" {
			violation.File = filepath.Join(location, diagnostic.Path)
		}
		if diagnostic.Range != nil {
			violation.Line, violation.Column = diagnostic.Range[0], diagnostic.Range[1]
		}
		violations = append(violations, violation)
	}
	return violations
}
