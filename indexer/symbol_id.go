package indexer

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/flanksource/uir/storage"
)

const (
	symbolIdentityDomain  = "uir-symbol"
	symbolIdentityVersion = 1
)

// Identity is the input to a canonical symbol id; see docs/symbols.md#canonical-symbols. Go extraction
// derives it from type-checker objects; a non-Go producer builds one for every symbol it publishes and
// computes the ids its documents carry with SymbolID. ModuleKey is the root key of the module that
// declares the symbol, OwnerID the id of the type, record, or screen that owns a member or method, and
// ParameterTypes the ordered parameter types that distinguish overloads.
type Identity struct {
	ModuleKey      string   `json:"module_key"`
	PackagePath    string   `json:"package_path"`
	Kind           string   `json:"kind"`
	OwnerID        string   `json:"owner_id,omitempty"`
	Name           string   `json:"name"`
	ParameterTypes []string `json:"parameter_types,omitempty"`
}

// CanonicalKey is the versioned, length-delimited encoding the id digests: every field is written as
// <byte length>:<bytes>, so no two field sequences share an encoding.
func (identity Identity) CanonicalKey() string {
	var key strings.Builder
	field := func(value string) {
		key.WriteString(strconv.Itoa(len(value)))
		key.WriteByte(':')
		key.WriteString(value)
		key.WriteByte(',')
	}
	field(symbolIdentityDomain)
	field(strconv.Itoa(symbolIdentityVersion))
	field(identity.ModuleKey)
	field(identity.PackagePath)
	field(identity.Kind)
	field(identity.OwnerID)
	field(identity.Name)
	field(strconv.Itoa(len(identity.ParameterTypes)))
	for _, parameter := range identity.ParameterTypes {
		field(parameter)
	}
	return key.String()
}

// SymbolID is the canonical symbol id of identity: the hex SHA-256 of its canonical key.
func SymbolID(identity Identity) string { return hashBytes([]byte(identity.CanonicalKey())) }

// symbolRow is the symbols row of an identity with its visibility, before a handle is assigned.
func symbolRow(identity Identity, visibility string) (storage.Symbol, error) {
	parameters := identity.ParameterTypes
	if parameters == nil {
		parameters = []string{}
	}
	encoded, err := json.Marshal(parameters)
	if err != nil {
		return storage.Symbol{}, fmt.Errorf("encode parameter types of %s: %w", identity.Name, err)
	}
	row := storage.Symbol{
		ID: SymbolID(identity), IdentityVersion: symbolIdentityVersion, CanonicalKey: identity.CanonicalKey(), ModuleKey: identity.ModuleKey,
		PackagePath: identity.PackagePath, Kind: identity.Kind, Name: identity.Name,
		SearchName: storage.SearchName(identity.Name), Visibility: visibility, ParameterTypes: storage.JSON(encoded),
	}
	if identity.OwnerID != "" {
		owner := identity.OwnerID
		row.OwnerID = &owner
	}
	return row, nil
}
