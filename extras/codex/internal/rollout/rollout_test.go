package rollout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"
)

func TestReadFileNormalizesCodexActivity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout-test.jsonl")
	patch := `@@ -1 +1 @@
-old
+new
`
	fixture := renderTestTemplate(t, "Codex rollout", `{"type":"session_meta","timestamp":"2026-08-25T01:00:00Z","payload":{"id":"session-1","cwd":"/work/one"}}
{"type":"turn_context","timestamp":"2026-08-25T01:00:00Z","payload":{"turn_id":"turn-1","cwd":"/work/one","model":"gpt-test"}}
{"type":"event_msg","timestamp":"2026-08-25T01:00:00Z","payload":{"type":"task_started","turn_id":"turn-1","trace_id":"trace-1","started_at":1787619600}}
{"type":"event_msg","timestamp":"2026-08-25T01:00:01Z","payload":{"type":"item_completed","thread_id":"session-1","turn_id":"turn-1","started_at_ms":1787619601000,"completed_at_ms":1787619601100,"item":{"type":"UserMessage","id":"user-1","content":[{"type":"Text","text":"fix it"}]}}}
{"type":"event_msg","timestamp":"2026-08-25T01:00:02Z","payload":{"type":"item_completed","thread_id":"session-1","turn_id":"turn-1","item":{"type":"AgentMessage","id":"message-1","phase":"commentary","content":[{"type":"Text","text":"checking"}]}}}
{"type":"event_msg","timestamp":"2026-08-25T01:00:03Z","payload":{"type":"item_completed","thread_id":"session-1","turn_id":"turn-1","started_at_ms":1787619603000,"completed_at_ms":1787619603400,"item":{"type":"CommandExecution","id":"command-1","command":["/bin/zsh","-lc","git status --short"],"cwd":"file:///work/two","stdout":" M file.go","stderr":"","exit_code":0}}}
{"type":"event_msg","timestamp":"2026-08-25T01:00:04Z","payload":{"type":"item_completed","thread_id":"session-1","turn_id":"turn-1","started_at_ms":1787619604000,"completed_at_ms":1787619604500,"item":{"type":"FileChange","id":"edit-1","status":"completed","changes":{"/work/two/file.go":{"type":"update","unified_diff":{{ json .Patch }}}}}}}
{"type":"event_msg","timestamp":"2026-08-25T01:00:05Z","payload":{"type":"task_complete","turn_id":"turn-1","started_at":1787619600,"completed_at":1787619605}}
`, struct{ Patch string }{Patch: patch})
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	batch := ReadFile(path)
	if got, want := len(batch.Spans), 4; got != want {
		t.Fatalf("spans = %d, want %d", got, want)
	}
	if got, want := len(batch.Records), 4; got != want {
		t.Fatalf("records = %d, want %d", got, want)
	}

	byID := map[string]int{}
	for index, span := range batch.Spans {
		byID[span.SpanID] = index
		if span.Session != "session-1" {
			t.Errorf("span %q session = %q", span.SpanID, span.Session)
		}
		if span.Attrs["traces.view"] != "activity" {
			t.Errorf("span %q has no activity view", span.SpanID)
		}
	}

	turn := batch.Spans[byID["turn-1"]]
	if turn.Name != "agent.turn" || turn.Attrs["model"] != "gpt-test" {
		t.Errorf("turn = %#v", turn)
	}
	message := batch.Spans[byID["message-1"]]
	if message.Start.Year() != 2026 || message.End != message.Start {
		t.Errorf("message timestamps = %s to %s", message.Start, message.End)
	}
	command := batch.Spans[byID["command-1"]]
	if command.ParentID != "turn-1" || command.Attrs["full_command"] != "git status --short" || command.Attrs["cwd"] != "/work/two" {
		t.Errorf("command = %#v", command)
	}
	if command.Attrs["traces.action"] != "shell" {
		t.Errorf("command action = %q", command.Attrs["traces.action"])
	}
	edit := batch.Spans[byID["edit-1"]]
	if edit.Attrs["files_changed"] != "1" || edit.Attrs["lines_added"] != "1" || edit.Attrs["lines_removed"] != "1" {
		t.Errorf("edit attrs = %#v", edit.Attrs)
	}
	if edit.Attrs["traces.action"] != "edit" {
		t.Errorf("edit action = %q", edit.Attrs["traces.action"])
	}
	if !strings.Contains(edit.Attrs["traces.patch"], "+++ /work/two/file.go") {
		t.Errorf("edit patch = %q", edit.Attrs["traces.patch"])
	}
	if batch.Records[0].Event != EventPrompt || batch.Records[0].Attrs["prompt"] != "fix it" {
		t.Errorf("prompt = %#v", batch.Records[0])
	}
}

func renderTestTemplate(t *testing.T, name, source string, data any) string {
	t.Helper()
	functions := template.FuncMap{"json": func(value string) (string, error) {
		encoded, err := json.Marshal(value)
		return string(encoded), err
	}}
	parsed, err := template.New(name).Funcs(functions).Option("missingkey=error").Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := parsed.Execute(&output, data); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestReadFilePreservesSubagentActor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout-subagent.jsonl")
	fixture := `{"type":"session_meta","timestamp":"2026-08-27T15:03:46Z","payload":{"session_id":"root-session","id":"child-session","cwd":"/work/one","thread_source":"subagent","agent_path":"/root/reviewer"}}
{"type":"session_meta","timestamp":"2026-08-27T15:03:46Z","payload":{"session_id":"root-session","id":"root-session","cwd":"/work/one","thread_source":"user"}}
{"type":"event_msg","timestamp":"2026-08-27T15:03:47Z","payload":{"type":"task_started","turn_id":"child-turn","trace_id":"trace-1","started_at":1787843027}}
{"type":"event_msg","timestamp":"2026-08-27T15:03:48Z","payload":{"type":"item_completed","thread_id":"child-session","turn_id":"child-turn","item":{"type":"CommandExecution","id":"command-1","command":["git","status"],"exit_code":0}}}
{"type":"event_msg","timestamp":"2026-08-27T15:03:49Z","payload":{"type":"item_completed","thread_id":"child-session","turn_id":"child-turn","item":{"type":"AgentMessage","id":"reply-1","content":[{"type":"Text","text":"child answer"}]}}}
`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	batch := ReadFile(path)
	for _, span := range batch.Spans {
		if span.Session != "root-session" {
			t.Errorf("span %q session = %q, want root-session", span.SpanID, span.Session)
		}
		if got := span.Attrs["agent.path"]; got != "main/reviewer" {
			t.Errorf("span %q agent.path = %q, want main/reviewer", span.SpanID, got)
		}
	}
}

func TestReadFileRecognizesSubagentActivity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout-parent.jsonl")
	fixture := `{"type":"session_meta","timestamp":"2026-08-27T15:03:46Z","payload":{"id":"root-session","cwd":"/work/one"}}
{"type":"event_msg","timestamp":"2026-08-27T15:03:46Z","payload":{"type":"task_started","turn_id":"root-turn","trace_id":"trace-1","started_at":1787843026}}
{"type":"event_msg","timestamp":"2026-08-27T15:03:47Z","payload":{"type":"item_completed","thread_id":"root-session","turn_id":"root-turn","item":{"type":"SubAgentActivity","id":"call-1","kind":"started","agent_thread_id":"child-session","agent_path":"/root/reviewer"}}}
`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	batch := ReadFile(path)
	if got, want := len(batch.Spans), 2; got != want {
		t.Fatalf("spans = %d, want %d", got, want)
	}
	activity := batch.Spans[0]
	if activity.SpanID != "call-1" {
		activity = batch.Spans[1]
	}
	if activity.Name != "agent.tool" || activity.Attrs["traces.action"] != "delegate" {
		t.Errorf("subagent activity = %#v", activity)
	}
	if got := activity.Attrs["subagent_type"]; got != "main/reviewer" {
		t.Errorf("subagent_type = %q, want main/reviewer", got)
	}
}

func TestReadFindsSubagentFileByRootSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-child-session.jsonl")
	fixture := `{"type":"session_meta","timestamp":"2026-08-27T15:03:46Z","payload":{"session_id":"root-session","id":"child-session","cwd":"/work/one","thread_source":"subagent","agent_path":"/root/reviewer"}}
{"type":"event_msg","timestamp":"2026-08-27T15:03:47Z","payload":{"type":"task_started","turn_id":"child-turn","trace_id":"trace-1","started_at":1787843027}}
{"type":"event_msg","timestamp":"2026-08-27T15:03:48Z","payload":{"type":"item_completed","thread_id":"child-session","turn_id":"child-turn","item":{"type":"CommandExecution","id":"command-1","command":["git","status"],"exit_code":0}}}
{"type":"event_msg","timestamp":"2026-08-27T15:03:49Z","payload":{"type":"item_completed","thread_id":"child-session","turn_id":"child-turn","item":{"type":"AgentMessage","id":"reply-1","content":[{"type":"Text","text":"child answer"}]}}}
`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	batch := Read(dir, time.Hour, "root-session")
	if got, want := len(batch.Spans), 3; got != want {
		t.Fatalf("spans = %d, want %d", got, want)
	}
	if root := ReadExact(dir, "root-session"); len(root.Spans) != 0 {
		t.Fatalf("exact root read included %d child spans", len(root.Spans))
	}
	child := ReadExact(dir, "child-session")
	if got, want := len(child.Spans), 3; got != want {
		t.Fatalf("exact child read returned %d spans, want %d", got, want)
	}
	for _, span := range child.Spans {
		if span.Session != "child-session" {
			t.Fatalf("exact child span session = %q", span.Session)
		}
	}
	foundReply := false
	for _, record := range child.Records {
		if record.Event == EventText && record.Body == "child answer" && record.Session == "child-session" {
			foundReply = true
		}
	}
	if !foundReply {
		t.Fatalf("exact child read omitted assistant reply: %#v", child.Records)
	}
}

func TestReadExactFindsArchivedSessionWithoutMatchingPrefixCollision(t *testing.T) {
	dir := t.TempDir()
	wanted := filepath.Join(dir, "rollout-wanted.jsonl")
	other := filepath.Join(dir, "rollout-wanted-extra.jsonl")
	for path, session := range map[string]string{wanted: "wanted", other: "wanted-extra"} {
		fixture := `{"type":"session_meta","timestamp":"2026-08-27T15:03:46Z","payload":{"id":"` + session + `","cwd":"/work"}}
{"type":"event_msg","timestamp":"2026-08-27T15:03:47Z","payload":{"type":"task_started","turn_id":"turn","trace_id":"trace","started_at":1787843027}}
{"type":"event_msg","timestamp":"2026-08-27T15:03:48Z","payload":{"type":"item_completed","thread_id":"` + session + `","turn_id":"turn","item":{"type":"AgentMessage","id":"message","content":[{"type":"Text","text":"done"}]}}}
`
		if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-30 * 24 * time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if got := Read(dir, time.Hour, "wanted"); len(got.Spans) != 0 {
		t.Fatalf("ordinary read returned %d archived spans", len(got.Spans))
	}
	batch := ReadExact(dir, "wanted")
	if len(batch.Spans) != 2 {
		t.Fatalf("exact archived read returned %d spans", len(batch.Spans))
	}
	for _, span := range batch.Spans {
		if span.Session != "wanted" {
			t.Fatalf("exact read included session %q", span.Session)
		}
	}
}
