package symbolhandle_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSymbolHandle(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Symbol handle Suite")
}
