package sqlsyntax_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
)

func TestSQLSyntax(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SQL Navigation Syntax Suite")
}
