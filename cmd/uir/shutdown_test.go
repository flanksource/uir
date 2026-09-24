package main

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/flanksource/clicky/shutdown"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUIRShutdownHelper(t *testing.T) {
	mode := os.Getenv("UIR_SHUTDOWN_TEST_MODE")
	if mode == "" {
		return
	}
	shutdown.AddHook("record UIR shutdown", func() {
		if err := os.WriteFile(os.Getenv("UIR_SHUTDOWN_TEST_MARKER"), []byte("shutdown"), 0o600); err != nil {
			panic(err)
		}
	})
	switch mode {
	case "serve":
		os.Args = []string{"uir", "--dsn", os.Getenv("UIR_SHUTDOWN_TEST_DSN"), "serve", "--host", "127.0.0.1", "--port", os.Getenv("UIR_SHUTDOWN_TEST_PORT")}
	case "error":
		os.Args = []string{"uir", "--invalid-flag"}
	case "version":
		os.Args = []string{"uir", "version"}
	default:
		t.Fatalf("unknown shutdown test mode %q", mode)
	}
	main()
}

var _ = Describe("UIR command shutdown", func() {
	It("runs Clicky shutdown after Ctrl+C stops the HTTP listener", func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).To(Succeed())
		port := listener.Addr().(*net.TCPAddr).Port
		Expect(listener.Close()).To(Succeed())

		executable, err := os.Executable()
		Expect(err).To(Succeed())
		marker := filepath.Join(GinkgoT().TempDir(), "shutdown.marker")
		command := exec.Command(executable, "-test.run=^TestUIRShutdownHelper$")
		command.Env = append(os.Environ(),
			"UIR_SHUTDOWN_TEST_MODE=serve",
			"UIR_SHUTDOWN_TEST_MARKER="+marker,
			"UIR_SHUTDOWN_TEST_DSN="+filepath.Join(GinkgoT().TempDir(), "uir.db"),
			"UIR_SHUTDOWN_TEST_PORT="+strconv.Itoa(port),
		)
		var stderr bytes.Buffer
		command.Stderr = &stderr
		Expect(command.Start()).To(Succeed())
		DeferCleanup(func() {
			if command.ProcessState == nil {
				_ = command.Process.Kill()
				_ = command.Wait()
			}
		})

		address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		Eventually(func() error {
			connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
			if err != nil {
				return err
			}
			return connection.Close()
		}, 20*time.Second, 50*time.Millisecond).Should(Succeed())
		Expect(command.Process.Signal(os.Interrupt)).To(Succeed())
		finished := make(chan error, 1)
		go func() { finished <- command.Wait() }()
		select {
		case err := <-finished:
			Expect(err).To(Succeed(), stderr.String())
		case <-time.After(10 * time.Second):
			_ = command.Process.Kill()
			<-finished
			Fail("UIR did not exit within 10 seconds of Ctrl+C")
		}
		contents, err := os.ReadFile(marker)
		Expect(err).To(Succeed())
		Expect(contents).To(Equal([]byte("shutdown")))
		listener, err = net.Listen("tcp", address)
		Expect(err).To(Succeed())
		Expect(listener.Close()).To(Succeed())
	})

	DescribeTable("runs Clicky shutdown after a command returns", func(mode string, expectedCode int) {
		executable, err := os.Executable()
		Expect(err).To(Succeed())
		marker := filepath.Join(GinkgoT().TempDir(), "shutdown.marker")
		command := exec.Command(executable, "-test.run=^TestUIRShutdownHelper$")
		command.Env = append(os.Environ(), "UIR_SHUTDOWN_TEST_MODE="+mode, "UIR_SHUTDOWN_TEST_MARKER="+marker)
		err = command.Run()
		if expectedCode == 0 {
			Expect(err).To(Succeed())
		} else {
			Expect(err).To(HaveOccurred())
		}
		Expect(command.ProcessState.ExitCode()).To(Equal(expectedCode))
		contents, err := os.ReadFile(marker)
		Expect(err).To(Succeed())
		Expect(contents).To(Equal([]byte("shutdown")))
	}, Entry("success", "version", 0), Entry("error", "error", 1))
})
