package symboldiff

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSymbolDiff(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "UIR Symbol Diff Suite")
}
