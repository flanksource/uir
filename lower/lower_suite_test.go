package lower_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLowerSpecs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Lower")
}
