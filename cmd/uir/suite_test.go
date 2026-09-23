package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUIRCommand(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "UIR Command Suite")
}
