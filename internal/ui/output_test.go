package ui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

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

func TestPrintMessagesJSONLWritesPlainVersionedRecords(t *testing.T) {
	messages := []session.Message{
		{ID: "msg_one", Session: "native-session", At: time.Date(2026, 9, 6, 1, 2, 3, 4, time.FixedZone("offset", 3600)), Text: "\x1b[32mfirst\x1b[0m\x1b]52;c;clipboard\a\b"},
		{ID: "msg_two", Session: "native-session", At: time.Date(2026, 9, 6, 2, 3, 4, 5, time.UTC), Text: "second\nline"},
	}
	var output bytes.Buffer
	if err := PrintMessagesJSONL(&output, messages); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(output.String(), '\x1b') || strings.ContainsRune(output.String(), '\b') {
		t.Fatalf("JSONL contains terminal controls: %q", output.String())
	}
	for index, value := range output.Bytes() {
		if value < 0x20 && value != '\n' {
			t.Fatalf("JSONL byte %d is control %#x", index, value)
		}
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("JSONL lines = %q", lines)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first["version"] != "traces.message/v1" || first["id"] != "msg_one" || first["session"] != "native-session" {
		t.Fatalf("first record = %#v", first)
	}
	if first["timestamp"] != "2026-09-06T00:02:03.000000004Z" || first["body"] != "first" {
		t.Fatalf("first record = %#v", first)
	}
}

func TestPrintMessagesJSONLBoundsCompleteNewestRecords(t *testing.T) {
	messages := []session.Message{
		{ID: "old", Session: "native", At: time.Unix(1, 0), Text: "old"},
		{ID: "new", Session: "native", At: time.Unix(2, 0), Text: "beginning-" + strings.Repeat("x", MessageOutputLimit) + "-ending"},
	}
	var output bytes.Buffer
	if err := PrintMessagesJSONL(&output, messages); err != nil {
		t.Fatal(err)
	}
	if output.Len() > MessageOutputLimit {
		t.Fatalf("output size = %d", output.Len())
	}
	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("JSONL contains partial or older records: %d lines", len(lines))
	}
	var got struct {
		ID        string `json:"id"`
		Body      string `json:"body"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("invalid bounded JSONL: %v", err)
	}
	if got.ID != "new" || !got.Truncated || strings.Contains(got.Body, "beginning-") || !strings.HasSuffix(got.Body, "-ending") {
		t.Fatalf("bounded record = %#v", got)
	}
}
