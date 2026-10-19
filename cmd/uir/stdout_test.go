package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(fn func() error) (string, error) {
	GinkgoHelper()
	reader, writer, err := os.Pipe()
	Expect(err).To(Succeed())
	defer func() { Expect(reader.Close()).To(Succeed()) }()
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(&output, reader)
		done <- err
	}()
	original := os.Stdout
	os.Stdout = writer
	runErr := func() error {
		defer func() {
			os.Stdout = original
			Expect(writer.Close()).To(Succeed())
		}()
		return fn()
	}()
	Expect(<-done).To(Succeed())
	return output.String(), runErr
}

var _ = Describe("stdout capture", func() {
	It("captures output larger than a pipe buffer and preserves the command error", func() {
		contents := strings.Repeat("command output\n", 1<<16)
		commandErr := errors.New("command failed after printing")

		output, err := captureStdout(func() error {
			if err := os.Stdout.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return err
			}
			if _, err := os.Stdout.WriteString(contents); err != nil {
				return err
			}
			return commandErr
		})

		Expect(err).To(MatchError(commandErr))
		Expect(output).To(Equal(contents))
	})
})
