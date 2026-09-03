// traces-provider-opencode exports OpenCode activity through the Traces protocol.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/roshbhatia/traces/extras/opencode/internal/opencode"
	"github.com/roshbhatia/traces/internal/source"
)

func main() {
	action := flag.String("action", "activity", "provider action")
	since := flag.Duration("since", 2*time.Hour, "activity window")
	session := flag.String("session", "", "session id or prefix")
	directory := flag.String("directory", os.Getenv("TRACES_DIRECTORY"), "workspace directory")
	flag.Parse()
	if *action == "current" {
		fmt.Println(firstEnvironment("OPENCODE_SESSION_ID"))
		return
	}
	batch, err := opencode.Read(context.Background(), "opencode", *since, *session, *directory)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
