package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/indexer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const renamedSource = "package browser\n\nfunc Renamed() {}\n"

// fakeGopatch writes a gopatch stand-in: with --diff it prints a one-line change to main.go, and
// otherwise applies it, then runs apply, a shell statement, in the checkout.
func fakeGopatch(apply string) string {
	GinkgoHelper()
	script := filepath.Join(GinkgoT().TempDir(), "gopatch")
	Expect(os.WriteFile(script, []byte("#!/bin/sh\nfor arg in \"$@\"; do\n  if [ \"$arg\" = \"--diff\" ]; then\n"+
		"    printf -- '--- main.go\\n+++ main.go\\n@@ -1 +1 @@\\n-a\\n+b\\n'\n    exit 0\n  fi\ndone\n"+
		"printf '"+strings.ReplaceAll(renamedSource, "\n", "\\n")+"' > main.go\n"+apply+"\n"), 0o755)).To(Succeed())
	return script
}

// refactorRequest posts a loopback, same-origin refactor operation.
func refactorRequest(ctx context.Context, handler http.Handler, operation string, body map[string]string) *httptest.ResponseRecorder {
	GinkgoHelper()
	encoded, err := json.Marshal(body)
	Expect(err).To(Succeed())
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/modules/refactor/"+operation, strings.NewReader(string(encoded))).WithContext(ctx)
	request.RemoteAddr = "127.0.0.1:4123"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// applyRefactor indexes the browser module without tests, previews a file move, and applies it.
func applyRefactor(ctx context.Context, gopatch string) (*commandRuntime, string, *httptest.ResponseRecorder) {
	GinkgoHelper()
	runtime, handler := servedRuntime(ctx)
	runtime.GopatchBin = gopatch
	workspace := browserModule()
	indexed, err := addModules(ctx, runtime.database, workspace, false)
	Expect(err).To(Succeed())
	browsed := httptest.NewRecorder()
	handler.ServeHTTP(browsed, httptest.NewRequest(http.MethodGet, "/api/v1/modules/browse?snapshot="+indexed[0].SnapshotID, nil))
	Expect(browsed.Code).To(Equal(http.StatusOK), browsed.Body.String())
	var browse struct {
		Sources []struct {
			ID string `json:"id"`
		} `json:"sources"`
	}
	Expect(json.Unmarshal(browsed.Body.Bytes(), &browse)).To(Succeed())
	body := map[string]string{"snapshot": indexed[0].SnapshotID, "source": browse.Sources[0].ID, "action": "move", "destination": "moved.go"}
	preview := refactorRequest(ctx, handler, "preview", body)
	Expect(preview.Code).To(Equal(http.StatusOK), preview.Body.String())
	var previewed refactorPreview
	Expect(json.Unmarshal(preview.Body.Bytes(), &previewed)).To(Succeed())
	body["preview-hash"] = previewed.PreviewHash
	return runtime, workspace, refactorRequest(ctx, handler, "apply", body)
}

var _ = Describe("explorer refactor apply", func() {
	It("reindexes the refactored checkout under the test setting its head was indexed with", func(ctx SpecContext) {
		runtime, workspace, response := applyRefactor(ctx, fakeGopatch(""))

		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var applied refactorApplyResult
		Expect(json.Unmarshal(response.Body.Bytes(), &applied)).To(Succeed())
		Expect(applied.Snapshots).To(ConsistOf(And(HaveField("Location", workspace), HaveField("Unchanged", false))))
		Expect(applied.RunID).ToNot(BeEmpty())
		engine, err := indexer.New(runtime.database)
		Expect(err).To(Succeed())
		Expect(engine.HeadIncludesTests(ctx, workspace)).To(BeFalse(), "the head indexed without tests stays without tests")
	})

	It("fails the apply, saying gopatch applied it, when the reindex fails", func(ctx SpecContext) {
		_, workspace, response := applyRefactor(ctx, fakeGopatch("rm go.mod"))

		Expect(response.Code).To(Equal(http.StatusInternalServerError), response.Body.String())
		var failure struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		Expect(json.Unmarshal(response.Body.Bytes(), &failure)).To(Succeed())
		Expect(failure.Code).To(Equal("reindex_failed"))
		Expect(failure.Message).To(And(ContainSubstring("Gopatch applied the refactor to "+filepath.Join(workspace, "main.go")), ContainSubstring("but reindexing failed")))
	})
})
