package ui

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/roshbhatia/go-utils/diffview"
	"github.com/roshbhatia/traces/internal/source"
)

type diffRenderer struct {
	provider *source.Provider
	identity string
	mu       sync.Mutex
	cache    map[string]string
}

const (
	diffCacheMaxEntries = 128
	diffCacheTTL        = 7 * 24 * time.Hour
)

func newDiffRenderer(providers ...*source.Provider) *diffRenderer {
	var selected *source.Provider
	if len(providers) > 0 {
		selected = providers[0]
	}
	pruneDiffCache(diffCacheDirectory(), time.Now())
	return &diffRenderer{
		provider: selected,
		identity: diffProviderIdentity(selected),
		cache:    map[string]string{},
	}
}

func (renderer *diffRenderer) render(patch string, width int) string {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(renderer.identity+"\x00"+strconv.Itoa(width)+"\x00"+patch)))
	renderer.mu.Lock()
	if cached, ok := renderer.cache[key]; ok {
		renderer.mu.Unlock()
		return cached
	}
	renderer.mu.Unlock()
	if cached := readDiffCache(key); cached != "" {
		renderer.remember(key, cached)
		return cached
	}

	out := renderer.external(patch, width)
	if out == "" {
		out = diffview.Render(diffview.Options{Files: diffview.Parse(patch), Width: width})
	}
	renderer.remember(key, out)
	writeDiffCache(key, out)
	return out
}

func diffProviderIdentity(provider *source.Provider) string {
	manifest, _ := json.Marshal(provider)
	hash := sha256.New()
	_, _ = hash.Write(manifest)
	if provider == nil {
		return fmt.Sprintf("%x", hash.Sum(nil))
	}
	for _, argument := range provider.Manifest.Command {
		info, err := os.Stat(argument)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		_, _ = io.WriteString(hash, "\x00"+argument+"\x00")
		file, err := os.Open(argument)
		if err != nil {
			_, _ = io.WriteString(hash, err.Error())
			continue
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			_, _ = io.WriteString(hash, copyErr.Error())
		}
		if closeErr != nil {
			_, _ = io.WriteString(hash, closeErr.Error())
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func (renderer *diffRenderer) remember(key, output string) {
	renderer.mu.Lock()
	renderer.cache[key] = output
	renderer.mu.Unlock()
}

func (renderer *diffRenderer) external(patch string, width int) string {
	if renderer.provider == nil {
		return ""
	}
	files := diffview.Parse(patch)
	if len(files) == 0 {
		return ""
	}
	directory, err := os.MkdirTemp("", "traces-difftool-")
	if err != nil {
		return ""
	}
	defer func() { _ = os.RemoveAll(directory) }()
	outputs := make([]string, 0, len(files))
	for index, file := range files {
		local := filepath.Join(directory, fmt.Sprintf("%03d-local", index), filepath.Base(file.Path))
		remote := filepath.Join(directory, fmt.Sprintf("%03d-remote", index), filepath.Base(file.Path))
		if writeFragments(local, remote, file) != nil {
			continue
		}
		if output := renderer.run(local, remote, file.Path, width); output != "" {
			if len(files) > 1 {
				output = file.Path + "\n" + output
			}
			outputs = append(outputs, output)
		}
	}
	return strings.Join(outputs, "\n\n")
}

func (renderer *diffRenderer) run(local, remote, merged string, width int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := renderer.provider.RenderDiff(ctx, local, remote, merged, width)
	if err != nil {
		return ""
	}
	return output
}

func writeFragments(local, remote string, file diffview.File) error {
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(remote), 0o755); err != nil {
		return err
	}
	oldLines, newLines := []string{}, []string{}
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.Kind != '+' {
				oldLines = append(oldLines, line.Text)
			}
			if line.Kind != '-' {
				newLines = append(newLines, line.Text)
			}
		}
	}
	if err := os.WriteFile(local, []byte(strings.Join(oldLines, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.WriteFile(remote, []byte(strings.Join(newLines, "\n")+"\n"), 0o600)
}

func diffCachePath(key string) string {
	directory := diffCacheDirectory()
	if directory == "" {
		return ""
	}
	return filepath.Join(directory, key+".txt")
}

func diffCacheDirectory() string {
	root, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "traces", "diffs")
}

func readDiffCache(key string) string {
	path := diffCachePath(key)
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if time.Since(info.ModTime()) > diffCacheTTL {
		_ = os.Remove(path)
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	return string(data)
}

func writeDiffCache(key, output string) {
	path := diffCachePath(key)
	if path == "" || output == "" || os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".diff-*.txt")
	if err != nil {
		return
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if _, err := temporary.WriteString(output); err != nil {
		_ = temporary.Close()
		return
	}
	if temporary.Close() != nil {
		return
	}
	if os.Rename(temporaryPath, path) != nil {
		return
	}
	pruneDiffCache(filepath.Dir(path), time.Now())
}

func pruneDiffCache(directory string, now time.Time) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	type cacheEntry struct {
		path     string
		modified time.Time
	}
	files := make([]cacheEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".txt" {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if now.Sub(info.ModTime()) > diffCacheTTL {
			_ = os.Remove(path)
			continue
		}
		files = append(files, cacheEntry{path: path, modified: info.ModTime()})
	}
	if len(files) <= diffCacheMaxEntries {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modified.Before(files[j].modified)
	})
	for _, entry := range files[:len(files)-diffCacheMaxEntries] {
		_ = os.Remove(entry.path)
	}
}
