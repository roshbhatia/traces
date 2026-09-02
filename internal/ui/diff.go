package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/roshbhatia/go-utils/diffview"
)

type diffRenderer struct {
	command []string
	mu      sync.Mutex
	cache   map[string]string
}

func newDiffRenderer(commands ...[]string) *diffRenderer {
	var command []string
	if len(commands) > 0 {
		command = commands[0]
	}
	return &diffRenderer{command: command, cache: map[string]string{}}
}

func (renderer *diffRenderer) render(patch string, width int) string {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(renderer.command, "\x00")+"\x00"+strconv.Itoa(width)+"\x00"+patch)))
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

func (renderer *diffRenderer) remember(key, output string) {
	renderer.mu.Lock()
	renderer.cache[key] = output
	renderer.mu.Unlock()
}

func (renderer *diffRenderer) external(patch string, width int) string {
	if len(renderer.command) == 0 {
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
	command := expandDiffCommand(renderer.command, local, remote, merged, width)
	if !hasDiffFiles(renderer.command) {
		command = append(command, local, remote)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, command[0], command[1:]...)
	process.Env = append(os.Environ(),
		"LOCAL="+local,
		"REMOTE="+remote,
		"MERGED="+merged,
		"TRACES_DIFF_COLOR=always",
		"TRACES_DIFF_WIDTH="+strconv.Itoa(width),
	)
	var stdout bytes.Buffer
	process.Stdout = &stdout
	err := process.Run()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return ""
		}
	}
	return strings.TrimSpace(stdout.String())
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

func expandDiffCommand(command []string, local, remote, merged string, width int) []string {
	out := make([]string, len(command))
	for index, argument := range command {
		out[index] = strings.NewReplacer(
			"$LOCAL", local,
			"$REMOTE", remote,
			"$MERGED", merged,
			"$WIDTH", strconv.Itoa(width),
		).Replace(argument)
	}
	return out
}

func hasDiffFiles(command []string) bool {
	for _, argument := range command {
		if strings.Contains(argument, "$LOCAL") || strings.Contains(argument, "$REMOTE") {
			return true
		}
	}
	return false
}

func diffCachePath(key string) string {
	root, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "traces", "diffs", key+".txt")
}

func readDiffCache(key string) string {
	path := diffCachePath(key)
	if path == "" {
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
	_ = os.Rename(temporaryPath, path)
}
