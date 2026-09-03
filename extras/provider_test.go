package extras

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sharedprovider "github.com/roshbhatia/go-utils/provider"
)

func TestReleaseProvidersDeclareExternalCommands(t *testing.T) {
	tests := []struct {
		provider string
		commands []string
	}{
		{provider: "git", commands: []string{"bash", "git"}},
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

func TestGitProviderUsesMergedLabelsAndColorPolicy(t *testing.T) {
	directory := t.TempDir()
	local := filepath.Join(directory, "local")
	remote := filepath.Join(directory, "remote")
	if err := os.WriteFile(local, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remote, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plain := runGitProvider(t, local, remote, "internal/file.go", "never")
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain output contains ANSI: %q", plain)
	}
	if !strings.Contains(plain, "diff --git a/internal/file.go b/internal/file.go") {
		t.Fatalf("merged label missing from diff:\n%s", plain)
	}
	if strings.Contains(plain, directory) {
		t.Fatalf("temporary path leaked into diff:\n%s", plain)
	}

	colored := runGitProvider(t, local, remote, "internal/file.go", "always")
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("colored output has no ANSI: %q", colored)
	}
}

func runGitProvider(t *testing.T, local, remote, merged, color string) string {
	t.Helper()
	command := exec.Command("bash", "git/main.sh", local, remote, merged, color)
	output, err := command.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("git provider: %v\n%s", err, output)
	}
	return string(output)
}
