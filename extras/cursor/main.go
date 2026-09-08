// traces-provider-cursor exports Cursor CLI activity through the Traces
// protocol.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/roshbhatia/go-utils/workspace"
	"github.com/roshbhatia/traces/extras/cursor/internal/transcript"
	"github.com/roshbhatia/traces/internal/source"
)

func main() {
	action := flag.String("action", "activity", "provider action")
	since := flag.Duration("since", 2*time.Hour, "activity window")
	session := flag.String("session", "", "session id or prefix")
	exactSession := flag.String("exact-session", "", "exact session id, including archived sessions")
	directory := flag.String("directory", os.Getenv("TRACES_DIRECTORY"), "workspace directory")
	root := flag.String("root", "", "state directory, defaulting to the CLI's projects path")
	flag.Parse()

	state := transcript.Root()
	if *root != "" {
		state = *root
	}
	switch *action {
	case "validate":
		if err := validate(state); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	case "current":
		// The CLI exports the conversation into every tool shell it runs, so
		// a provider started from inside one reads its own session off the
		// environment.
		fmt.Println(strings.TrimSpace(os.Getenv("CURSOR_CONVERSATION_ID")))
		return
	case "discover":
		for _, id := range discover(state, absolute(*directory)) {
			fmt.Println(id)
		}
		return
	}
	if *session != "" && *exactSession != "" {
		fmt.Fprintln(os.Stderr, "--session and --exact-session are mutually exclusive")
		os.Exit(1)
	}
	options := transcript.Options{
		Window: *since, Session: *session, Directory: absolute(*directory),
	}
	if *exactSession != "" {
		options.Session, options.Exact = *exactSession, true
	}
	if err := source.Encode(os.Stdout, transcript.Read(state, options)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// validate reports whether the projects directory can be read. A directory that
// is not there is still ok: the CLI creates it on its first run, and a machine
// that has not made one has nothing wrong with it.
func validate(root string) error {
	status, message := "ok", root
	switch {
	case root == "":
		status, message = "failed", "no home directory for the projects path"
	default:
		entries, err := os.ReadDir(root)
		switch {
		case os.IsNotExist(err):
			message = root + " does not exist yet"
		case err != nil:
			status, message = "failed", err.Error()
		default:
			message = fmt.Sprintf("%s holds %d entries", root, len(entries))
		}
	}
	report := map[string]any{"checks": []map[string]string{{
		"kind": "path", "name": "cursor-cli", "status": status, "message": message,
	}}}
	return json.NewEncoder(os.Stdout).Encode(report)
}

// discover walks from the directory up to the workspace root, because the CLI
// keys a project on the directory it was started in and a reader may be asking
// from a subdirectory of it.
func discover(root, directory string) []string {
	if directory == "" {
		return nil
	}
	stop := workspace.Root(directory)
	seen := map[string]bool{}
	var sessions []string
	for at := directory; ; at = filepath.Dir(at) {
		for _, id := range transcript.Discover(root, at) {
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

func absolute(directory string) string {
	if directory == "" {
		return ""
	}
	resolved, err := filepath.Abs(directory)
	if err != nil {
		return directory
	}
	return resolved
}
