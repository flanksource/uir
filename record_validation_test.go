package uir

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
)

func conditionalFieldValidation() FieldValidation {
	return FieldValidation{
		Required: true,
		Conditions: []ConditionalFieldValidation{
			{
				Condition:  Expression{Expression: "state == 'active'", ExpressionType: ExpressionTypeCEL},
				Validation: FieldValidation{Min: lo.ToPtr(1.0)},
			},
			{
				Condition:  Expression{Expression: "state == 'closed'", ExpressionType: ExpressionTypeCEL},
				Validation: FieldValidation{NonEmpty: true},
			},
		},
	}
}

// Conditional validations used to be keyed by Expression — a struct, which encoding/json
// rejects as a map key. Any UIR carrying one failed to marshal outright, taking down the
// whole payload rather than just the condition.
func TestFieldValidationConditionsRoundTrip(t *testing.T) {
	data, err := json.Marshal(conditionalFieldValidation())
	if err != nil {
		t.Fatalf("failed to marshal field validation: %v", err)
	}

	var decoded FieldValidation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal field validation: %v", err)
	}

	if len(decoded.Conditions) != 2 {
		t.Fatalf("got %d conditions, want 2", len(decoded.Conditions))
	}
	if got := decoded.Conditions[0].Condition.Expression; got != "state == 'active'" {
		t.Errorf("condition 0 = %q, want the first-declared condition", got)
	}
	if decoded.Conditions[0].Validation.Min == nil || *decoded.Conditions[0].Validation.Min != 1 {
		t.Errorf("condition 0 validation min = %v, want 1", decoded.Conditions[0].Validation.Min)
	}
	if !decoded.Conditions[1].Validation.NonEmpty {
		t.Error("condition 1 validation lost nonEmpty")
	}
}

func TestRecordValidationConditionsRoundTrip(t *testing.T) {
	original := RecordValidation{
		MinRecords: lo.ToPtr(1),
		Conditions: []ConditionalRecordValidation{
			{
				Condition:  Expression{Expression: "size(rows) > 0", ExpressionType: ExpressionTypeCEL},
				Validation: RecordValidation{UniqueFields: []string{"id"}},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal record validation: %v", err)
	}

	var decoded RecordValidation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal record validation: %v", err)
	}

	if len(decoded.Conditions) != 1 {
		t.Fatalf("got %d conditions, want 1", len(decoded.Conditions))
	}
	if got := decoded.Conditions[0].Validation.UniqueFields; len(got) != 1 || got[0] != "id" {
		t.Errorf("nested record validation = %v, want [id]", got)
	}
}

// A UIR whose record fields carry conditional validation is the case that broke the
// --uir-cache round-trip: FieldValidation sits under both ASTRecord.Fields and
// MethodNode.Params, so one unmarshalable map killed the entire payload.
func TestUIRWithConditionalValidationRoundTrips(t *testing.T) {
	field := RecordField{Validation: conditionalFieldValidation()}
	field.Field = "Premium"
	record := ASTRecord{Fields: []RecordField{field}}
	record.Package = "policyadmin.rules"
	record.Type = "Premium"

	data, err := json.Marshal((&UIR{}).Add(record))
	if err != nil {
		t.Fatalf("failed to marshal UIR: %v", err)
	}

	var decoded UIR
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal UIR: %v", err)
	}
	if len(decoded.Records) != 1 {
		t.Fatalf("got %d records, want 1", len(decoded.Records))
	}
	if got := len(decoded.Records[0].Fields[0].Validation.Conditions); got != 2 {
		t.Errorf("got %d conditions after round-trip, want 2", got)
	}
}

// Conditions are an ordered list, so reordering them is a real change — the old
// map-keyed shape could not express that and had to sort to hash at all.
func TestFieldValidationHashIsOrderSensitive(t *testing.T) {
	forward := conditionalFieldValidation()
	reversed := conditionalFieldValidation()
	reversed.Conditions[0], reversed.Conditions[1] = reversed.Conditions[1], reversed.Conditions[0]

	if hashFieldValidation(forward) == hashFieldValidation(reversed) {
		t.Error("reordered conditions hash identically; condition order is not being hashed")
	}
	if hashFieldValidation(forward) != hashFieldValidation(conditionalFieldValidation()) {
		t.Error("identical validations hash differently")
	}
}
