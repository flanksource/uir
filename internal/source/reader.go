package source

import (
	"bufio"
	"fmt"
	"os"
	"sync"
)

// Reader provides cached access to source code lines for on-demand retrieval
type Reader struct {
	cache map[string][]string
	mutex sync.RWMutex
}

// NewReader creates a new source reader with caching
func NewReader() *Reader {
	return &Reader{
		cache: make(map[string][]string),
	}
}

// GetLine retrieves a specific line from a file (1-based line numbering)
func (r *Reader) GetLine(filepath string, lineNum int) (string, error) {
	lines, err := r.getLines(filepath)
	if err != nil {
		return "", err
	}

	if lineNum < 1 || lineNum > len(lines) {
		return "", fmt.Errorf("line %d out of range for file %s (max: %d)", lineNum, filepath, len(lines))
	}

	return lines[lineNum-1], nil
}

// GetLines retrieves a range of lines from a file (inclusive, 1-based line numbering)
func (r *Reader) GetLines(filepath string, start, end int) ([]string, error) {
	lines, err := r.getLines(filepath)
	if err != nil {
		return nil, err
	}

	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return []string{}, nil
	}

	return lines[start-1 : end], nil
}

// getLines loads and caches all lines from a file
func (r *Reader) getLines(filepath string) ([]string, error) {
	// Check cache first (read lock)
	r.mutex.RLock()
	if lines, exists := r.cache[filepath]; exists {
		r.mutex.RUnlock()
		return lines, nil
	}
	r.mutex.RUnlock()

	// Load file (write lock)
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Double-check cache after acquiring write lock
	if lines, exists := r.cache[filepath]; exists {
		return lines, nil
	}

	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filepath, err)
	}
	defer func() { _ = file.Close() }()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filepath, err)
	}

	// Cache the lines
	r.cache[filepath] = lines
	return lines, nil
}

// ClearCache removes all cached file contents
func (r *Reader) ClearCache() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.cache = make(map[string][]string)
}

// RemoveFromCache removes a specific file from cache
func (r *Reader) RemoveFromCache(filepath string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	delete(r.cache, filepath)
}
