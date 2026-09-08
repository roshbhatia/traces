// traces-provider-antigravity exports Antigravity CLI activity through the
// Traces protocol.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/roshbhatia/traces/extras/antigravity/internal/trajectory"
	"github.com/roshbhatia/traces/internal/source"
)

func main() {
	action := flag.String("action", "activity", "provider action")
	since := flag.Duration("since", 2*time.Hour, "activity window")
	session := flag.String("session", "", "session id or prefix")
	exactSession := flag.String("exact-session", "", "exact session id, including archived sessions")
	directory := flag.String("directory", os.Getenv("TRACES_DIRECTORY"), "workspace directory")
	root := flag.String("root", "", "state directory, defaulting to the CLI's application data path")
	flag.Parse()

	state := trajectory.Root()
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
		fmt.Println(trajectory.Current(state, absolute(*directory)))
		return
	case "discover":
		for _, id := range trajectory.Discover(state, absolute(*directory)) {
			fmt.Println(id)
		}
		return
	}
	if *session != "" && *exactSession != "" {
		fmt.Fprintln(os.Stderr, "--session and --exact-session are mutually exclusive")
		os.Exit(1)
	}
	options := trajectory.Options{
		Window: *since, Session: *session, Directory: absolute(*directory),
	}
	if *exactSession != "" {
		options.Session, options.Exact = *exactSession, true
	}
	if err := source.Encode(os.Stdout, trajectory.Read(state, options)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// validate reports whether the state directory can be read. A directory that is
// not there is still ok: the CLI creates it on its first run, and a machine that
// has not made one has nothing wrong with it.
func validate(root string) error {
	status, message := "ok", root
	switch {
	case root == "":
		status, message = "failed", "no home directory for the state path"
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
		"kind": "path", "name": "antigravity-cli", "status": status, "message": message,
	}}}
	return json.NewEncoder(os.Stdout).Encode(report)
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
