package uir

// SQLCatalog records the inspection contract. A missing or older version cannot
// establish that absent database members are safe to remove.
type SQLCatalog struct {
	Version  int      `json:"version"`
	Dialect  string   `json:"dialect"`
	Schemas  []string `json:"schemas"`
	Complete bool     `json:"complete"`
}

type SQLIdentity struct {
	Seed      int64 `json:"seed"`
	Increment int64 `json:"increment"`
}

type SQLDefault struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
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
