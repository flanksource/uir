package sql_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSQLSpecs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SQL")
}
