//go:build !wasm

package uir_test

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The model package is compiled into browser bundles (oipa-cli's jep.wasm), so
// its js/wasm closure must stay free of heavy or terminal-only modules.
var _ = Describe("js/wasm build of the model package", func() {
	forbidden := []string{
		"github.com/alecthomas/chroma",
		"github.com/charmbracelet/lipgloss",
		"github.com/flanksource/commons",
		"github.com/flanksource/gomplate",
		"github.com/google/cel-go",
		"github.com/prometheus",
		"github.com/samber/lo",
		"golang.org/x/exp",
		"golang.org/x/text",
		"google.golang.org/protobuf",
	}

	It("imports none of the forbidden modules", func() {
		cmd := exec.Command("go", "list", "-deps", "-json=ImportPath", ".")
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
		cmd.Stderr = GinkgoWriter
		out, err := cmd.StdoutPipe()
		Expect(err).NotTo(HaveOccurred())
		Expect(cmd.Start()).To(Succeed())

		var offending []string
		decoder := json.NewDecoder(out)
		for {
			var pkg struct{ ImportPath string }
			err := decoder.Decode(&pkg)
			if errors.Is(err, io.EOF) {
				break
			}
			Expect(err).NotTo(HaveOccurred())
			for _, module := range forbidden {
				if pkg.ImportPath == module || strings.HasPrefix(pkg.ImportPath, module+"/") {
					offending = append(offending, pkg.ImportPath)
				}
			}
		}
		Expect(cmd.Wait()).To(Succeed())
		Expect(offending).To(BeEmpty(), "trace one with: GOOS=js GOARCH=wasm go list -deps -f '{{.ImportPath}} <- {{.Imports}}' .")
	})
})
