package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/roshbhatia/traces/internal/session"
)

func TestPrintMessagesWritesOnlyBodiesAndHonorsColor(t *testing.T) {
	messages := []session.Message{
		{Text: "first"},
		{Text: "\x1b[32msecond\x1b[0m\x1b]52;c;clipboard\a\a"},
		{Text: "\x1b[31mthird\x1b[?25m\b"},
	}
	var colored bytes.Buffer
	PrintMessages(&colored, messages, "always")
	if got := colored.String(); got != "first\n\n\x1b[32msecond\x1b[0m\n\n\x1b[31mthird\x1b[0m\n" {
		t.Fatalf("colored output = %q", got)
	}
	var plain bytes.Buffer
	PrintMessages(&plain, messages, "never")
	if got := plain.String(); got != "first\n\nsecond\n\nthird\n" {
		t.Fatalf("plain output = %q", got)
	}
}

func TestPrintMessagesBoundsNewestOutput(t *testing.T) {
	messages := []session.Message{{Text: strings.Repeat("old", MessageOutputLimit)}, {Text: "newest"}}
	var output bytes.Buffer
	PrintMessages(&output, messages, "never")
	if output.Len() > MessageOutputLimit {
		t.Fatalf("output size = %d", output.Len())
	}
	if !strings.HasSuffix(output.String(), "newest\n") {
		t.Fatalf("output omitted newest message")
	}
	if !strings.HasPrefix(output.String(), "… earlier assistant output omitted …\n") {
		t.Fatalf("output omitted truncation marker")
	}
}

func TestPrintMessagesTailsOversizedNewestMessage(t *testing.T) {
	messages := []session.Message{{Text: "beginning-" + strings.Repeat("x", MessageOutputLimit) + "-ending"}}
	var output bytes.Buffer
	PrintMessages(&output, messages, "always")
	if output.Len() > MessageOutputLimit {
		t.Fatalf("output size = %d", output.Len())
	}
	if strings.Contains(output.String(), "beginning-") || !strings.HasSuffix(output.String(), "-ending\n") {
		t.Fatalf("output did not retain newest tail")
	}
}
