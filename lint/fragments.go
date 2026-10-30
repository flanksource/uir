package lint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const ConfigDirectoryName = ".uir.d"

var errExpiredConfig = errors.New("lint configuration has expired")

func readConfig(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var metadata struct {
		Expires *string `yaml:"_expires"`
	}
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if metadata.Expires == nil {
		return data, nil
	}
	expires, err := time.Parse(time.RFC3339, *metadata.Expires)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid _expires timestamp: %w", path, err)
	}
	if expires.After(time.Now()) {
		return data, nil
	}
	if err := os.Remove(path); err != nil {
		return nil, fmt.Errorf("delete expired lint configuration %s: %w", path, err)
	}
	return nil, fmt.Errorf("%s: %w", path, errExpiredConfig)
}

func directoryConfigurations(directory string) ([]string, error) {
	var paths []string
	path := filepath.Join(directory, ConfigName)
	if _, err := os.Stat(path); err == nil {
		paths = append(paths, path)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	fragments := filepath.Join(directory, ConfigDirectoryName)
	entries, err := os.ReadDir(fragments)
	if os.IsNotExist(err) {
		return paths, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		extension := filepath.Ext(entry.Name())
		if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && (extension == ".yaml" || extension == ".yml") {
			paths = append(paths, filepath.Join(fragments, entry.Name()))
		}
	}
	return paths, nil
}
