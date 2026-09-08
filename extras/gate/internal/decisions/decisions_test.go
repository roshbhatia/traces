package decisions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "decisions.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func line(t *testing.T, fields map[string]any) string {
	t.Helper()
	if _, ok := fields["ts"]; !ok {
		fields["ts"] = time.Now().Format(time.RFC3339Nano)
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestReadSelectsLines(t *testing.T) {
	old := time.Now().Add(-6 * time.Hour).Format(time.RFC3339Nano)
	deny := line(t, map[string]any{
		"harness": "claude", "event": "PreToolUse", "tool": "Read", "final": "deny",
		"session": "abcdef-1234", "cwd": "/work/one",
		"fields": map[string]string{"orc_session": "orc-99"},
	})
	block := line(t, map[string]any{
		"harness": "claude", "event": "Stop", "final": "block", "session": "other-7",
		"cwd": "/work/two",
	})
	pass := line(t, map[string]any{
		"harness": "claude", "event": "PreToolUse", "tool": "Bash", "final": "pass",
		"session": "abcdef-1234", "cwd": "/work/one",
	})
	stale := line(t, map[string]any{
		"ts": old, "harness": "claude", "event": "PreToolUse", "tool": "Edit",
		"final": "deny", "session": "abcdef-1234", "cwd": "/work/one",
	})
	path := write(t, deny, block, pass, stale, "", "{ not json", `{"final":`)

	tests := []struct {
		name    string
		options Options
		want    []string
	}{
		{
			name:    "window omits the older decision",
			options: Options{Window: time.Hour},
			want:    []string{"PreToolUse Read -> deny", "Stop -> block"},
		},
		{
			name:    "all keeps the pass",
			options: Options{Window: time.Hour, All: true},
			want: []string{
				"PreToolUse Read -> deny", "Stop -> block", "PreToolUse Bash -> pass",
			},
		},
		{
			name:    "session prefix matches the harness identity",
			options: Options{Window: time.Hour, Session: "abcdef"},
			want:    []string{"PreToolUse Read -> deny"},
		},
		{
			name:    "session matches the orc identity",
			options: Options{Window: time.Hour, Session: "orc-99"},
			want:    []string{"PreToolUse Read -> deny"},
		},
		{
			name:    "exact session ignores the window",
			options: Options{Window: time.Hour, Session: "abcdef-1234", Exact: true},
			want:    []string{"PreToolUse Read -> deny", "PreToolUse Edit -> deny"},
		},
		{
			name:    "exact session rejects a prefix",
			options: Options{Window: time.Hour, Session: "abcdef", Exact: true},
			want:    nil,
		},
		{
			name:    "directory filters on cwd",
			options: Options{Window: time.Hour, Directory: "/work/two"},
			want:    []string{"Stop -> block"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := []string{}
			for _, span := range Read(path, test.options).Spans {
				got = append(got, span.Attrs["note.kind"])
			}
			if len(got) != len(test.want) {
				t.Fatalf("read %v, want %v", got, test.want)
			}
			for _, want := range test.want {
				if !contains(got, want) {
					t.Errorf("read %v, want %q", got, want)
				}
			}
		})
	}
}

func TestReadMissingLogIsEmpty(t *testing.T) {
	for _, path := range []string{"", filepath.Join(t.TempDir(), "absent.jsonl")} {
		if batch := Read(path, Options{Window: time.Hour}); !batch.Empty() {
			t.Errorf("path %q read %d spans", path, len(batch.Spans))
		}
	}
}

func TestSpanCarriesTheDecision(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Millisecond)
	path := write(t, line(t, map[string]any{
		"ts": at.Format(time.RFC3339Nano), "harness": "claude", "event": "PreToolUse",
		"tool": "Read", "final": "deny", "session": "run-1", "cwd": "/work", "ms": 12,
		"agent": "reviewer",
		"decisions": []map[string]any{
			{"provider": "quiet", "kind": "pass", "ms": 1},
			{"provider": "secrets", "kind": "deny", "ms": 4, "message": "reads a key file"},
		},
	}))
	spans := Read(path, Options{Window: time.Hour}).Spans
	if len(spans) != 1 {
		t.Fatalf("read %d spans", len(spans))
	}
	span := spans[0]
	if want := "PreToolUse Read -> deny"; span.Attrs["note.kind"] != want {
		t.Errorf("note.kind %q, want %q", span.Attrs["note.kind"], want)
	}
	if want := "reads a key file"; span.Attrs["note.text"] != want {
		t.Errorf("note.text %q, want %q", span.Attrs["note.text"], want)
	}
	if span.Error != span.Attrs["note.text"] {
		t.Errorf("error %q, want the note text", span.Error)
	}
	if !span.Failed {
		t.Error("a deny must mark the span failed")
	}
	// The service is the harness reader's own, so the decision joins that
	// session rather than opening one beside it.
	if span.Service != "claude-code" || span.Session != "run-1" {
		t.Errorf("keyed %s/%s, want claude-code/run-1", span.Service, span.Session)
	}
	if span.Name != Name || span.Attrs["traces.view"] != "activity" {
		t.Errorf("span %q view %q", span.Name, span.Attrs["traces.view"])
	}
	if got := span.End.Sub(span.Start); got != 12*time.Millisecond {
		t.Errorf("duration %s, want 12ms", got)
	}
	for key, want := range map[string]string{
		"gate.provider": "secrets", "tool_name": "Read", "agent.name": "reviewer",
		"note.level": "error", "cwd": "/work",
	} {
		if span.Attrs[key] != want {
			t.Errorf("attr %s is %q, want %q", key, span.Attrs[key], want)
		}
	}
}

func TestSpanIDIsStablePerLine(t *testing.T) {
	path := write(t, line(t, map[string]any{
		"harness": "claude", "event": "Stop", "final": "block", "session": "run-1",
	}))
	first := Read(path, Options{Window: time.Hour}).Spans
	second := Read(path, Options{Window: time.Hour}).Spans
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("read %d then %d spans", len(first), len(second))
	}
	if first[0].SpanID != second[0].SpanID {
		t.Errorf("span id %q then %q", first[0].SpanID, second[0].SpanID)
	}
	if first[0].TraceID != "run-1" {
		t.Errorf("trace id %q, want run-1", first[0].TraceID)
	}
}

func TestPathFallsBackToStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	if got := Path(""); got != filepath.Join("/state", "gate", "decisions.jsonl") {
		t.Errorf("path %q", got)
	}
	if got := Path("/elsewhere/log.jsonl"); got != "/elsewhere/log.jsonl" {
		t.Errorf("override %q", got)
	}
}

func contains(values []string, want string) bool {
	for _, one := range values {
		if one == want {
			return true
		}
	}
	return false
}
