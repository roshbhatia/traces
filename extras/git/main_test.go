package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderDiffUsesMergedLabelsAndColorPolicy(t *testing.T) {
	directory := t.TempDir()
	local := filepath.Join(directory, "local")
	remote := filepath.Join(directory, "remote")
	if err := os.WriteFile(local, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remote, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plain, status, err := renderDiff(local, remote, "internal/file.go", "never")
	if err != nil || status != 1 {
		t.Fatalf("git provider: status %d, error %v", status, err)
	}
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain output contains ANSI: %q", plain)
	}
	if !strings.Contains(plain, "diff --git a/internal/file.go b/internal/file.go") {
		t.Fatalf("merged label missing from diff:\n%s", plain)
	}
	if strings.Contains(plain, directory) {
		t.Fatalf("temporary path leaked into diff:\n%s", plain)
	}

	colored, _, err := renderDiff(local, remote, "internal/file.go", "always")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("colored output has no ANSI: %q", colored)
	}
}
