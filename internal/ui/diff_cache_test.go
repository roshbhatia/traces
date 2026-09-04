package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const cacheTestPatch = `--- a/file.go
+++ b/file.go
@@ -1 +1 @@
-old
+new
`

func TestDiffCacheInvalidatesChangedProviderFiles(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		command func(string) []string
	}{
		{name: "executable", command: func(script string) []string { return []string{script} }},
		{name: "interpreter script", command: func(script string) []string { return []string{shell, script} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			script := filepath.Join(t.TempDir(), "provider.sh")
			writeDiffProvider(t, script, "first")
			provider := diffProvider("test", test.command(script), nil)
			if got := newDiffRenderer(provider).render(cacheTestPatch, 80); got != "first" {
				t.Fatalf("first render = %q", got)
			}

			writeDiffProvider(t, script, "second")
			if got := newDiffRenderer(provider).render(cacheTestPatch, 80); got != "second" {
				t.Fatalf("render after provider change = %q", got)
			}
		})
	}
}

func TestDiffCachePrunesExpiredAndExcessEntries(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	directory := filepath.Dir(diffCachePath("current"))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	expired := filepath.Join(directory, "expired.txt")
	if err := os.WriteFile(expired, []byte("expired"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-diffCacheTTL - time.Hour)
	if err := os.Chtimes(expired, old, old); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < diffCacheMaxEntries+5; index++ {
		path := filepath.Join(directory, fmt.Sprintf("%064d.txt", index))
		if err := os.WriteFile(path, []byte("cached"), 0o600); err != nil {
			t.Fatal(err)
		}
		modified := now.Add(-time.Duration(diffCacheMaxEntries+5-index) * time.Second)
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}

	_ = newDiffRenderer()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != diffCacheMaxEntries {
		t.Fatalf("cache entries = %d, want %d", len(entries), diffCacheMaxEntries)
	}
	if _, err := os.Stat(expired); !os.IsNotExist(err) {
		t.Fatalf("expired cache entry remains: %v", err)
	}
}

func TestDiffCacheRejectsExpiredRead(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := diffCachePath("expired")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-diffCacheTTL - time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if got := readDiffCache("expired"); got != "" {
		t.Fatalf("expired cache read = %q", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired cache entry remains: %v", err)
	}
}

func writeDiffProvider(t *testing.T, path, output string) {
	t.Helper()
	script := "#!/bin/sh\nprintf '%s\\n' " + output + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}
