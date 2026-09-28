package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("explorer refactor preview", func() {
	It("resolves changed files in the producer and consumer checkouts", func() {
		root := filepath.Join(GinkgoT().TempDir(), "producer")
		diff := "--- src/store.go\n+++ src/store.go\n@@ -1 +1 @@\n-a\n+b\n--- /dev/null\n+++ ../consumer/model.go\n@@ -0,0 +1 @@\n+x\n"
		files, err := refactorDiffFiles(diff, root)
		Expect(err).To(Succeed())
		Expect(files).To(Equal([]string{filepath.Join(filepath.Dir(root), "consumer", "model.go"), filepath.Join(root, "src", "store.go")}))
	})

	It("rejects malformed diff paths", func() {
		_, err := refactorDiffFiles("--- /dev/null\n+++ README.md\n", GinkgoT().TempDir())
		Expect(err).To(MatchError(ContainSubstring("invalid path")))
	})

	It("does not treat edited source lines as diff file headers", func() {
		root := GinkgoT().TempDir()
		diff := "--- store.go\n+++ store.go\n@@ -1,2 +1,2 @@\n--- README.md\n+++ README.md\n unchanged\n"
		files, err := refactorDiffFiles(diff, root)
		Expect(err).To(Succeed())
		Expect(files).To(Equal([]string{filepath.Join(root, "store.go")}))
	})

	It("accepts only loopback same-origin refactor requests", func() {
		request := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/v1/modules/refactor/apply", nil)
		request.RemoteAddr = "127.0.0.1:4123"
		request.Header.Set("Origin", "http://localhost:8080")
		Expect(localRefactorRequest(request)).To(BeTrue())
		request.Header.Set("Origin", "http://elsewhere.test")
		Expect(localRefactorRequest(request)).To(BeFalse())
		request.Header.Set("Origin", "http://localhost:8080")
		request.RemoteAddr = "192.0.2.1:4123"
		Expect(localRefactorRequest(request)).To(BeFalse())
		request.RemoteAddr = "127.0.0.1:4123"
		request.Host = "attacker.test:8080"
		request.Header.Set("Origin", "http://attacker.test:8080")
		Expect(localRefactorRequest(request)).To(BeFalse())
	})

	It("resolves a relative SQLite DSN before gopatch changes working directory", func() {
		absolute, err := filepath.Abs(".tmp/index.db")
		Expect(err).To(Succeed())
		plain, err := refactorIndexDSN(".tmp/index.db")
		Expect(err).To(Succeed())
		Expect(plain).To(Equal(absolute))
		uri, err := refactorIndexDSN("sqlite://.tmp/index.db?cache=shared")
		Expect(err).To(Succeed())
		Expect(uri).To(Equal("sqlite://" + absolute + "?cache=shared"))
	})
})
