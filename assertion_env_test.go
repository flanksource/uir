package uir_test

import (
	"github.com/flanksource/uir"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AssertionEnv", func() {
	const (
		txPkg     = "policyadmin.transactions"
		brPkg     = "policyadmin.br"
		txName    = "IssueOverride"
		screenPkg = "policyadmin.screen"
		txKey     = txPkg + "." + txName + "." + txName
	)

	build := func() uir.UIR {
		first := uir.NewMethod(txName).
			WithPackage(txPkg).
			WithType(txName).
			WithParam(uir.NewRecordField("PolicyNumber", uir.RecordFieldTypeString).Build()).
			WithReturn(uir.NewRecordField("Status", uir.RecordFieldTypeString).Build()).
			WithProperties("rootElement", "Transaction", "spawnCount", 1.0).
			WithStatement(uir.NewMethodCall("EligibilityDefaults", uir.Identifier{Package: brPkg}).Build()).
			WithStatement(uir.NewIf(uir.ExprStmt{}).
				WithThen(uir.NewBlock().
					WithStatement(uir.NewMethodCall("NestedEligibility", uir.Identifier{Package: brPkg}).Build()).
					Build()).
				Build()).
			Build()
		second := uir.NewMethod(txName).WithPackage(txPkg).WithType(txName).
			WithProperties("rootElement", "Transaction").Build()
		screen := uir.NewMethod("MemberScreen").WithPackage(screenPkg).WithType("MemberScreen").
			WithProperties("rootElement", "IntakeProfileScreen").Build()

		result := uir.UIR{}
		result.Add(first)
		result.Add(second)
		result.Add(screen)
		return result
	}

	methodOf := func(index map[string]any, key string) any {
		Expect(index).To(HaveKey(key))
		return index[key].(map[string]any)["method"]
	}

	It("indexes functions by symbol, method name and root element, suffixing duplicates", func() {
		env, err := build().AssertionEnv()
		Expect(err).NotTo(HaveOccurred())
		index := env["symbols"].(map[string]any)

		Expect(map[string]any{
			txKey:                      methodOf(index, txKey),
			txKey + "#2":               methodOf(index, txKey+"#2"),
			"root.Transaction":         methodOf(index, "root.Transaction"),
			"root.Transaction#2":       methodOf(index, "root.Transaction#2"),
			"root.IntakeProfileScreen": methodOf(index, "root.IntakeProfileScreen"),
			"method.MemberScreen":      methodOf(index, "method.MemberScreen"),
		}).To(Equal(map[string]any{
			txKey:                      txName,
			txKey + "#2":               txName,
			"root.Transaction":         txName,
			"root.Transaction#2":       txName,
			"root.IntakeProfileScreen": "MemberScreen",
			"method.MemberScreen":      "MemberScreen",
		}))
		Expect(index[txKey+"._instances"]).To(HaveLen(2))
	})

	It("indexes params, returns, properties and nested calls of a function", func() {
		env, err := build().AssertionEnv()
		Expect(err).NotTo(HaveOccurred())
		index := env["symbols"].(map[string]any)

		Expect(index[txKey+"._params"]).To(HaveKey("PolicyNumber"))
		Expect(index[txKey+"._returns"]).To(HaveKey("Status"))
		Expect(index[txKey+"._properties"]).To(HaveKeyWithValue("spawnCount", 1.0))
		Expect(index[txKey+"._calls"]).To(And(
			HaveKey(brPkg+".EligibilityDefaults"),
			HaveKey(brPkg+".NestedEligibility"),
		))
	})

	It("aliases CEL-reserved keys so package/type fields stay addressable", func() {
		env, err := build().ToMap()
		Expect(err).NotTo(HaveOccurred())

		fn := env["functions"].([]any)[0].(map[string]any)
		Expect(fn).To(HaveKeyWithValue("pkg", fn["package"]))
		Expect(fn).To(HaveKeyWithValue("clazz", fn["type"]))
	})

	It("reuses an existing index instead of rebuilding it", func() {
		env, err := build().AssertionEnv()
		Expect(err).NotTo(HaveOccurred())
		first := env["symbols"].(map[string]any)
		first["sentinel"] = true

		Expect(uir.AddAssertionIndex(env)).To(HaveKeyWithValue("sentinel", true))
	})
})
