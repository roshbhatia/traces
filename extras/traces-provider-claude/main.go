// traces-provider-claude exports Claude activity through the Traces protocol.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/roshbhatia/traces/extras/internal/transcript"
	"github.com/roshbhatia/traces/internal/source"
)

func main() {
	since := flag.Duration("since", 2*time.Hour, "activity window")
	session := flag.String("session", "", "session id or prefix")
	_ = flag.String("directory", "", "workspace directory")
	flag.Parse()
	batch := transcript.Read(transcript.Root(), *since, *session)
	if err := source.Encode(os.Stdout, batch); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
