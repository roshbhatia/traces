// traces-worklog reduces one finished session to one line of a worklog. It is
// a plain command shipped from extras, not a provider: it answers no Traces
// action and reads no activity into the viewer. A harness's session-end hook
// runs it with the harness's payload on stdin.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/roshbhatia/go-utils/xdg"
	"github.com/roshbhatia/traces/extras/worklog/internal/worklog"
)

const usage = `traces-worklog: append one session-end record to the worklog

Usage:
  traces-worklog [flags] < event.json

Reads a session-end payload on stdin, with session_id, cwd, reason and
transcript_path, and appends one schema v2 JSON line. A session with no
repository and no prompt is not recorded, and neither is a resume, which
starts work rather than finishing it.

Exits 0 whether or not a record was written. A session-end hook must not fail
a session that has already ended.

Flags:
`

func main() {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	output := flag.String("output", defaultOutput(), "file the record is appended to")
	sessionsRoot := flag.String("sessions-root", "", "directory whose children are multi-repository sessions; a cwd under one is recorded as that session")
	ended := flag.String("now", "", "when the session ended, as RFC 3339; defaults to the wall clock")
	flag.Parse()

	now := time.Now()
	if *ended != "" {
		parsed, err := time.Parse(time.RFC3339, *ended)
		if err != nil {
			fmt.Fprintf(os.Stderr, "traces-worklog: --now: %v\n", err)
			os.Exit(2)
		}
		now = parsed
	}

	var ev worklog.Event
	if err := json.NewDecoder(os.Stdin).Decode(&ev); err != nil {
		return
	}
	record, ok := worklog.Build(ev, worklog.Options{SessionsRoot: *sessionsRoot}, now)
	if !ok {
		return
	}
	if err := worklog.Append(*output, record); err != nil {
		fmt.Fprintf(os.Stderr, "traces-worklog: %v\n", err)
	}
}

func defaultOutput() string {
	state, err := xdg.StateHome()
	if err != nil {
		return filepath.Join(".local", "state", "traces", "worklog.jsonl")
	}
	return filepath.Join(state, "traces", "worklog.jsonl")
}
