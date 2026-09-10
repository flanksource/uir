package uir

import (
	"database/sql/driver"
	"encoding/json"
	"testing"
)

func TestUnit_Value(t *testing.T) {
	tests := []struct {
		name    string
		unit    Unit
		want    driver.Value
		wantErr bool
	}{
		{
			name: "unit with value",
			unit: Unit{
				Type: UnitTypeCurrency,
				Val:  stringPtr("USD"),
			},
			want:    "currency:USD",
			wantErr: false,
		},
		{
			name: "unit without value",
			unit: Unit{
				Type: UnitTypePercentage,
				Val:  nil,
			},
			want:    "percentage:",
			wantErr: false,
		},
		{
			name: "unit with empty value string",
			unit: Unit{
				Type: UnitTypeBytes,
				Val:  stringPtr(""),
			},
			want:    "bytes:",
			wantErr: false,
		},
		{
			name: "unit with value containing colon",
			unit: Unit{
				Type: UnitTypeCurrency,
				Val:  stringPtr("USD:EUR"),
			},
			want:    "currency:USD:EUR",
			wantErr: false,
		},
		{
			name: "empty type returns error",
			unit: Unit{
				Type: "",
				Val:  stringPtr("test"),
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.unit.Value()
			if (err != nil) != tt.wantErr {
				t.Errorf("Unit.Value() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Unit.Value() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnit_Scan(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    Unit
		wantErr bool
	}{
		{
			name:  "valid format with value",
			input: "currency:USD",
			want: Unit{
				Type: UnitTypeCurrency,
				Val:  stringPtr("USD"),
			},
			wantErr: false,
		},
		{
			name:  "valid format without value",
			input: "percentage:",
			want: Unit{
				Type: UnitTypePercentage,
				Val:  nil,
			},
			wantErr: false,
		},
		{
			name:  "valid format with empty string value",
			input: "bytes:",
			want: Unit{
				Type: UnitTypeBytes,
				Val:  nil,
			},
			wantErr: false,
		},
		{
			name:  "valid format with colon in value",
			input: "currency:USD:EUR",
			want: Unit{
				Type: UnitTypeCurrency,
				Val:  stringPtr("USD:EUR"),
			},
			wantErr: false,
		},
		{
			name:  "byte slice input",
			input: []byte("seconds:3600"),
			want: Unit{
				Type: UnitTypeSeconds,
				Val:  stringPtr("3600"),
			},
			wantErr: false,
		},
		{
			name:  "null input",
			input: nil,
			want: Unit{
				Type: "",
				Val:  nil,
			},
			wantErr: false,
		},
		{
			name:    "invalid format - no colon",
			input:   "currency",
			want:    Unit{},
			wantErr: true,
		},
		{
			name:    "invalid type - int",
			input:   123,
			want:    Unit{},
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			want:    Unit{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var u Unit
			err := u.Scan(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unit.Scan() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			if u.Type != tt.want.Type {
				t.Errorf("Unit.Scan() Type = %v, want %v", u.Type, tt.want.Type)
			}

			if (u.Val == nil) != (tt.want.Val == nil) {
				t.Errorf("Unit.Scan() Val nil mismatch: got %v, want %v", u.Val == nil, tt.want.Val == nil)
				return
			}

			if u.Val != nil && tt.want.Val != nil && *u.Val != *tt.want.Val {
				t.Errorf("Unit.Scan() Val = %v, want %v", *u.Val, *tt.want.Val)
			}
		})
	}
}

func TestUnit_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		unit Unit
	}{
		{
			name: "currency with value",
			unit: Unit{Type: UnitTypeCurrency, Val: stringPtr("USD")},
		},
		{
			name: "percentage without value",
			unit: Unit{Type: UnitTypePercentage, Val: nil},
		},
		{
			name: "bytes with numeric value",
			unit: Unit{Type: UnitTypeBytes, Val: stringPtr("1024")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := tt.unit.Value()
			if err != nil {
				t.Fatalf("Value() failed: %v", err)
			}

			var scanned Unit
			err = scanned.Scan(value)
			if err != nil {
				t.Fatalf("Scan() failed: %v", err)
			}

			if scanned.Type != tt.unit.Type {
				t.Errorf("Round-trip Type mismatch: got %v, want %v", scanned.Type, tt.unit.Type)
			}

			if (scanned.Val == nil) != (tt.unit.Val == nil) {
				t.Errorf("Round-trip Val nil mismatch")
				return
			}

			if scanned.Val != nil && tt.unit.Val != nil && *scanned.Val != *tt.unit.Val {
				t.Errorf("Round-trip Val mismatch: got %v, want %v", *scanned.Val, *tt.unit.Val)
			}
		})
	}
}

func TestUnit_JSONCompatibility(t *testing.T) {
	tests := []struct {
		name string
		unit Unit
		json string
	}{
		{
			name: "currency with value",
			unit: Unit{Type: UnitTypeCurrency, Val: stringPtr("USD")},
			json: `{"type":"currency","value":"USD"}`,
		},
		{
			name: "percentage without value",
			unit: Unit{Type: UnitTypePercentage},
			json: `{"type":"percentage"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" marshal", func(t *testing.T) {
			data, err := json.Marshal(tt.unit)
			if err != nil {
				t.Fatalf("json.Marshal() failed: %v", err)
			}

			var unmarshaled Unit
			err = json.Unmarshal(data, &unmarshaled)
			if err != nil {
				t.Fatalf("json.Unmarshal() failed: %v", err)
			}

			if unmarshaled.Type != tt.unit.Type {
				t.Errorf("JSON round-trip Type mismatch: got %v, want %v", unmarshaled.Type, tt.unit.Type)
			}

			if (unmarshaled.Val == nil) != (tt.unit.Val == nil) {
				t.Errorf("JSON round-trip Val nil mismatch")
				return
			}

			if unmarshaled.Val != nil && tt.unit.Val != nil && *unmarshaled.Val != *tt.unit.Val {
				t.Errorf("JSON round-trip Val mismatch: got %v, want %v", *unmarshaled.Val, *tt.unit.Val)
			}
		})
	}
}

func stringPtr(s string) *string {
	return &s
}
