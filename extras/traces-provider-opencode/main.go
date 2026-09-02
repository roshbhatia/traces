// traces-provider-opencode exports OpenCode activity through the Traces protocol.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/roshbhatia/traces/extras/internal/opencode"
	"github.com/roshbhatia/traces/internal/source"
)

func main() {
	since := flag.Duration("since", 2*time.Hour, "activity window")
	session := flag.String("session", "", "session id or prefix")
	directory := flag.String("directory", os.Getenv("TRACES_DIRECTORY"), "workspace directory")
	flag.Parse()
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
