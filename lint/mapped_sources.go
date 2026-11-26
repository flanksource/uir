package lint

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/flanksource/sqlglot-go/database/migrate/workflow"
)

// SourceMapping selects source files and configures their producer. An entry
// with Project instead takes its file mapping from a .sqlglot-go.yaml project,
// so it sets only Dialect and DefaultSchema beside it.
type SourceMapping struct {
	Match         string `yaml:"match,omitempty"`
	MatchRegex    string `yaml:"match_regex,omitempty"`
	Producer      string `yaml:"producer,omitempty"`
	Dialect       string `yaml:"dialect,omitempty"`
	DefaultSchema string `yaml:"default_schema,omitempty"`
	Transform     string `yaml:"transform,omitempty"`
	// Project is the path of a sqlglot project file, relative to the project root.
	Project string `yaml:"project,omitempty"`
	regex   *regexp.Regexp
}

func (m SourceMapping) selector() string {
	switch {
	case m.Project != "":
		return "project:" + m.Project
	case m.MatchRegex != "":
		return "regex:" + m.MatchRegex
	}
	return "glob:" + m.Match
}

func validateSources(sources []SourceMapping) error {
	seen := map[string]bool{}
	for i := range sources {
		mapping := &sources[i]
		if err := mapping.compile(); err != nil {
			return fmt.Errorf("source mapping %d: %w", i+1, err)
		}
		if seen[mapping.selector()] {
			return fmt.Errorf("duplicate source mapping %s", mapping.selector())
		}
		seen[mapping.selector()] = true
	}
	return nil
}

func (m *SourceMapping) compile() error {
	if m.Project != "" {
		return m.compileProject()
	}
	if (m.Match == "") == (m.MatchRegex == "") {
		return errors.New("specify one of match or match_regex")
	}
	if m.Match != "" && (!validSourcePath(m.Match) || !doublestar.ValidatePattern(m.Match)) {
		return fmt.Errorf("invalid source glob %q", m.Match)
	}
	if m.MatchRegex != "" {
		rule, err := regexp.Compile(m.MatchRegex)
		if err != nil {
			return err
		}
		names := map[string]bool{}
		for _, name := range rule.SubexpNames() {
			if name != "" && names[name] {
				return fmt.Errorf("duplicate capture name %q", name)
			}
			names[name] = true
		}
		m.regex = rule
	}
	return nil
}

func (m SourceMapping) capture(name string) (map[string]string, bool) {
	captures := map[string]string{}
	if m.regex == nil {
		return captures, doublestar.MatchUnvalidated(m.Match, name)
	}
	matches := m.regex.FindStringSubmatch(name)
	if len(matches) == 0 || matches[0] != name {
		return nil, false
	}
	for index, key := range m.regex.SubexpNames() {
		if key != "" {
			captures[key] = matches[index]
		}
	}
	return captures, true
}

type mappedSource struct {
	Content       string         `json:"content"`
	Filename      string         `json:"filename"`
	Package       string         `json:"package"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Origin        string         `json:"-"`
	Inputs        []string       `json:"-"`
	Producer      string         `json:"-"`
	Dialect       string         `json:"-"`
	DefaultSchema string         `json:"-"`
}

func validSourcePath(name string) bool {
	return fs.ValidPath(name) && name != "." && !strings.ContainsAny(name, "\\:\x00")
}

func readMappedSources(directory string, mappings []SourceMapping) (_ []mappedSource, returnErr error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	projects, err := readProjectSources(root.FS(), mappings)
	if err != nil {
		return nil, err
	}
	var files []mappedSource
	filenames := map[string]string{}
	for _, source := range projects.sources {
		if previous, exists := filenames[source.Filename]; exists {
			return nil, fmt.Errorf("%s and %s both map to %s", previous, source.Inputs[0], source.Filename)
		}
		filenames[source.Filename] = source.Inputs[0]
	}
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || name == "." {
			return err
		}
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".") {
			return fs.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		var selected *SourceMapping
		var captures map[string]string
		for _, mapping := range mappings {
			if mapping.Project != "" {
				continue
			}
			if values, ok := mapping.capture(name); ok {
				if selected != nil {
					return fmt.Errorf("%s matches %s and %s", name, selected.selector(), mapping.selector())
				}
				selected, captures = &mapping, values
			}
		}
		if selected == nil {
			return nil
		}
		if owner, claimed := projects.claimed[name]; claimed {
			return fmt.Errorf("%s matches %s and %s", name, selected.selector(), owner)
		}
		file, err := selected.resolve(root.FS(), name, captures)
		if err != nil {
			return fmt.Errorf("source %s: %w", name, err)
		}
		if previous, exists := filenames[file.Filename]; exists {
			return fmt.Errorf("%s and %s both map to %s", previous, name, file.Filename)
		}
		filenames[file.Filename] = name
		files = append(files, file)
		return nil
	})
	return append(files, projects.sources...), err
}

func (m SourceMapping) resolve(fsys fs.FS, name string, captures map[string]string) (mappedSource, error) {
	raw, err := readSourceFile(fsys, name)
	if err != nil {
		return mappedSource{}, err
	}
	source := mappedSource{Content: raw, Filename: name, Package: "migrations"}
	reads := map[string]string{}
	if m.Transform != "" {
		fields, err := workflow.TransformSource(m.Transform, name, raw, captures, func(input string) (string, error) {
			if !validSourcePath(input) {
				return "", fmt.Errorf("invalid companion file %q", input)
			}
			companion := path.Join(path.Dir(name), input)
			body, err := readSourceFile(fsys, companion)
			if err == nil {
				reads[companion] = body
			}
			return body, err
		})
		if err != nil {
			return source, err
		}
		if _, ok := fields["content"]; !ok {
			return source, errors.New("transform must return content")
		}
		data, err := json.Marshal(fields)
		if err != nil {
			return source, err
		}
		source = mappedSource{}
		if err := workflow.Decode(data, &source); err != nil {
			return source, err
		}
	}
	if !validSourcePath(source.Filename) || strings.TrimSpace(source.Package) == "" {
		return source, errors.New("source requires a project-relative filename and a package")
	}
	source.Inputs = []string{name}
	for companion := range reads {
		source.Inputs = append(source.Inputs, companion)
	}
	slices.Sort(source.Inputs)
	origins := []string{}
	if source.Content == raw {
		origins = append(origins, name)
	}
	for companion, body := range reads {
		if body == source.Content && companion != name {
			origins = append(origins, companion)
		}
	}
	if len(origins) == 1 {
		source.Origin = origins[0]
	}
	source.Producer = cmp.Or(m.Producer, strings.TrimPrefix(strings.ToLower(path.Ext(source.Filename)), "."))
	source.Dialect, source.DefaultSchema = m.Dialect, m.DefaultSchema
	return source, nil
}

func readSourceFile(fsys fs.FS, name string) (string, error) {
	info, err := fs.Stat(fsys, name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", name)
	}
	body, err := fs.ReadFile(fsys, name)
	return string(body), err
}

func validateCompatibilityLevel(level int) error {
	if level != 0 && (level < 80 || level > 170 || level%10 != 0) {
		return fmt.Errorf("invalid SQL Server compatibility level %d", level)
	}
	return nil
}
