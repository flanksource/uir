package taskruns_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTaskRuns(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "UIR Task Runs Suite")
}
