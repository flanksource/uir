package uir_test

import (
	"fmt"

	"github.com/flanksource/uir"
)

// ExampleUIR_Add builds a small document with the fluent builders, mirroring the
// walkthrough in README.md.
func ExampleUIR_Add() {
	method := uir.NewMethod("GetUser").
		WithPackage("com.example.service").
		WithType("UserService").
		WithParam(uir.Field("id", uir.RecordFieldTypeString)).
		WithReturnType(uir.TypeReference{Name: "User"}).
		WithBody(uir.NewBlock().
			WithStatement(uir.NewReturn(uir.VarExpr("user"))).
			Build()).
		Build()

	doc := &uir.UIR{}
	doc.Add(uir.NewPackage("com.example.service").WithFunction(method).Build())

	pkg := doc.Packages[0]
	fmt.Println(pkg.Package, pkg.Functions[0].Method, len(pkg.Functions[0].Params))
	// Output: com.example.service GetUser 1
}
