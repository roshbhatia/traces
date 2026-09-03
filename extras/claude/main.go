// traces-provider-claude exports Claude activity through the Traces protocol.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/roshbhatia/go-utils/workspace"
	"github.com/roshbhatia/traces/extras/internal/transcript"
	"github.com/roshbhatia/traces/internal/source"
)

func main() {
	action := flag.String("action", "activity", "provider action")
	since := flag.Duration("since", 2*time.Hour, "activity window")
	session := flag.String("session", "", "session id or prefix")
	directory := flag.String("directory", os.Getenv("TRACES_DIRECTORY"), "workspace directory")
	flag.Parse()
	switch *action {
	case "current":
		fmt.Println(firstEnvironment("CLAUDE_CODE_SESSION_ID", "CLAUDE_SESSION_ID"))
		return
	case "discover":
		for _, id := range discover(*directory) {
			fmt.Println(id)
		}
		return
	}
	batch := transcript.Read(transcript.Root(), *since, *session)
	if err := source.Encode(os.Stdout, batch); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func firstEnvironment(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func discover(directory string) []string {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		absolute = directory
	}
	stop := workspace.Root(absolute)
	seen := map[string]bool{}
	var sessions []string
	for at := absolute; ; at = filepath.Dir(at) {
		for _, id := range sessionsIn(at) {
			if !seen[id] {
				seen[id] = true
				sessions = append(sessions, id)
			}
		}
		if at == stop || at == filepath.Dir(at) {
			break
		}
	}
	return sessions
}

func sessionsIn(directory string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	encoded := strings.Map(func(value rune) rune {
		if unicode.IsLetter(value) || unicode.IsDigit(value) {
			return value
		}
		return '-'
	}, directory)
	entries, err := os.ReadDir(filepath.Join(home, ".claude", "projects", encoded))
	if err != nil {
		return nil
	}
	type candidate struct {
		id string
		at time.Time
	}
	var found []candidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		if len(id) != len("00000000-0000-0000-0000-000000000000") {
			continue
		}
		info, _ := entry.Info()
		var at time.Time
		if info != nil {
			at = info.ModTime()
		}
		found = append(found, candidate{id: id, at: at})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].at.After(found[j].at) })
	out := make([]string, 0, len(found))
	for _, item := range found {
		out = append(out, item.id)
	}
	return out
}
