package uir

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUIR_IsEmpty(t *testing.T) {
	assert.True(t, UIR{}.IsEmpty())

	assert.False(t, UIR{Modules: []ModuleNode{{}}}.IsEmpty())
	assert.False(t, UIR{Packages: []PackageNode{{}}}.IsEmpty())
	assert.False(t, UIR{Types: []TypedNode{{}}}.IsEmpty())
	assert.False(t, UIR{Records: []ASTRecord{{}}}.IsEmpty())
	assert.False(t, UIR{Endpoints: []ASTEndpoint{{}}}.IsEmpty())
	assert.False(t, UIR{Functions: []MethodNode{{}}}.IsEmpty())

	// RawFiles count: callers like policyadmin-cli emit verbatim TS files
	// (AsCode lookup tables) into UIR.RawFiles without populating any
	// other slice. Those UIRs must not be reported as empty by
	// generate.Run, which short-circuits on IsEmpty().
	assert.False(t, UIR{RawFiles: []RawFile{{Path: "x", Content: "y"}}}.IsEmpty())
}
