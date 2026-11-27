package lint

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/flanksource/sqlglot-go/database/migrate/workflow"
)

// compileProject validates a source entry that takes its mapping from a
// sqlglot project: it names the project file and the dialect, and nothing that
// would map files itself.
func (m *SourceMapping) compileProject() error {
	var conflicts []string
	for _, own := range []struct{ key, value string }{{"match", m.Match}, {"match_regex", m.MatchRegex}, {"producer", m.Producer}, {"transform", m.Transform}} {
		if own.value != "" {
			conflicts = append(conflicts, own.key)
		}
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("project %q maps its own files, so it cannot also set %s", m.Project, strings.Join(conflicts, ", "))
	}
	if !validSourcePath(m.Project) {
		return fmt.Errorf("invalid project path %q", m.Project)
	}
	if m.Dialect == "" {
		return fmt.Errorf("project %q requires a dialect", m.Project)
	}
	return nil
}

// projectSources are the sources every project entry maps, and the owner of
// each file they read, a mapped file or a companion, so a hand-written mapping
// or another project cannot claim it too.
type projectSources struct {
	sources []mappedSource
	claimed map[string]string
}

func readProjectSources(root fs.FS, mappings []SourceMapping) (projectSources, error) {
	out := projectSources{claimed: map[string]string{}}
	for _, mapping := range mappings {
		if mapping.Project == "" {
			continue
		}
		sources, err := mapping.projectSources(root)
		if err != nil {
			return out, fmt.Errorf("project %s: %w", mapping.Project, err)
		}
		for _, source := range sources {
			for _, name := range source.Inputs {
				if owner, taken := out.claimed[name]; taken && owner != mapping.selector() {
					return out, fmt.Errorf("%s is mapped by %s and %s", name, owner, mapping.selector())
				}
				out.claimed[name] = mapping.selector()
			}
		}
		out.sources = append(out.sources, sources...)
	}
	return out, nil
}

// projectSources loads the sqlglot project and maps each of its sources. The
// project's own default select is not applied: every mapped source is analysed
// and carries its labels, so a rule selects on them.
func (m SourceMapping) projectSources(root fs.FS) ([]mappedSource, error) {
	data, err := fs.ReadFile(root, m.Project)
	if err != nil {
		return nil, err
	}
	config, err := workflow.ParseProjectConfig(data)
	if err != nil {
		return nil, err
	}
	config.Select = nil
	directory := path.Dir(m.Project)
	tree, prefix := root, ""
	if directory != "." {
		if tree, err = fs.Sub(root, directory); err != nil {
			return nil, err
		}
		prefix = directory + "/"
	}
	project, err := config.Load(tree)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, fmt.Errorf("it maps no sources")
	}
	var sources []mappedSource
	for _, file := range project.Files() {
		source, err := m.projectSource(tree, prefix, file)
		if err != nil {
			return nil, fmt.Errorf("source %s: %w", file.Path, err)
		}
		sources = append(sources, source)
	}
	return sources, nil
}

// projectSource maps one project file as the equivalent hand-written mapping
// does: the bundle path is the filename, the project's name, phase and labels
// are the script metadata, the inputs are the mapped file first and then each
// companion its transform read (so a change to any of them selects the source),
// and findings land on the physical file whose bytes are the published content.
func (m SourceMapping) projectSource(tree fs.FS, prefix string, file workflow.ProjectFile) (mappedSource, error) {
	metadata, err := projectMetadata(file)
	if err != nil {
		return mappedSource{}, err
	}
	source := mappedSource{
		Content: file.Content, Filename: prefix + file.BundlePath, Package: "migrations", Metadata: metadata,
		Producer: string(file.Kind), Dialect: m.Dialect, DefaultSchema: m.DefaultSchema,
	}
	var origins []string
	for _, input := range file.Inputs {
		body, err := readSourceFile(tree, input)
		if err != nil {
			return mappedSource{}, err
		}
		source.Inputs = append(source.Inputs, prefix+input)
		if body == file.Content {
			origins = append(origins, prefix+input)
		}
	}
	if len(origins) == 1 {
		source.Origin = origins[0]
	}
	return source, nil
}

// projectMetadata is the metadata a transform returns for a SQL script: name,
// phase and labels, each only when the project sets it. It goes through JSON so
// it has the shape a decoded transform result has.
func projectMetadata(file workflow.ProjectFile) (map[string]any, error) {
	metadata := map[string]any{}
	if file.Name != "" {
		metadata["name"] = file.Name
	}
	if file.Phase != "" {
		metadata["phase"] = file.Phase
	}
	if len(file.Labels) > 0 {
		metadata["labels"] = file.Labels
	}
	if len(metadata) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	return decoded, json.Unmarshal(data, &decoded)
}
