package uir

// SQLCatalog records the inspection contract. A missing or older version cannot
// establish that absent database members are safe to remove.
type SQLCatalog struct {
	Version    int            `json:"version"`
	Dialect    string         `json:"dialect"`
	Schemas    []string       `json:"schemas"`
	Complete   bool           `json:"complete"`
	TableTypes []SQLTableType `json:"tableTypes,omitempty"`
	Sequences  []SQLSequence  `json:"sequences,omitempty"`
}

// SQLTableType is a user-defined table type (CREATE TYPE ... AS TABLE). The
// server names its defaults and keys itself, so SQLDefault.Name and the key
// names stay empty.
type SQLTableType struct {
	Schema  string         `json:"schema"`
	Name    string         `json:"name"`
	Columns []RecordColumn `json:"columns"`
	Keys    []RecordIndex  `json:"keys,omitempty"`
	Checks  []string       `json:"checks,omitempty"`
}

// SQLSequence is a CREATE SEQUENCE object. Bounds are exact decimal strings;
// CacheSize 0 without NoCache is the server's default cache.
type SQLSequence struct {
	Schema    string `json:"schema"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Start     string `json:"start"`
	Increment string `json:"increment"`
	Minimum   string `json:"minimum"`
	Maximum   string `json:"maximum"`
	Cycle     bool   `json:"cycle,omitempty"`
	NoCache   bool   `json:"noCache,omitempty"`
	CacheSize int64  `json:"cacheSize,omitempty"`
}

type SQLIdentity struct {
	Seed      int64 `json:"seed"`
	Increment int64 `json:"increment"`
}

type SQLDefault struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
	// SystemNamed marks a constraint the server named itself; its Name differs
	// in every database.
	SystemNamed bool `json:"systemNamed,omitempty"`
}

// SQLRuntimeDependency is a module reference the catalogue could not bind to a
// local object (missing, cross-database, linked-server or built-in); it
// resolves only when the module runs.
type SQLRuntimeDependency struct {
	Server   string `json:"server,omitempty"`
	Database string `json:"database,omitempty"`
	Schema   string `json:"schema,omitempty"`
	Name     string `json:"name"`
}

type SQLCheck struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
}

type SQLDependency struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
}

const TriggerDisabledProperty = "isDisabled"
