package extras

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	sharedprovider "github.com/roshbhatia/go-utils/provider"
)

func TestReleaseProvidersDeclareExternalCommands(t *testing.T) {
	tests := []struct {
		provider string
		commands []string
	}{
		{provider: "devin", commands: []string{"sqlite3"}},
		{provider: "git", commands: []string{"git"}},
		{provider: "opencode", commands: []string{"opencode"}},
	}
	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			manifest, err := os.ReadFile(filepath.Join(test.provider, "provider.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			installed := t.TempDir()
			if err := os.WriteFile(filepath.Join(installed, "provider.yaml"), manifest, 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := sharedprovider.Discover(installed)
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded) != 1 {
				t.Fatalf("discovered %d manifests", len(loaded))
			}
			for _, command := range test.commands {
				if !slices.Contains(loaded[0].Manifest.Requires.Commands, command) {
					t.Errorf("required commands %v omit %q", loaded[0].Manifest.Requires.Commands, command)
				}
			}
		})
	}
}
