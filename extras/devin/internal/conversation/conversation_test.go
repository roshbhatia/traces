package conversation

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/roshbhatia/traces/internal/otlp"
)

func stamp(s string) time.Time {
	at, _ := time.Parse(time.RFC3339Nano, s)
	return at
}

func yes() *bool { b := true; return &b }
func no() *bool  { b := false; return &b }

// node builds one message node. parent < 0 marks a root.
func mknode(id, parent int64, m message) nodeRow {
	row := nodeRow{NodeID: id, Message: m}
	if parent >= 0 {
		row.Parent, row.HasPar = parent, true
	}
	return row
}

func spanByName(spans []otlp.Span, name string) []otlp.Span {
	var out []otlp.Span
	for _, one := range spans {
		if one.Name == name {
			out = append(out, one)
		}
	}
	return out
}

func recordByEvent(records []otlp.Record, event string) []otlp.Record {
	var out []otlp.Record
	for _, one := range records {
		if one.Event == event {
			out = append(out, one)
		}
	}
	return out
}

// exec-tool session shaped like the CLI writes: a prompt, a reply that only
// asked for a tool, the tool output, then the reply that answered.
func execWalk() (sessionRow, []nodeRow) {
	session := sessionRow{
		ID: "puzzle-marigold", Model: "gemini-3-8-flash-low",
		Directory: "/work/one", Title: "Run echo", LastActivity: 1788905229,
		MainChain: 3, HasMainChain: true,
	}
	walk := []nodeRow{
		mknode(0, -1, message{Role: "system", Content: "You are Devin",
			Metadata: metadata{CreatedAt: "2026-09-08T22:07:00Z"}}),
		mknode(1, 0, message{Role: "user", Content: "Run echo hello",
			Metadata: metadata{IsUserInput: yes(), CreatedAt: "2026-09-08T22:07:03Z"}}),
		mknode(2, 1, message{Role: "assistant", Content: "",
			ToolCalls: []toolCall{{ID: "call_1", Name: "exec", Index: 0,
				Arguments: map[string]json.RawMessage{"command": json.RawMessage(`"echo hello"`)}}},
			Metadata: metadata{RequestID: "req-1", FinishReason: "tool_calls",
				CreatedAt: "2026-09-08T22:07:07Z"}}),
		mknode(3, 2, message{Role: "tool", Content: "Output from command in shell 59f39e:\nhello\n\nExit code: 0",
			ToolCallID: "call_1", Metadata: metadata{CreatedAt: "2026-09-08T22:07:07.3Z"}}),
		mknode(4, 3, message{Role: "assistant", Content: "hello",
			Metadata: metadata{RequestID: "req-2", CreatedAt: "2026-09-08T22:07:09Z"}}),
	}
	return session, walk
}

func TestChainWalksMainLeafInTimeOrder(t *testing.T) {
	session, all := execWalk()
	// Add an abandoned branch the main chain must not follow.
	all = append(all, mknode(9, 1, message{Role: "assistant", Content: "stale",
		Metadata: metadata{CreatedAt: "2026-09-08T22:07:06Z"}}))
	session.MainChain = 4
	got := chain(all, session)
	want := []int64{0, 1, 2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("chain length %d, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].NodeID != id {
			t.Errorf("chain[%d] = %d, want %d", i, got[i].NodeID, id)
		}
	}
}

func TestChainFallsBackToTopNode(t *testing.T) {
	session, all := execWalk()
	session.MainChain, session.HasMainChain = 0, false
	got := chain(all, session)
	if len(got) == 0 || got[len(got)-1].NodeID != 4 {
		t.Fatalf("fallback chain leaf = %v, want 4", got)
	}
}

func TestBuildRendersTurnToolAndReply(t *testing.T) {
	session, walk := execWalk()
	batch := build(session, walk)

	turns := spanByName(batch.Spans, "agent.turn")
	if len(turns) != 1 {
		t.Fatalf("turn spans = %d, want 1", len(turns))
	}
	if got := turns[0].Attrs["user_prompt"]; got != "Run echo hello" {
		t.Errorf("turn prompt = %q", got)
	}

	tools := spanByName(batch.Spans, "agent.tool")
	if len(tools) != 1 {
		t.Fatalf("tool spans = %d, want 1", len(tools))
	}
	tool := tools[0]
	if tool.ParentID != turns[0].SpanID {
		t.Errorf("tool parent = %q, want turn %q", tool.ParentID, turns[0].SpanID)
	}
	if tool.Attrs["tool_name"] != "exec" || tool.Attrs["traces.action"] != "shell" {
		t.Errorf("tool attrs = %v", tool.Attrs)
	}
	if tool.Attrs["full_command"] != "echo hello" {
		t.Errorf("full_command = %q", tool.Attrs["full_command"])
	}
	if tool.Attrs["exit_code"] != "0" || tool.Failed {
		t.Errorf("exit=%q failed=%v, want 0/false", tool.Attrs["exit_code"], tool.Failed)
	}

	models := spanByName(batch.Spans, "agent.model")
	if len(models) != 1 {
		t.Fatalf("model spans = %d, want 1", len(models))
	}

	if len(recordByEvent(batch.Records, EventPrompt)) != 1 {
		t.Errorf("prompt records = %d", len(recordByEvent(batch.Records, EventPrompt)))
	}
	results := recordByEvent(batch.Records, EventResult)
	if len(results) != 1 || results[0].Attrs["tool_use_id"] != tool.SpanID {
		t.Errorf("tool_result records = %v, want linked to %q", results, tool.SpanID)
	}
	text := recordByEvent(batch.Records, EventText)
	if len(text) != 1 || text[0].Body != "hello" {
		t.Errorf("assistant records = %v", text)
	}
}

func TestBuildMarksFailedTool(t *testing.T) {
	session, walk := execWalk()
	walk[3].Message.Content = "Output from command\nExit code: 2"
	batch := build(session, walk)
	tools := spanByName(batch.Spans, "agent.tool")
	if len(tools) != 1 || !tools[0].Failed || tools[0].Attrs["exit_code"] != "2" {
		t.Fatalf("failed tool = %+v", tools)
	}
	results := recordByEvent(batch.Records, EventResult)
	if len(results) != 1 || results[0].Attrs["is_error"] != "true" {
		t.Errorf("tool_result is_error = %v", results)
	}
}

func TestBuildSkipsSystemAndSynthesizedUser(t *testing.T) {
	session, walk := execWalk()
	// A user role the CLI threaded back is not a turn.
	walk = append(walk, mknode(5, 4, message{Role: "user", Content: "synthetic",
		Metadata: metadata{IsUserInput: no(), CreatedAt: "2026-09-08T22:07:10Z"}}))
	batch := build(session, walk)
	if got := len(spanByName(batch.Spans, "agent.turn")); got != 1 {
		t.Errorf("turn spans = %d, want 1 (system and synthetic user skipped)", got)
	}
	for _, one := range batch.Records {
		if one.Body == "You are Devin" || one.Body == "synthetic" {
			t.Errorf("emitted a skipped node: %q", one.Body)
		}
	}
}

func TestBuildNamesEditToolAndFilePath(t *testing.T) {
	session, walk := execWalk()
	walk[2].Message.ToolCalls = []toolCall{{ID: "call_1", Name: "str_replace", Index: 0,
		Arguments: map[string]json.RawMessage{"path": json.RawMessage(`"/work/one/main.go"`)}}}
	batch := build(session, walk)
	edits := spanByName(batch.Spans, "agent.edit")
	if len(edits) != 1 {
		t.Fatalf("edit spans = %d, want 1", len(edits))
	}
	if edits[0].Attrs["file_path"] != "main.go" {
		t.Errorf("file_path = %q, want main.go (relative to cwd)", edits[0].Attrs["file_path"])
	}
	if edits[0].Attrs["traces.action"] != "edit" {
		t.Errorf("action = %q, want edit", edits[0].Attrs["traces.action"])
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		content string
		code    int
		ok      bool
	}{
		{"Exit code: 0", 0, true},
		{"stuff\nExit code: 137\nmore", 137, true},
		{"exit code: -1", -1, true},
		{"no code here", 0, false},
	}
	for _, test := range tests {
		code, ok := exitCode(test.content)
		if ok != test.ok || code != test.code {
			t.Errorf("exitCode(%q) = %d,%v want %d,%v", test.content, code, ok, test.code, test.ok)
		}
	}
}

func TestActionOf(t *testing.T) {
	tests := map[string]string{
		"exec":         "shell",
		"str_replace":  "edit",
		"read_file":    "read",
		"grep":         "search",
		"browser_open": "browse",
		"run_subagent": "delegate",
		"mystery_tool": "",
	}
	for tool, want := range tests {
		if got := actionOf(tool); got != want {
			t.Errorf("actionOf(%q) = %q, want %q", tool, got, want)
		}
	}
}

func TestText(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{`"echo hello"`, "echo hello"},
		{`null`, ""},
		{``, ""},
		{`42`, "42"},
	}
	for _, test := range tests {
		if got := text(json.RawMessage(test.raw)); got != test.want {
			t.Errorf("text(%q) = %q, want %q", test.raw, got, test.want)
		}
	}
}

func TestMatchesAndDirectory(t *testing.T) {
	if !matches("puzzle-marigold", "puzzle", false) {
		t.Error("prefix should match by substring")
	}
	if matches("puzzle-marigold", "puzzle", true) {
		t.Error("exact should not match a prefix")
	}
	if !matches("anything", "", false) {
		t.Error("empty session matches in non-exact")
	}
	if !inDirectory("", "/work/one") {
		t.Error("unknown directory is kept")
	}
	if inDirectory("/work/two", "/work/one") {
		t.Error("different directory is filtered")
	}
	if !sameDirectory("/work/one/", "/work/one") {
		t.Error("clean should equate trailing slash")
	}
}

// TestReadRoundTrip proves the sqlite3 reader against a fixture database. It is
// skipped where sqlite3 is not on PATH, so the pure tests above still run.
func TestReadRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}
	root := t.TempDir()
	db := dbPath(root)
	schema := `
CREATE TABLE sessions (id TEXT PRIMARY KEY, working_directory TEXT, model TEXT,
  last_activity_at INTEGER, title TEXT, main_chain_id INTEGER, hidden INTEGER DEFAULT 0);
CREATE TABLE message_nodes (session_id TEXT, node_id INTEGER, parent_node_id INTEGER, chat_message TEXT);
INSERT INTO sessions VALUES ('sess-a','/work/one','gemini-3-8-flash-low',` +
		`9999999999,'A run',2,0);
INSERT INTO message_nodes VALUES ('sess-a',0,NULL,'{"role":"user","content":"hi",` +
		`"metadata":{"is_user_input":true,"created_at":"2026-09-08T22:00:00Z"}}');
INSERT INTO message_nodes VALUES ('sess-a',1,0,'{"role":"assistant","content":"",` +
		`"tool_calls":[{"id":"c1","name":"exec","index":0,"arguments":{"command":"ls"}}],` +
		`"metadata":{"request_id":"r1","created_at":"2026-09-08T22:00:01Z"}}');
INSERT INTO message_nodes VALUES ('sess-a',2,1,'{"role":"assistant","content":"done",` +
		`"metadata":{"request_id":"r2","created_at":"2026-09-08T22:00:02Z"}}');`
	cmd := exec.Command("sqlite3", db)
	cmd.Stdin = strings.NewReader(schema)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seed db: %v: %s", err, out)
	}

	batch := Read(root, Options{Window: time.Hour, Session: "sess-a", Directory: "/work/one",
		Exact: true})
	if len(spanByName(batch.Spans, "agent.turn")) != 1 {
		t.Errorf("round trip turns = %d, want 1", len(spanByName(batch.Spans, "agent.turn")))
	}
	if len(spanByName(batch.Spans, "agent.tool")) != 1 {
		t.Errorf("round trip tools = %d, want 1", len(spanByName(batch.Spans, "agent.tool")))
	}
	if ids := Discover(root, "/work/one"); len(ids) != 1 || ids[0] != "sess-a" {
		t.Errorf("discover = %v, want [sess-a]", ids)
	}
	if got := Current(root, "/work/one"); got != "sess-a" {
		t.Errorf("current = %q, want sess-a", got)
	}
}
