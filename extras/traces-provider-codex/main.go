// traces-provider-codex exports Codex activity through the Traces protocol.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/roshbhatia/traces/extras/internal/rollout"
	"github.com/roshbhatia/traces/internal/source"
)

func main() {
	since := flag.Duration("since", 2*time.Hour, "activity window")
	session := flag.String("session", "", "session id or prefix")
	_ = flag.String("directory", "", "workspace directory")
	flag.Parse()
	batch := rollout.Read(rollout.Root(), *since, *session)
	if err := source.Encode(os.Stdout, batch); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
