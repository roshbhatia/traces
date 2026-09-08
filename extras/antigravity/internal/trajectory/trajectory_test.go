package trajectory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/roshbhatia/traces/internal/otlp"
)

const (
	firstID  = "11111111-1111-1111-1111-111111111111"
	secondID = "22222222-2222-2222-2222-222222222222"
)

const firstTranscript = `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-09-08T19:02:21Z","content":"<USER_REQUEST>\nlist the store\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\nThe current local time is: 2026-09-08T12:02:21-07:00.\n</ADDITIONAL_METADATA>"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-09-08T19:02:22Z","tool_calls":[{"name":"run_command","args":{"CommandLine":"ls -la /nix/store","Cwd":"/work/one","WaitMsBeforeAsync":2000,"toolSummary":"Store listing"}}]}
{"step_index":2,"source":"MODEL","type":"GENERIC","status":"DONE","created_at":"2026-09-08T19:02:25Z","content":"Created At: 2026-09-08T12:02:22-07:00\nThe command exited with code 2.\nOutput:\nno such directory"}
{"step_index":3,"source":"SYSTEM","type":"SYSTEM_MESSAGE","status":"DONE","created_at":"2026-09-08T19:02:26Z","content":"a system note"}
{"step_index":4,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-09-08T19:02:27Z","thinking":"weighing it","tool_calls":[{"name":"write_to_file","args":{"TargetFile":"/work/one/notes.md"}}]}
{"step_index":5,"source":"MODEL","type":"GENERIC","status":"DONE","created_at":"2026-09-08T19:02:28Z","content":"Created At: 2026-09-08T12:02:27-07:00\nEdited the file."}
{"step_index":6,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-09-08T19:02:29Z","content":"It is not there."}`

const secondTranscript = `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-09-08T19:30:00Z","content":"<USER_REQUEST>\nopen the file\n</USER_REQUEST>"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-09-08T19:30:01Z","tool_calls":[{"name":"view_file","args":{"AbsolutePath":"/work/two/main.go"}}]}`

// state writes a fake application data directory. Both conversations carry a
// workspace so the directory filter has something to disagree with.
func state(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "brain", firstID, ".system_generated", "logs", "transcript_full.jsonl"), firstTranscript)
	write(t, filepath.Join(root, "brain", secondID, ".system_generated", "logs", "transcript_full.jsonl"), secondTranscript)
	write(t, filepath.Join(root, "cache", "conversation_metadata.json"), `{
  "conversations": {
    "`+firstID+`": {"summary": {"Title": "Store Listing", "WorkspaceURIs": ["file:///work/one"]}},
    "`+secondID+`": {"summary": {"Title": "File Read", "WorkspaceURIs": ["file:///work/two"]}}
  }
}`)
	write(t, filepath.Join(root, "cache", "last_conversations.json"), `{"/work/one":"`+firstID+`"}`)
	return root
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sessions(spans []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range spans {
		conversation, _, _ := strings.Cut(id, "/")
		if !seen[conversation] {
			seen[conversation] = true
			out = append(out, conversation)
		}
	}
	slices.Sort(out)
	return out
}

func spanIDs(root string, options Options) []string {
	batch := Read(root, options)
	out := make([]string, 0, len(batch.Spans))
	for _, span := range batch.Spans {
		out = append(out, span.SpanID)
	}
	return out
}

func TestReadSelectsConversations(t *testing.T) {
	root := state(t)
	tests := []struct {
		name    string
		options Options
		want    []string
	}{
		{
			name:    "every conversation inside the window",
			options: Options{Window: time.Hour},
			want:    []string{firstID, secondID},
		},
		{
			name:    "a session prefix",
			options: Options{Window: time.Hour, Session: "1111"},
			want:    []string{firstID},
		},
		{
			name:    "an exact session ignores the window",
			options: Options{Session: secondID, Exact: true},
			want:    []string{secondID},
		},
		{
			name:    "an exact session rejects a prefix",
			options: Options{Session: "2222", Exact: true},
			want:    []string{},
		},
		{
			name:    "a directory filter",
			options: Options{Window: time.Hour, Directory: "/work/two"},
			want:    []string{secondID},
		},
		{
			name:    "a directory nothing ran in",
			options: Options{Window: time.Hour, Directory: "/work/three"},
			want:    []string{},
		},
		{
			name:    "a window older than every transcript",
			options: Options{Window: time.Nanosecond},
			want:    []string{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sessions(spanIDs(root, test.options)); !slices.Equal(got, test.want) {
				t.Errorf("read %v, want %v", got, test.want)
			}
		})
	}
}

func TestReadSkipsMalformedLinesRatherThanFailingTheFile(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "brain", firstID, ".system_generated", "logs", "transcript_full.jsonl"),
		"{not json at all\n"+firstTranscript+"\n{\"step_index\":9,\n")
	batch := Read(root, Options{Window: time.Hour})
	if len(batch.Spans) == 0 {
		t.Fatal("a broken line took the whole transcript down")
	}
	if got := sessions(spanIDs(root, Options{Window: time.Hour})); !slices.Equal(got, []string{firstID}) {
		t.Errorf("read %v", got)
	}
}

func TestReadReportsNothingWithoutAStateDirectory(t *testing.T) {
	for _, root := range []string{"", filepath.Join(t.TempDir(), "absent")} {
		batch := Read(root, Options{Window: time.Hour})
		if !batch.Empty() {
			t.Errorf("root %q returned %d spans", root, len(batch.Spans))
		}
		if Current(root, "/work/one") != "" || len(Discover(root, "")) != 0 {
			t.Errorf("root %q answered a session question", root)
		}
	}
}

func TestReadBuildsTheTurnTree(t *testing.T) {
	batch := Read(state(t), Options{Window: time.Hour, Session: firstID})
	spans := map[string]struct {
		name   string
		parent string
		failed bool
		attrs  map[string]string
	}{}
	for _, span := range batch.Spans {
		if span.Service != Service || span.Session != firstID {
			t.Fatalf("span %s keyed %s/%s", span.SpanID, span.Service, span.Session)
		}
		spans[span.SpanID] = struct {
			name   string
			parent string
			failed bool
			attrs  map[string]string
		}{span.Name, span.ParentID, span.Failed, span.Attrs}
	}

	turn := firstID + "/step-0"
	if spans[turn].name != "agent.turn" {
		t.Fatalf("step 0 is %q", spans[turn].name)
	}
	if got := spans[turn].attrs["user_prompt"]; got != "list the store" {
		t.Errorf("prompt = %q, want the request without its metadata", got)
	}
	if got := spans[turn].attrs["session.title"]; got != "Store Listing" {
		t.Errorf("title = %q", got)
	}

	// A reply that only asked for a tool is not a row, so its tool hangs off
	// the turn rather than under an empty model call.
	tool := firstID + "/step-1/tool-0"
	if spans[tool].name != "agent.tool" || spans[tool].parent != turn {
		t.Errorf("tool is %q under %q, want agent.tool under the turn", spans[tool].name, spans[tool].parent)
	}
	if got := spans[tool].attrs["full_command"]; got != "ls -la /nix/store" {
		t.Errorf("full_command = %q", got)
	}
	if got := spans[tool].attrs["traces.action"]; got != "shell" {
		t.Errorf("action = %q", got)
	}
	if got := spans[tool].attrs["exit_code"]; got != "2" || !spans[tool].failed {
		t.Errorf("exit_code = %q, failed = %v", got, spans[tool].failed)
	}

	if got := spans[firstID+"/step-3"].name; got != "agent.note" {
		t.Errorf("system message is %q", got)
	}
	// A reply that reasoned out loud is its own row, and its tools hang under
	// it rather than beside it on the turn.
	model := firstID + "/step-4"
	if got := spans[model]; got.name != "agent.model" || got.parent != turn {
		t.Errorf("step 4 is %q under %q", got.name, got.parent)
	}
	edit := firstID + "/step-4/tool-0"
	if got := spans[edit]; got.name != "agent.edit" || got.parent != model {
		t.Errorf("step 4 tool is %q under %q, want agent.edit under the model call", got.name, got.parent)
	}
	if got := spans[edit].attrs["file_path"]; got != "notes.md" {
		t.Errorf("file_path = %q, want it relative to the workspace", got)
	}
	if got := spans[firstID+"/step-6"].name; got != "agent.model" {
		t.Errorf("step 6 is %q", got)
	}
}

func TestReadPairsToolOutputWithTheCallThatAskedForIt(t *testing.T) {
	batch := Read(state(t), Options{Window: time.Hour, Session: firstID})
	events := map[string]int{}
	results := map[string]otlp.Record{}
	for _, record := range batch.Records {
		events[record.Event]++
		if record.Event == EventResult {
			results[record.Attrs["tool_use_id"]] = record
		}
	}
	if events[EventPrompt] != 1 || events[EventResult] != 2 || events[EventText] != 2 {
		t.Fatalf("events = %v", events)
	}
	failed := results[firstID+"/step-1/tool-0"]
	if failed.Attrs["is_error"] != "true" {
		t.Errorf("a nonzero exit read as success")
	}
	if !strings.Contains(failed.Body, "no such directory") {
		t.Errorf("result body = %q", failed.Body)
	}
	// The second output belongs to the second call, not to the first one still
	// waiting when it arrived.
	if got := results[firstID+"/step-4/tool-0"]; got.Attrs["is_error"] != "" {
		t.Errorf("second result = %+v", got.Attrs)
	}
}

func TestReadUnwrapsTheClippedTranscriptArguments(t *testing.T) {
	// transcript.jsonl re-encodes every argument as a JSON string holding a
	// JSON value, and it is read only when the full file is absent.
	root := t.TempDir()
	write(t, filepath.Join(root, "brain", firstID, ".system_generated", "logs", "transcript.jsonl"),
		`{"step_index":0,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-09-08T19:02:22Z","truncated_fields":["content"],"tool_calls":[{"name":"run_command","args":{"CommandLine":"\"ls -la\"","WaitMsBeforeAsync":"2000"}}]}`)
	batch := Read(root, Options{Window: time.Hour})
	if len(batch.Spans) != 2 {
		t.Fatalf("spans = %d, want a stand-in turn and its tool", len(batch.Spans))
	}
	for _, span := range batch.Spans {
		if span.Name != "agent.tool" {
			continue
		}
		if got := span.Attrs["full_command"]; got != "ls -la" {
			t.Fatalf("full_command = %q, want the unwrapped command", got)
		}
		return
	}
	t.Fatal("no tool span")
}

func TestTranscriptPathPrefersTheUntruncatedFile(t *testing.T) {
	root := t.TempDir()
	logs := filepath.Join(root, "brain", firstID, ".system_generated", "logs")
	write(t, filepath.Join(logs, "transcript.jsonl"), "")
	if got := TranscriptPath(root, firstID); got != filepath.Join(logs, "transcript.jsonl") {
		t.Fatalf("path = %q, want the clipped file as the only one present", got)
	}
	write(t, filepath.Join(logs, "transcript_full.jsonl"), "")
	if got := TranscriptPath(root, firstID); got != filepath.Join(logs, "transcript_full.jsonl") {
		t.Fatalf("path = %q, want the full file", got)
	}
	if got := TranscriptPath(root, secondID); got != "" {
		t.Fatalf("path = %q for a conversation with no transcript", got)
	}
}

func TestCurrentAndDiscoverBindConversationsToAWorkspace(t *testing.T) {
	root := state(t)
	if got := Current(root, "/work/one"); got != firstID {
		t.Errorf("current = %q", got)
	}
	if got := Current(root, "/work/two"); got != "" {
		t.Errorf("current = %q for a workspace the cache does not name", got)
	}
	if got := Discover(root, "/work/two"); !slices.Equal(got, []string{secondID}) {
		t.Errorf("discover = %v", got)
	}
	if got := Discover(root, "/work/three"); len(got) != 0 {
		t.Errorf("discover = %v for a workspace nothing ran in", got)
	}
	if got := Discover(root, ""); len(got) != 2 {
		t.Errorf("discover = %v with no workspace", got)
	}
}

func TestSummariesToleratesABrokenCache(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "brain", firstID, ".system_generated", "logs", "transcript_full.jsonl"), firstTranscript)
	write(t, filepath.Join(root, "cache", "conversation_metadata.json"), "{ half written")
	if got := Summaries(root); len(got) != 0 {
		t.Fatalf("summaries = %v", got)
	}
	// A conversation the cache says nothing about is still activity, so a
	// directory filter must not hide it.
	if got := sessions(spanIDs(root, Options{Window: time.Hour, Directory: "/work/one"})); !slices.Equal(got, []string{firstID}) {
		t.Errorf("read %v", got)
	}
}

func TestSummaryDirectoryReducesTheFileURI(t *testing.T) {
	var one Summary
	if err := json.Unmarshal([]byte(`{"Title":"t","WorkspaceURIs":["file:///work/one"]}`), &one); err != nil {
		t.Fatal(err)
	}
	if got := one.Directory(); got != "/work/one" {
		t.Fatalf("directory = %q", got)
	}
	if got := (Summary{}).Directory(); got != "" {
		t.Fatalf("directory = %q with no workspace", got)
	}
}

func TestActionOfNamesTheGenericActions(t *testing.T) {
	tests := map[string]string{
		"run_command":     "shell",
		"view_file":       "read",
		"grep_search":     "search",
		"search_web":      "search",
		"write_to_file":   "edit",
		"invoke_subagent": "delegate",
		"manage_task":     "plan",
		"browser_click":   "browse",
		"mcp0_something":  "",
	}
	for tool, want := range tests {
		if got := actionOf(tool); got != want {
			t.Errorf("actionOf(%q) = %q, want %q", tool, got, want)
		}
	}
}
