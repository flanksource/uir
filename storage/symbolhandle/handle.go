// Package symbolhandle is the H64a layout of a symbol handle: a database-local 64-bit surrogate for a
// canonical SHA-256 symbol id. Bits, most significant first:
//
//	0 | module 11 | package 16 | visibility 1 | kind 3 | local 32
//
// The sign bit is always zero, so a handle is a non-negative int64 that both engines store as bigint,
// and handles sort by module, package, visibility, kind, then local. Every prefix is a contiguous
// range, so "every exported func of a package" is one B-tree range scan over symbols.handle or the
// posting index. Module and package numbers come from the symbol_modules and symbol_packages
// registries; local numbers are dense per (module, package, visibility, kind) bucket.
package symbolhandle

import (
	"fmt"
)

const (
	ModuleBits  = 11
	PackageBits = 16
	LocalBits   = 32

	MaxModule  uint64 = 1<<ModuleBits - 1
	MaxPackage uint64 = 1<<PackageBits - 1
	MaxLocal   uint64 = 1<<LocalBits - 1

	moduleShift     = 52
	packageShift    = 36
	visibilityShift = 35
	kindShift       = 32
)

// Module numbers 0 and 1 are reserved: builtins have an empty module key, and the standard library is
// "std". Every other module key gets the next number from FirstAllocatedModule.
const (
	BuiltinModule        uint64 = 0
	StdModule            uint64 = 1
	FirstAllocatedModule uint64 = 2
)

// ReservedModuleNumber is the fixed number of a reserved module key.
func ReservedModuleNumber(moduleKey string) (uint64, bool) {
	switch moduleKey {
	case "":
		return BuiltinModule, true
	case "std":
		return StdModule, true
	}
	return 0, false
}

// Fields are the unpacked parts of a handle. They are uint64 so an allocator's arithmetic can overflow
// a field and Pack reports which one.
type Fields struct {
	Module     uint64
	Package    uint64
	Visibility Visibility
	Kind       Kind
	Local      uint64
}

// Range is an inclusive handle range.
type Range struct{ Low, High int64 }

func (r Range) Contains(handle int64) bool { return handle >= r.Low && handle <= r.High }

// Pack builds the handle of fields; a field wider than its bit budget is an error naming it.
func Pack(fields Fields) (int64, error) {
	for _, field := range []struct {
		name  string
		value uint64
		bits  uint
	}{
		{"module", fields.Module, ModuleBits}, {"package", fields.Package, PackageBits},
		{"visibility", uint64(fields.Visibility), 1}, {"kind", uint64(fields.Kind), 3}, {"local", fields.Local, LocalBits},
	} {
		if field.value > 1<<field.bits-1 {
			unit := "bits"
			if field.bits == 1 {
				unit = "bit"
			}
			return 0, fmt.Errorf("symbol handle %s %d exceeds %d %s", field.name, field.value, field.bits, unit)
		}
	}
	return int64(fields.Module<<moduleShift | fields.Package<<packageShift | uint64(fields.Visibility)<<visibilityShift |
		uint64(fields.Kind)<<kindShift | fields.Local), nil
}

// Unpack splits a handle into its fields; a negative handle was not produced by Pack.
func Unpack(handle int64) (Fields, error) {
	if handle < 0 {
		return Fields{}, fmt.Errorf("symbol handle %d has the sign bit set", handle)
	}
	value := uint64(handle)
	return Fields{
		Module: value >> moduleShift & MaxModule, Package: value >> packageShift & MaxPackage,
		Visibility: Visibility(value >> visibilityShift & 1), Kind: Kind(value >> kindShift & 7), Local: value & MaxLocal,
	}, nil
}

// PackageRange is every handle of one package.
func PackageRange(module, pkg uint64) (Range, error) {
	low, err := Pack(Fields{Module: module, Package: pkg})
	return Range{Low: low, High: low | int64(1<<packageShift-1)}, err
}

// VisibilityRange is every handle of one package with one visibility.
func VisibilityRange(module, pkg uint64, visibility Visibility) (Range, error) {
	low, err := Pack(Fields{Module: module, Package: pkg, Visibility: visibility})
	return Range{Low: low, High: low | int64(1<<visibilityShift-1)}, err
}

// BucketRange is every handle of one (module, package, visibility, kind) bucket; its locals are
// handle - Low.
func BucketRange(module, pkg uint64, visibility Visibility, kind Kind) (Range, error) {
	low, err := Pack(Fields{Module: module, Package: pkg, Visibility: visibility, Kind: kind})
	return Range{Low: low, High: low | int64(MaxLocal)}, err
}
