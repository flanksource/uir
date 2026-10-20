package state

import (
	ctx "context"

	"github.com/oipa/cli/internal/config"
	"github.com/oipa/cli/internal/envctx"
	"github.com/oipa/cli/internal/oipa/oipactx"
	orm "gorm.io/gorm"
)

var globalContext ctx.Context
var ConfigValue config.Config
var ConfigPointer *config.Config
var RuntimeValue envctx.Runtime
var RuntimePointer *envctx.Runtime
var OipaValue oipactx.OipaContext
var OipaPointer *oipactx.OipaContext
var DBValue orm.DB
var DBPointer *orm.DB

type Alias = *orm.DB

var Aliased Alias
var Inferred = makeDB()
var GroupA, GroupB *orm.DB

type NamedDB orm.DB
type Holder struct{ State *orm.DB }

var Distinct NamedDB
var Container []*orm.DB
var DeepPointer **orm.DB
var ContextPointer *ctx.Context

const Allowed = 1

func makeDB() *orm.DB { return nil }
func Local(parameter *orm.DB) *orm.DB {
	local := parameter
	return local
}
