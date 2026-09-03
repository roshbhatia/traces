// traces-provider-codex exports Codex activity through the Traces protocol.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/roshbhatia/traces/extras/internal/rollout"
	"github.com/roshbhatia/traces/internal/source"
)

func main() {
	action := flag.String("action", "activity", "provider action")
	since := flag.Duration("since", 2*time.Hour, "activity window")
	session := flag.String("session", "", "session id or prefix")
	_ = flag.String("directory", "", "workspace directory")
	flag.Parse()
	if *action == "current" {
		fmt.Println(firstEnvironment("CODEX_SESSION_ID", "CODEX_THREAD_ID"))
		return
	}
	batch := rollout.Read(rollout.Root(), *since, *session)
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
