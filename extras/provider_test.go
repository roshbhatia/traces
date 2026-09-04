package extras

import (
	"slices"
	"testing"

	sharedprovider "github.com/roshbhatia/go-utils/provider"
)

func TestReleaseProvidersDeclareExternalCommands(t *testing.T) {
	tests := []struct {
		provider string
		commands []string
	}{
		{provider: "git", commands: []string{"git"}},
		{provider: "opencode", commands: []string{"opencode"}},
	}
	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			loaded, err := sharedprovider.Discover(test.provider)
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
