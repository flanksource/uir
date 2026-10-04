package main

import (
	"bytes"
	"fmt"
	"os"
)

type artifact struct {
	Path string
	Data []byte
}

type artifactOptions struct {
	Check bool
}

func updateArtifacts(artifacts []artifact, options artifactOptions) error {
	for _, output := range artifacts {
		if options.Check {
			actual, err := os.ReadFile(output.Path)
			if err != nil {
				return fmt.Errorf("checking %s: %w", output.Path, err)
			}
			if !bytes.Equal(actual, output.Data) {
				return fmt.Errorf("%s is stale; run task schema", output.Path)
			}
			continue
		}
		if err := os.WriteFile(output.Path, output.Data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", output.Path, err)
		}
	}
	return nil
}
