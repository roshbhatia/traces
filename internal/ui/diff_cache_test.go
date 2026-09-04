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
		argv    func(string) []string
	}{
		{name: "executable", command: func(script string) []string { return []string{script} }},
		{name: "interpreter script", command: func(script string) []string { return []string{shell, script} }},
		{
			name:    "action interpreter script",
			command: func(string) []string { return []string{shell} },
			argv:    func(script string) []string { return []string{script} },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateDiffCache(t)
			script := filepath.Join(t.TempDir(), "provider.sh")
			writeDiffProvider(t, script, "first")
			var argv []string
			if test.argv != nil {
				argv = test.argv(script)
			}
			provider := diffProvider("test", test.command(script), argv)
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

func TestDiffMemoryCacheExpiresAndRemainsBounded(t *testing.T) {
	isolateDiffCache(t)
	renderer := newDiffRenderer()
	renderer.cache["expired"] = diffCacheEntry{
		output: "stale",
		stored: time.Now().Add(-diffCacheTTL - time.Hour),
	}
	if output, ok := renderer.cached("expired", time.Now()); ok || output != "" {
		t.Fatalf("expired memory entry = %q, %v", output, ok)
	}
	for index := 0; index < diffCacheMaxEntries+5; index++ {
		renderer.remember(fmt.Sprintf("entry-%d", index), "cached")
	}
	if got := len(renderer.cache); got != diffCacheMaxEntries {
		t.Fatalf("memory cache entries = %d, want %d", got, diffCacheMaxEntries)
	}
}

func TestDiffCachePrunesExpiredAndExcessEntries(t *testing.T) {
	isolateDiffCache(t)
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
	isolateDiffCache(t)
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
	if got, _ := readDiffCache("expired"); got != "" {
		t.Fatalf("expired cache read = %q", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired cache entry remains: %v", err)
	}
}

func TestDiffCacheKeepsDiskAgeInMemory(t *testing.T) {
	isolateDiffCache(t)
	path := diffCachePath("aging")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("cached"), 0o600); err != nil {
		t.Fatal(err)
	}
	stored := time.Now().Add(-6 * 24 * time.Hour)
	if err := os.Chtimes(path, stored, stored); err != nil {
		t.Fatal(err)
	}
	output, modified := readDiffCache("aging")
	if output != "cached" {
		t.Fatalf("disk cache read = %q", output)
	}
	renderer := newDiffRenderer()
	renderer.rememberAt("aging", output, modified)
	if output, ok := renderer.cached("aging", stored.Add(diffCacheTTL+time.Second)); ok || output != "" {
		t.Fatalf("aged memory entry = %q, %v", output, ok)
	}
}

func isolateDiffCache(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
}

func writeDiffProvider(t *testing.T, path, output string) {
	t.Helper()
	script := "#!/bin/sh\nprintf '%s\\n' " + output + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}
