package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLauncherResolvesSiblingCoreAndProviderPaths(t *testing.T) {
	archiveDirectory := t.TempDir()
	archiveDirectory, err := filepath.EvalSymlinks(archiveDirectory)
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(archiveDirectory, "traces")
	build := exec.Command("go", "build", "-o", launcher, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build launcher: %v\n%s", err, output)
	}
	core := filepath.Join(archiveDirectory, ".traces-core")
	script := `#!/bin/sh
printf '%s\n' "$PATH" "$XDG_DATA_DIRS" "$1"
`
	if err := os.WriteFile(core, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	symlinkDirectory := t.TempDir()
	symlink := filepath.Join(symlinkDirectory, "traces")
	if err := os.Symlink(launcher, symlink); err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		archiveDirectory + string(os.PathListSeparator) + "/original/bin",
		filepath.Join(archiveDirectory, "share") + string(os.PathListSeparator) + "/original/share",
		"passed-through",
	}, "\n") + "\n"

	for name, executable := range map[string]string{
		"direct":  launcher,
		"symlink": symlink,
	} {
		t.Run(name, func(t *testing.T) {
			command := exec.Command(executable, "passed-through")
			command.Env = []string{"PATH=/original/bin", "XDG_DATA_DIRS=/original/share"}
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("run launcher: %v\n%s", err, output)
			}
			if string(output) != want {
				t.Fatalf("launcher output = %q, want %q", output, want)
			}
		})
	}
}
