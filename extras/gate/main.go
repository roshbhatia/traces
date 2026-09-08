// traces-provider-gate exports gate hook decisions through the Traces protocol.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/roshbhatia/traces/extras/gate/internal/decisions"
	"github.com/roshbhatia/traces/internal/source"
)

func main() {
	action := flag.String("action", "activity", "provider action")
	since := flag.Duration("since", 2*time.Hour, "activity window")
	session := flag.String("session", "", "session id or prefix")
	exactSession := flag.String("exact-session", "", "exact session id, including archived sessions")
	directory := flag.String("directory", os.Getenv("TRACES_DIRECTORY"), "workspace directory")
	file := flag.String("file", "", "gate decision log, defaulting to the XDG state path")
	all := flag.Bool("all", false, "include the decisions that changed nothing")
	flag.Parse()

	path := decisions.Path(*file)
	if *action == "validate" {
		if err := validate(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *session != "" && *exactSession != "" {
		fmt.Fprintln(os.Stderr, "--session and --exact-session are mutually exclusive")
		os.Exit(1)
	}
	options := decisions.Options{
		Window: *since, Session: *session, Directory: *directory, All: *all,
	}
	if *exactSession != "" {
		options.Session, options.Exact = *exactSession, true
	}
	if err := source.Encode(os.Stdout, decisions.Read(path, options)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// validate reports whether the log can be read. A log that is not there yet is
// still ok: gate writes it on its first hook call, and a machine that has not
// made one has nothing wrong with it.
func validate(path string) error {
	status, message := "ok", path
	switch {
	case path == "":
		status, message = "failed", "no state directory for the decision log"
	default:
		file, err := os.Open(path)
		switch {
		case os.IsNotExist(err):
			message = path + " does not exist yet"
		case err != nil:
			status, message = "failed", err.Error()
		default:
			_ = file.Close()
		}
	}
	report := map[string]any{"checks": []map[string]string{{
		"kind": "path", "name": "decisions.jsonl", "status": status, "message": message,
	}}}
	return json.NewEncoder(os.Stdout).Encode(report)
}
