package uir_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUIRSpecs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "UIR")
}
