package uir

import (
	"database/sql/driver"
	"fmt"
	"strings"
)

type UnitType string

const (
	UnitTypeMetric       UnitType = "metric"
	UnitTypeImperial     UnitType = "imperial"
	UnitTypeSI           UnitType = "si"
	UnitTypePercentage   UnitType = "percentage"
	UnitTypeBytes        UnitType = "bytes"
	UnitTypeSeconds      UnitType = "seconds"
	UnitTypeMilliseconds UnitType = "milliseconds"
	UnitTypeNanoseconds  UnitType = "nanoseconds"
	UnitTypeEpoch        UnitType = "epoch"
	UnitTypeCurrency     UnitType = "currency"
)

type Unit struct {
	Type UnitType `json:"type,omitempty"`
	// Optional modifier for e.g. Currency Code for currency, Timezone for dates, etc..
	Val *string `json:"value,omitempty"`
}

func (u Unit) String() string {
	if u.Val != nil {
		return string(u.Type) + " (" + *u.Val + ")"
	}
	return string(u.Type)
}

// Value implements driver.Valuer interface for database serialization.
// Serializes Unit to "type:value" format for storage in database.
// Returns NULL for nil pointers, "type:" for units without value, "type:value" for units with value.
func (u Unit) Value() (driver.Value, error) {
	if u.Type == "" {
		return nil, fmt.Errorf("cannot serialize Unit with empty Type")
	}

	value := ""
	if u.Val != nil {
		value = *u.Val
	}

	return string(u.Type) + ":" + value, nil
}

// Scan implements sql.Scanner interface for database deserialization.
// Parses "type:value" format from database into Unit struct.
// Handles NULL as zero value (empty Type, nil Val).
func (u *Unit) Scan(value interface{}) error {
	if value == nil {
		u.Type = ""
		u.Val = nil
		return nil
	}

	var str string
	switch v := value.(type) {
	case string:
		str = v
	case []byte:
		str = string(v)
	default:
		return fmt.Errorf("failed to scan Unit: expected string or []byte, got %T", value)
	}

	parts := strings.SplitN(str, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("failed to scan Unit: invalid format '%s', expected 'type:value'", str)
	}

	u.Type = UnitType(parts[0])
	if parts[1] != "" {
		u.Val = &parts[1]
	} else {
		u.Val = nil
	}

	return nil
}
