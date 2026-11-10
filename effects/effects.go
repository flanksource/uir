// Package effects names what a symbol does beyond computing from its inputs and the indexed source:
// reading the clock, a file, the network or a database, touching package-level state, starting
// goroutines. A symbol is deterministic when none of its effects, nor those of anything it reaches,
// breaks determinism, so the same source and inputs always behave the same. Unknown stands for what
// cannot be proved, such as a call through an interface or a function value, so determinism is never
// over-claimed. It imports only the standard library.
package effects

import (
	"slices"
	"strings"
)

// Effect is one kind of external effect.
type Effect string

const (
	TimeRead     Effect = "time.read"
	RandomRead   Effect = "random.read"
	EnvRead      Effect = "env.read"
	EnvWrite     Effect = "env.write"
	ProcessRead  Effect = "process.read"
	ProcessWrite Effect = "process.write"
	FileRead     Effect = "file.read"
	FileWrite    Effect = "file.write"
	NetworkIO    Effect = "network.io"
	DBRead       Effect = "db.read"
	DBWrite      Effect = "db.write"
	// GlobalRead reads a package-level variable that indexed code writes after its declaration.
	GlobalRead Effect = "global.read"
	// GlobalWrite writes a package-level variable, or through one: a field, an element, its address,
	// or a method that may mutate it.
	GlobalWrite Effect = "global.write"
	Goroutine   Effect = "goroutine"
	Channel     Effect = "channel"
	// LockRead takes or releases a shared lock, LockWrite an exclusive one. A lock does not break
	// determinism: the state it guards does, as a global, goroutine or channel effect.
	LockRead  Effect = "lock.read"
	LockWrite Effect = "lock.write"
	// MapOrder iterates a map, whose order Go randomizes. It does not break determinism, since most
	// iterations, such as collecting keys to sort, do not depend on the order.
	MapOrder Effect = "map.order"
	// Unknown is a call or a symbol whose effects the index cannot see.
	Unknown Effect = "unknown"
)

// All is every effect, sorted.
var All = []Effect{Channel, DBRead, DBWrite, EnvRead, EnvWrite, FileRead, FileWrite, GlobalRead, GlobalWrite,
	Goroutine, LockRead, LockWrite, MapOrder, NetworkIO, ProcessRead, ProcessWrite, RandomRead, TimeRead, Unknown}

// Valid reports whether effect is one of All.
func Valid(effect Effect) bool { return slices.Contains(All, effect) }

// BreaksDeterminism reports whether the effect makes a symbol's behavior depend on more than the
// indexed source and its inputs.
func (effect Effect) BreaksDeterminism() bool {
	switch effect {
	case LockRead, LockWrite, MapOrder:
		return false
	}
	return true
}

// Deterministic reports whether none of the effects breaks determinism.
func Deterministic(set []Effect) bool {
	return !slices.ContainsFunc(set, Effect.BreaksDeterminism)
}

// Lookup is the effects of a library symbol the catalog knows, by its package path, its owner type
// ("" for a package-level symbol) and its name, and whether the catalog knows it. An empty set is a
// symbol known to be deterministic. The most specific entry wins: the symbol itself, then the longest
// pattern ending in * that prefixes it.
func Lookup(packagePath, owner, name string) ([]Effect, bool) {
	key := packagePath + "." + name
	if owner != "" {
		key = packagePath + "." + owner + "." + name
	}
	if found, known := catalog[key]; known {
		return found, true
	}
	best := ""
	for pattern := range catalog {
		prefix, wildcard := strings.CutSuffix(pattern, "*")
		if wildcard && strings.HasPrefix(key, prefix) && len(pattern) > len(best) {
			best = pattern
		}
	}
	if best == "" {
		return nil, false
	}
	return catalog[best], true
}

// Builtin is the effects of calling or using a predeclared identifier: print and println write to
// standard error, and the error interface's Error method, owned by error, runs whatever implements it.
func Builtin(owner, name string) []Effect {
	switch {
	case owner == "error":
		return []Effect{Unknown}
	case name == "print" || name == "println":
		return []Effect{FileWrite}
	}
	return nil
}

// MutatesReceiver reports whether calling a pointer-receiver method of a library type may change the
// value it is called on. Locks and other synchronizers change only their own synchronization state,
// which their lock or goroutine effects already name, and a compiled regular expression, a location or
// a URL is read only through its pointer methods.
func MutatesReceiver(packagePath, typeName string) bool {
	switch packagePath + "." + typeName {
	case "sync.Mutex", "sync.RWMutex", "sync.Once", "sync.WaitGroup", "sync.Cond",
		"regexp.Regexp", "time.Location", "net/url.URL":
		return false
	}
	return true
}
