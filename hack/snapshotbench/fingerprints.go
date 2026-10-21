//go:build hack

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

func captureBuildInputs(ctx context.Context, manifest *runManifest, originalOIPA string) error {
	builds := map[string]string{}
	for name, path := range map[string]string{"runner": manifest.Runner, "oipa": manifest.OIPA, "gavel": manifest.Gavel} {
		body, err := commandOutput(ctx, "go", "version", "-m", path)
		if err != nil {
			return err
		}
		builds[name] = body
	}
	if err := writeJSON(filepath.Join(manifest.Directory, "build-info.json"), builds); err != nil {
		return err
	}
	for name, directory := range map[string]string{"uir": manifest.WorkingDir, "oipa": filepath.Dir(filepath.Dir(originalOIPA))} {
		paths, err := commandOutput(ctx, "git", "-C", directory, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
		if err != nil {
			return err
		}
		checksums := map[string]string{}
		for _, path := range strings.Split(paths, "\x00") {
			if !strings.HasSuffix(path, ".go") && path != "go.mod" && path != "go.sum" {
				continue
			}
			body, err := os.ReadFile(filepath.Join(directory, path))
			if err != nil {
				return err
			}
			digest := sha256.Sum256(body)
			checksums[path] = hex.EncodeToString(digest[:])
		}
		if err := writeJSON(filepath.Join(manifest.Directory, name+"-source-checksums.json"), checksums); err != nil {
			return err
		}
	}
	return nil
}
