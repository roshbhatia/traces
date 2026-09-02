package source

import (
	"maps"
	"slices"
	"sort"
	"strings"

	sharedconfig "github.com/roshbhatia/go-utils/config"
)

// Manifest declares one external source provider and its advertised abilities.
type Manifest struct {
	Command      []string `json:"command" yaml:"command" jsonschema:"required,minItems=1"`
	Capabilities []string `json:"capabilities" yaml:"capabilities" jsonschema:"required,minItems=1"`
}

// Diff configures a Git-compatible difftool command.
type Diff struct {
	Command []string `json:"command,omitempty" yaml:"command"`
}

// Settings is the public Traces YAML surface.
type Settings struct {
	Color     string              `json:"color,omitempty" yaml:"color" jsonschema:"enum=auto,enum=always,enum=never"`
	Diff      Diff                `json:"diff,omitempty" yaml:"diff"`
	Providers map[string]Manifest `json:"providers,omitempty" yaml:"providers"`
	Sources   map[string][]string `json:"sources,omitempty" yaml:"sources"`
}

// Default keeps the core independent from every harness and difftool.
func Default() Settings {
	return Settings{
		Color:     "auto",
		Providers: map[string]Manifest{},
		Sources:   map[string][]string{},
	}
}

// LoadSettings applies YAML and TRACES_* environment overrides.
func LoadSettings(path string) (Settings, error) {
	return sharedconfig.Load(Default(), sharedconfig.Options{
		Name: "traces", EnvPrefix: "TRACES", Path: path,
	})
}

// ConfigFile returns the selected YAML path.
func ConfigFile(path string) string {
	selected, err := sharedconfig.Path(sharedconfig.Options{
		Name: "traces", EnvPrefix: "TRACES", Path: path,
	})
	if err != nil {
		return path
	}
	return selected
}

// Schema emits the configuration schema from the same Go types.
func Schema() ([]byte, error) {
	return sharedconfig.Schema[Settings]("Traces configuration")
}

func wanted(table map[string][]string, service string) []*Provider {
	harnesses := slices.Sorted(maps.Keys(table))
	out, seen := []*Provider{}, map[string]*Provider{}
	for _, harness := range harnesses {
		for _, name := range table[harness] {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if found, ok := seen[name]; ok {
				found.harnesses = append(found.harnesses, harness)
				continue
			}
			one := &Provider{Name: name, harnesses: []string{harness}}
			seen[name] = one
			out = append(out, one)
		}
	}
	if service == "" {
		return out
	}
	kept := []*Provider{}
	for _, one := range out {
		if one.answers(service) {
			kept = append(kept, one)
		}
	}
	return kept
}

func (provider Provider) answers(service string) bool {
	if service == "" || len(provider.harnesses) == 0 {
		return true
	}
	for _, harness := range provider.harnesses {
		if strings.HasPrefix(harness, service) || strings.HasPrefix(service, harness) {
			return true
		}
	}
	return false
}

// For names the harnesses a source answers for.
func (provider Provider) For() string {
	if len(provider.harnesses) == 0 {
		return ""
	}
	out := append([]string{}, provider.harnesses...)
	sort.Strings(out)
	return strings.Join(out, "+")
}
