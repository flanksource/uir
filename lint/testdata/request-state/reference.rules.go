//go:build ruleguard

package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

func noGlobalRequestState(m dsl.Matcher) {
	m.Import("context")
	m.Import("github.com/oipa/cli/internal/config")
	m.Import("github.com/oipa/cli/internal/envctx")
	m.Import("github.com/oipa/cli/internal/oipa/oipactx")
	m.Import("gorm.io/gorm")

	isRequestState := func(v dsl.Var) bool {
		return v.Type.Is("context.Context") ||
			v.Type.Is("config.Config") ||
			v.Type.Is("*config.Config") ||
			v.Type.Is("envctx.Runtime") ||
			v.Type.Is("*envctx.Runtime") ||
			v.Type.Is("oipactx.OipaContext") ||
			v.Type.Is("*oipactx.OipaContext") ||
			v.Type.Is("gorm.DB") ||
			v.Type.Is("*gorm.DB")
	}

	m.Match(
		`var $name = $value`,
		`$name $typ`,
		`$name $typ = $value`,
	).
		Where(m["name"].Object.IsGlobal() && isRequestState(m["name"])).
		At(m["name"]).
		Report("package-level request state $name is shared across users; pass it explicitly or through context")
}
