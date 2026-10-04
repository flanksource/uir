package storage

import (
	"context"
	"fmt"

	"github.com/flanksource/uir/storage/symbolhandle"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SymbolKind is one kind of the database's registry: a builtin kind (empty namespace, codes from 1) or
// a custom kind a non-Go producer registered (namespaced, codes from 63 downward). symbols.kind
// references Name.
type SymbolKind struct {
	Code      int16  `gorm:"column:code;primaryKey;autoIncrement:false"`
	Name      string `gorm:"column:name"`
	Category  string `gorm:"column:category"`
	Namespace string `gorm:"column:namespace"`
}

func (SymbolKind) TableName() string { return "symbol_kinds" }

func (kind SymbolKind) spec() (symbolhandle.KindSpec, error) {
	category, err := symbolhandle.ParseCategory(kind.Category)
	if err != nil {
		return symbolhandle.KindSpec{}, fmt.Errorf("symbol kind %q: %w", kind.Name, err)
	}
	return symbolhandle.KindSpec{Code: symbolhandle.Kind(kind.Code), Name: kind.Name, Category: category}, nil
}

// seedBuiltinKinds inserts the builtin kinds a database lacks and fails when a stored builtin code
// names another kind or category, which would make every handle of that kind ambiguous.
func seedBuiltinKinds(ctx context.Context, database *gorm.DB) error {
	builtins := symbolhandle.BuiltinKinds()
	rows := make([]SymbolKind, len(builtins))
	for i, spec := range builtins {
		rows[i] = SymbolKind{Code: int16(spec.Code), Name: spec.Name, Category: string(spec.Category)}
	}
	if err := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
		return fmt.Errorf("seed the builtin symbol kinds: %w", err)
	}
	var stored []SymbolKind
	if err := database.WithContext(ctx).Where("code <= ?", int16(symbolhandle.MaxBuiltinKind)).Order("code").Find(&stored).Error; err != nil {
		return fmt.Errorf("read the builtin symbol kinds: %w", err)
	}
	for i, row := range stored {
		if i >= len(rows) || row != rows[i] {
			return fmt.Errorf("symbol_kinds row %+v disagrees with the builtin kinds %+v", row, rows)
		}
	}
	if len(stored) != len(rows) {
		return fmt.Errorf("symbol_kinds holds %d of the %d builtin kinds", len(stored), len(rows))
	}
	return nil
}

// LoadSymbolKinds is the database's kind registry: the builtin kinds and every registered custom kind.
func LoadSymbolKinds(ctx context.Context, database *gorm.DB) (symbolhandle.Kinds, error) {
	var rows []SymbolKind
	if err := database.WithContext(ctx).Where("code > ?", int16(symbolhandle.MaxBuiltinKind)).Order("code").Find(&rows).Error; err != nil {
		return symbolhandle.Kinds{}, fmt.Errorf("load the custom symbol kinds: %w", err)
	}
	custom := make([]symbolhandle.KindSpec, len(rows))
	for i, row := range rows {
		spec, err := row.spec()
		if err != nil {
			return symbolhandle.Kinds{}, err
		}
		custom[i] = spec
	}
	return symbolhandle.NewKinds(custom)
}

// KindCode is the code of the kind named name, builtin or registered; an unregistered name is an error.
func KindCode(ctx context.Context, database *gorm.DB, name string) (symbolhandle.Kind, error) {
	kinds, err := LoadSymbolKinds(ctx, database)
	if err != nil {
		return 0, err
	}
	spec, err := kinds.Lookup(name)
	return spec.Code, err
}

// RegisterKind registers a namespaced custom kind and returns its code. Registering a name again with
// the same category returns its code; with another category it is an error. A new kind takes the code
// below the lowest custom code in use (MaxKind for the first), read inside the transaction, which runs
// again when a concurrent registration took the same code or name. A code that would reach the builtin
// codes is an error: the 6-bit kind field is full.
func RegisterKind(ctx context.Context, database *gorm.DB, name string, category symbolhandle.Category) (symbolhandle.Kind, error) {
	if err := symbolhandle.ValidateCustomKind(name, category); err != nil {
		return 0, err
	}
	var code symbolhandle.Kind
	err := RetryAllocationConflicts(ctx, database, func(tx *gorm.DB) error {
		var err error
		code, err = registerKind(ctx, tx, name, category)
		return err
	})
	return code, err
}

func registerKind(ctx context.Context, tx *gorm.DB, name string, category symbolhandle.Category) (symbolhandle.Kind, error) {
	var existing []SymbolKind
	if err := tx.WithContext(ctx).Where("name = ?", name).Find(&existing).Error; err != nil {
		return 0, fmt.Errorf("look up symbol kind %q: %w", name, err)
	}
	if len(existing) == 1 {
		if existing[0].Category != string(category) {
			return 0, fmt.Errorf("symbol kind %q is registered as %s, not %s", name, existing[0].Category, category)
		}
		return symbolhandle.Kind(existing[0].Code), nil
	}
	var lowest []int16
	if err := tx.WithContext(ctx).Model(&SymbolKind{}).Where("code > ?", int16(symbolhandle.MaxBuiltinKind)).Order("code").Limit(1).Pluck("code", &lowest).Error; err != nil {
		return 0, fmt.Errorf("read the lowest custom symbol kind code: %w", err)
	}
	next := int16(symbolhandle.MaxKind)
	if len(lowest) == 1 {
		next = lowest[0] - 1
	}
	if next <= int16(symbolhandle.MaxBuiltinKind) {
		return 0, fmt.Errorf("register symbol kind %q: no custom symbol kind code is free: the next would be %d, the highest builtin code", name, next)
	}
	row := SymbolKind{Code: next, Name: name, Category: string(category), Namespace: symbolhandle.KindNamespace(name)}
	if err := tx.WithContext(ctx).Create(&row).Error; err != nil {
		return 0, allocationConflict(err, fmt.Sprintf("register symbol kind %q with code %d", name, next))
	}
	return symbolhandle.Kind(next), nil
}
