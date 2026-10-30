package symboldiff

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	movedFrom = "example.org/shop/cmd"
	movedTo   = "example.org/shop/models"
)

var _ = DescribeTable("movedShapeEqual",
	func(before, after string, equal bool) {
		Expect(movedShapeEqual(before, after, movedFrom, movedTo)).To(Equal(equal))
	},
	Entry("equal shapes", "method public (a (int)) ( (int))", "method public (a (int)) ( (int))", true),
	Entry("a qualifier of the destination package dropped by the move",
		"method public (item (models.Item)) ( (int))", "method public (item (Item)) ( (int))", true),
	Entry("a qualifier of the source package added by the move",
		"method public (o (Options)) ( (error))", "method public (o (cmd.Options)) ( (error))", true),
	Entry("a type that really changed", "method public (a (int)) ( (int))", "method public (a (string)) ( (int))", false),
	Entry("a qualifier of an unrelated package", "method public (t (time.Time))", "method public (t (Time))", false),
	Entry("a name that only ends like the package", "method public (x (mymodels.Item))", "method public (x (myItem))", false),
)
