package uir_test

import (
	"encoding/json"
	"testing"

	"github.com/flanksource/uir"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTypeReference_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		ref  uir.TypeReference
	}{
		{
			name: "simple type",
			ref:  uir.SimpleType("string"),
		},
		{
			name: "generic Array<string>",
			ref:  uir.GenericType("Array", uir.SimpleType("string")),
		},
		{
			name: "generic Map<string, number>",
			ref:  uir.GenericType("Map", uir.SimpleType("string"), uir.SimpleType("number")),
		},
		{
			name: "union string | number | null",
			ref:  uir.UnionType(uir.SimpleType("string"), uir.SimpleType("number"), uir.SimpleType("null")),
		},
		{
			name: "intersection A & B",
			ref:  uir.IntersectionType(uir.SimpleType("A"), uir.SimpleType("B")),
		},
		{
			name: "optional type",
			ref:  uir.OptionalType(uir.SimpleType("string")),
		},
		{
			name: "array type",
			ref:  uir.ArrayType(uir.SimpleType("number")),
		},
		{
			name: "nested generic Promise<Array<string>>",
			ref:  uir.GenericType("Promise", uir.GenericType("Array", uir.SimpleType("string"))),
		},
		{
			name: "literal type",
			ref:  uir.TypeReference{LiteralValue: strPtr("hello")},
		},
		{
			name: "tuple type",
			ref: uir.TypeReference{
				TupleElements: []uir.TypeReference{
					uir.SimpleType("string"),
					uir.SimpleType("number"),
				},
			},
		},
		{
			name: "function type",
			ref: uir.TypeReference{
				FunctionParams:  []uir.TypeReference{uir.SimpleType("string"), uir.SimpleType("number")},
				FunctionReturns: &uir.TypeReference{Name: "boolean"},
			},
		},
		{
			name: "raw fallback",
			ref:  uir.TypeReference{RawType: "keyof typeof SomeEnum"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.ref)
			require.NoError(t, err)

			var result uir.TypeReference
			err = json.Unmarshal(data, &result)
			require.NoError(t, err)

			assert.Equal(t, tt.ref, result)
		})
	}
}

func TestTypeParam_JSONRoundTrip(t *testing.T) {
	param := uir.TypeParam{
		Name:       "T",
		Constraint: &uir.TypeReference{Name: "Comparable"},
		Default:    &uir.TypeReference{Name: "string"},
	}

	data, err := json.Marshal(param)
	require.NoError(t, err)

	var result uir.TypeParam
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, param, result)
}

func TestTypeReference_IsEmpty(t *testing.T) {
	assert.True(t, uir.TypeReference{}.IsEmpty())
	assert.False(t, uir.SimpleType("string").IsEmpty())
	assert.False(t, uir.UnionType(uir.SimpleType("a")).IsEmpty())
	assert.False(t, uir.TypeReference{RawType: "something"}.IsEmpty())
}

func TestMethodNode_NewFields_JSONRoundTrip(t *testing.T) {
	method := uir.NewMethod("fetchData", uir.Identifier{Package: "api"}).
		WithVisibility(uir.VisibilityPublic).
		Build()

	method.IsAsync = true
	method.IsGenerator = false
	method.TypeParams = []uir.TypeParam{
		{Name: "T", Constraint: &uir.TypeReference{Name: "Response"}},
	}
	method.ReturnType = &uir.TypeReference{
		Name:     "Promise",
		TypeArgs: []uir.TypeReference{uir.SimpleType("T")},
	}

	data, err := json.Marshal(method)
	require.NoError(t, err)

	var result uir.MethodNode
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.True(t, result.IsAsync)
	assert.False(t, result.IsGenerator)
	assert.Equal(t, 1, len(result.TypeParams))
	assert.Equal(t, "T", result.TypeParams[0].Name)
	assert.NotNil(t, result.ReturnType)
	assert.Equal(t, "Promise", result.ReturnType.Name)
}

func TestTypedNode_NewFields_JSONRoundTrip(t *testing.T) {
	builder := uir.NewType("Container")
	typeNode := builder.Build()

	typeNode.IsAbstract = true
	typeNode.TypeParams = []uir.TypeParam{
		{Name: "T", Constraint: &uir.TypeReference{Name: "Base"}},
	}
	typeNode.Extends = &uir.TypeReference{Name: "BaseClass"}
	typeNode.Implements = []uir.TypeReference{
		uir.SimpleType("Serializable"),
		uir.SimpleType("Comparable"),
	}

	data, err := json.Marshal(typeNode)
	require.NoError(t, err)

	var result uir.TypedNode
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.True(t, result.IsAbstract)
	assert.Equal(t, 1, len(result.TypeParams))
	assert.Equal(t, "T", result.TypeParams[0].Name)
	assert.NotNil(t, result.Extends)
	assert.Equal(t, "BaseClass", result.Extends.Name)
	assert.Equal(t, 2, len(result.Implements))
}

func TestRecordField_TypeRef_JSONRoundTrip(t *testing.T) {
	field := uir.Field("items", uir.RecordFieldTypeArray)
	field.TypeRef = &uir.TypeReference{
		Name:    "Array",
		IsArray: true,
		TypeArgs: []uir.TypeReference{
			uir.GenericType("Map", uir.SimpleType("string"), uir.SimpleType("number")),
		},
	}

	data, err := json.Marshal(field)
	require.NoError(t, err)

	var result uir.RecordField
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.NotNil(t, result.TypeRef)
	assert.Equal(t, "Array", result.TypeRef.Name)
	assert.True(t, result.TypeRef.IsArray)
	assert.Equal(t, 1, len(result.TypeRef.TypeArgs))
	assert.Equal(t, "Map", result.TypeRef.TypeArgs[0].Name)
}

func TestNewStatements_JSONRoundTrip(t *testing.T) {
	t.Run("RawStmt", func(t *testing.T) {
		stmt := uir.RawStmt{
			Source:   "const x = a?.b?.c ?? 'default'",
			Language: "typescript",
		}
		data, err := json.Marshal(stmt)
		require.NoError(t, err)

		var result uir.RawStmt
		err = json.Unmarshal(data, &result)
		require.NoError(t, err)
		assert.Equal(t, stmt.Source, result.Source)
		assert.Equal(t, stmt.Language, result.Language)
	})

	t.Run("DestructuringStmt", func(t *testing.T) {
		stmt := uir.DestructuringStmt{
			IsArray: false,
			Bindings: []uir.DestructureBinding{
				{Name: "name"},
				{Name: "age", Alias: "userAge"},
			},
			Rest: strPtr("rest"),
		}
		data, err := json.Marshal(stmt)
		require.NoError(t, err)

		var result uir.DestructuringStmt
		err = json.Unmarshal(data, &result)
		require.NoError(t, err)
		assert.Equal(t, 2, len(result.Bindings))
		assert.Equal(t, "userAge", result.Bindings[1].Alias)
		assert.NotNil(t, result.Rest)
		assert.Equal(t, "rest", *result.Rest)
	})

	t.Run("TemplateLiteralStmt", func(t *testing.T) {
		stmt := uir.TemplateLiteralStmt{
			Strings: []string{"Hello ", ", you have ", " items"},
		}
		data, err := json.Marshal(stmt)
		require.NoError(t, err)

		var result uir.TemplateLiteralStmt
		err = json.Unmarshal(data, &result)
		require.NoError(t, err)
		assert.Equal(t, 3, len(result.Strings))
	})
}

func strPtr(s string) *string { return &s }
