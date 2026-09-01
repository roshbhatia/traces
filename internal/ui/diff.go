package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/roshbhatia/go-utils/diffview"
)

const diffProviderEnv = "TRACES_DIFF_PROVIDER"

type diffRenderer struct {
	provider string
	mu       sync.Mutex
	cache    map[string]string
}

func newDiffRenderer() *diffRenderer {
	return &diffRenderer{provider: strings.TrimSpace(os.Getenv(diffProviderEnv)), cache: map[string]string{}}
}

func (r *diffRenderer) render(patch string, width int) string {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(r.provider+"\x00"+strconv.Itoa(width)+"\x00"+patch)))
	r.mu.Lock()
	if cached, ok := r.cache[key]; ok {
		r.mu.Unlock()
		return cached
	}
	r.mu.Unlock()

	out := r.external(patch, width)
	if out == "" {
		out = diffview.Render(diffview.Options{Files: diffview.Parse(patch), Width: width})
	}
	r.mu.Lock()
	r.cache[key] = out
	r.mu.Unlock()
	return out
}

func (r *diffRenderer) external(patch string, width int) string {
	if r.provider == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.provider, "render", "-color", "always", "-width", strconv.Itoa(width))
	cmd.Env = append(os.Environ(), "TRACES_DIFF_WIDTH="+strconv.Itoa(width), "TRACES_DIFF_COLOR=always")
	cmd.Stdin = strings.NewReader(patch)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if cmd.Run() != nil {
		return ""
	}
	return strings.TrimSpace(stdout.String())
}
