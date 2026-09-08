package transcript

import (
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

// firstTranscript is the shape the CLI wrote on 2026-09-08: a reply that only
// asked for a tool, a reply with reasoning folded ahead of its tool, and a
// closing status line.
const firstTranscript = `{"role":"user","message":{"content":[{"type":"text","text":"<timestamp>Tuesday, Sep 8, 2026, 1:56 PM (UTC-7)</timestamp>\n<user_query>\nread probe.txt then run the check\n</user_query>"}]}}
{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Shell","input":{"command":"printf ok; exit 3","description":"Print ok and exit with code 3"}}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"**Clarifying File Operations**\n\nI am deciding how to edit it.\n\n"},{"type":"tool_use","name":"StrReplace","input":{"new_string":"WORLD","old_string":"HELLO","path":"/work/one/probe.txt"}}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"Done."}]}}
{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"path":"/work/one","pattern":"HELLO"}}]}}
{"type":"turn_ended","status":"success"}
{"role":"user","message":{"content":[{"type":"text","text":"<timestamp>Tuesday, Sep 8, 2026, 2:10 PM (UTC-7)</timestamp>\n<user_query>\nnow delete it\n</user_query>"}]}}
{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Delete","input":{"path":"/work/one/probe.txt"}}]}}
{"type":"turn_ended","status":"error","error":"the model went away"}`

const secondTranscript = `{"role":"user","message":{"content":[{"type":"text","text":"<timestamp>Tuesday, Sep 8, 2026, 1:22 PM (UTC-7)</timestamp>\n<user_query>\nsay PONG\n</user_query>"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"PONG"}]}}
{"type":"turn_ended","status":"success"}`

// state writes a fake projects directory. Both workspaces carry a trust record
// so the directory filter has something to disagree with, and the second slug
// is the one the CLI would derive from /work/two.
func state(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, TranscriptPath(root, "work-one", firstID), firstTranscript)
	write(t, filepath.Join(root, "work-one", ".workspace-trusted"),
		`{"trustedAt":"2026-09-08T20:18:56.970Z","workspacePath":"/work/one","trustMethod":"cli-flag"}`)
	write(t, TranscriptPath(root, "work-two", secondID), secondTranscript)
	write(t, filepath.Join(root, "work-two", ".workspace-trusted"),
		`{"trustedAt":"2026-09-08T20:18:56.970Z","workspacePath":"/work/two","trustMethod":"cli-flag"}`)
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

func TestSlugFoldsThePathTheWayTheCLIDoes(t *testing.T) {
	tests := map[string]string{
		"/work/one": "work-one",
		"/private/tmp/claude-503/-Users-roshan-github-personal-roshbhatia-sysinit/scratch": "private-tmp-claude-503-Users-roshan-github-personal-roshbhatia-sysinit-scratch",
		"/Users/roshan/github/personal/roshbhatia/neph.nvim":                               "Users-roshan-github-personal-roshbhatia-neph-nvim",
		"/a b/c_d/": "a-b-c-d",
		"relative":  "relative",
		"":          "",
		"///":       "",
	}
	for directory, want := range tests {
		if got := Slug(directory); got != want {
			t.Errorf("Slug(%q) = %q, want %q", directory, got, want)
		}
	}
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
			name:    "a directory filter by slug",
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

func TestReadMatchesADirectoryByItsTrustRecord(t *testing.T) {
	// A project whose slug was derived from another spelling of the path is
	// still the same workspace when its trust record names the directory.
	root := t.TempDir()
	write(t, TranscriptPath(root, "some-other-slug", firstID), firstTranscript)
	write(t, filepath.Join(root, "some-other-slug", ".workspace-trusted"), `{"workspacePath":"/work/one/"}`)
	if got := sessions(spanIDs(root, Options{Window: time.Hour, Directory: "/work/one"})); !slices.Equal(got, []string{firstID}) {
		t.Errorf("read %v", got)
	}
}

func TestReadSkipsMalformedLinesRatherThanFailingTheFile(t *testing.T) {
	root := t.TempDir()
	write(t, TranscriptPath(root, "work-one", firstID),
		"{not json at all\n"+firstTranscript+"\n{\"role\":\"assistant\",\n")
	batch := Read(root, Options{Window: time.Hour})
	if len(batch.Spans) == 0 {
		t.Fatal("a broken line took the whole transcript down")
	}
	if got := sessions(spanIDs(root, Options{Window: time.Hour})); !slices.Equal(got, []string{firstID}) {
		t.Errorf("read %v", got)
	}
}

func TestReadReportsNothingWithoutAProjectsDirectory(t *testing.T) {
	for _, root := range []string{"", filepath.Join(t.TempDir(), "absent")} {
		batch := Read(root, Options{Window: time.Hour})
		if !batch.Empty() {
			t.Errorf("root %q returned %d spans", root, len(batch.Spans))
		}
		if len(Discover(root, "/work/one")) != 0 || len(Projects(root)) != 0 {
			t.Errorf("root %q answered a session question", root)
		}
	}
}

func TestReadBuildsTheTurnTree(t *testing.T) {
	batch := Read(state(t), Options{Window: time.Hour, Session: firstID})
	spans := map[string]otlp.Span{}
	for _, span := range batch.Spans {
		if span.Service != Service || span.Session != firstID {
			t.Fatalf("span %s keyed %s/%s", span.SpanID, span.Service, span.Session)
		}
		spans[span.SpanID] = span
	}

	turn := firstID + "/line-0"
	if spans[turn].Name != "agent.turn" {
		t.Fatalf("line 0 is %q", spans[turn].Name)
	}
	if got := spans[turn].Attrs["user_prompt"]; got != "read probe.txt then run the check" {
		t.Errorf("prompt = %q, want the query without its wrapper", got)
	}
	if got := spans[turn].Attrs["cwd"]; got != "/work/one" {
		t.Errorf("cwd = %q, want the trust record's workspace", got)
	}
	if got := spans[turn].Attrs["turn.status"]; got != "success" || spans[turn].Failed {
		t.Errorf("status = %q, failed = %v", got, spans[turn].Failed)
	}

	// A reply that only asked for a tool is not a row, so its tool hangs off
	// the turn rather than under an empty model call.
	tool := firstID + "/line-1/tool-0"
	if got := spans[tool]; got.Name != "agent.tool" || got.ParentID != turn {
		t.Errorf("tool is %q under %q, want agent.tool under the turn", got.Name, got.ParentID)
	}
	if got := spans[tool].Attrs["full_command"]; got != "printf ok; exit 3" {
		t.Errorf("full_command = %q", got)
	}
	if got := spans[tool].Attrs["traces.action"]; got != "shell" {
		t.Errorf("action = %q", got)
	}
	if got := spans[tool].Attrs["tool_summary"]; got != "Print ok and exit with code 3" {
		t.Errorf("tool_summary = %q", got)
	}

	// A reply that said something is its own row, and its tool hangs under it
	// rather than beside it on the turn.
	model := firstID + "/line-2"
	if got := spans[model]; got.Name != "agent.model" || got.ParentID != turn {
		t.Errorf("line 2 is %q under %q", got.Name, got.ParentID)
	}
	edit := firstID + "/line-2/tool-0"
	if got := spans[edit]; got.Name != "agent.edit" || got.ParentID != model {
		t.Errorf("line 2 tool is %q under %q, want agent.edit under the model call", got.Name, got.ParentID)
	}
	if got := spans[edit].Attrs["file_path"]; got != "probe.txt" {
		t.Errorf("file_path = %q, want it relative to the workspace", got)
	}
	if got := spans[edit].Attrs["traces.action"]; got != "edit" {
		t.Errorf("action = %q", got)
	}
	// A search over the workspace itself names it as ".", not as nothing.
	if got := spans[firstID+"/line-4/tool-0"].Attrs["file_path"]; got != "." {
		t.Errorf("file_path = %q for a search rooted at the workspace", got)
	}
	if got := spans[firstID+"/line-3"].Name; got != "agent.model" {
		t.Errorf("line 3 is %q", got)
	}

	// The second turn failed, and Delete is an edit action drawn as a tool.
	second := firstID + "/line-6"
	if got := spans[second]; !got.Failed || got.Error != "the model went away" || got.Attrs["turn.status"] != "error" {
		t.Errorf("second turn = failed %v, error %q, status %q", got.Failed, got.Error, got.Attrs["turn.status"])
	}
	remove := firstID + "/line-7/tool-0"
	if got := spans[remove]; got.Name != "agent.tool" || got.Attrs["traces.action"] != "edit" || got.ParentID != second {
		t.Errorf("delete is %q/%q under %q", got.Name, got.Attrs["traces.action"], got.ParentID)
	}
}

func TestReadClocksTurnsFromTheTimestampTag(t *testing.T) {
	root := state(t)
	// The file was last written after the second prompt, which is what a real
	// transcript's mtime says about the turn still going.
	written := time.Date(2026, 9, 8, 21, 12, 30, 0, time.UTC)
	if err := os.Chtimes(TranscriptPath(root, "work-one", firstID), written, written); err != nil {
		t.Fatal(err)
	}
	batch := Read(root, Options{Window: 24 * 365 * time.Hour, Session: firstID})
	spans := map[string]otlp.Span{}
	for _, span := range batch.Spans {
		spans[span.SpanID] = span
	}
	zone := time.FixedZone("UTC-7", -7*3600)
	first, second := time.Date(2026, 9, 8, 13, 56, 0, 0, zone), time.Date(2026, 9, 8, 14, 10, 0, 0, zone)

	turn := spans[firstID+"/line-0"]
	if !turn.Start.Equal(first) {
		t.Errorf("first turn starts %s, want %s", turn.Start, first)
	}
	// A turn runs until the next one opens.
	if !turn.End.Equal(second) {
		t.Errorf("first turn ends %s, want the second turn's start %s", turn.End, second)
	}
	// Its tools are points at the turn's clock, in append order.
	if tool := spans[firstID+"/line-1/tool-0"]; !tool.Start.Equal(first) || !tool.End.Equal(first) {
		t.Errorf("tool spans %s to %s", tool.Start, tool.End)
	}
	// The last turn runs until the file was last written.
	if last := spans[firstID+"/line-6"]; !last.Start.Equal(second) || !last.End.Equal(written) {
		t.Errorf("last turn spans %s to %s, want %s to %s", last.Start, last.End, second, written)
	}
	for i := 1; i < len(batch.Spans); i++ {
		if batch.Spans[i].Start.Before(batch.Spans[i-1].Start) {
			t.Fatalf("span %s out of order", batch.Spans[i].SpanID)
		}
	}
}

func TestStampOfReadsTheOffsetByHand(t *testing.T) {
	tests := map[string]time.Time{
		"<timestamp>Tuesday, Sep 8, 2026, 1:31 PM (UTC-7)</timestamp>":    time.Date(2026, 9, 8, 20, 31, 0, 0, time.UTC),
		"<timestamp>Wednesday, Jan 1, 2025, 12:05 AM (UTC+0)</timestamp>": time.Date(2025, 1, 1, 0, 5, 0, 0, time.UTC),
		"<timestamp>Monday, Mar 2, 2026, 9:00 AM (UTC+5:30)</timestamp>":  time.Date(2026, 3, 2, 3, 30, 0, 0, time.UTC),
		"<timestamp>Monday, Mar 2, 2026, 9:00 AM (UTC-3:30)</timestamp>":  time.Date(2026, 3, 2, 12, 30, 0, 0, time.UTC),
	}
	for raw, want := range tests {
		if got := stampOf(raw); !got.Equal(want) {
			t.Errorf("stampOf(%q) = %s, want %s", raw, got, want)
		}
	}
	for _, raw := range []string{"no tag", "<timestamp>garbage (UTC-7)</timestamp>", "<timestamp>Tuesday, Sep 8, 2026, 1:31 PM</timestamp>"} {
		if got := stampOf(raw); !got.IsZero() {
			t.Errorf("stampOf(%q) = %s, want the zero time", raw, got)
		}
	}
}

func TestReadFallsBackToTheFileClockWithoutATag(t *testing.T) {
	root := t.TempDir()
	path := TranscriptPath(root, "work-one", firstID)
	write(t, path, `{"role":"user","message":{"content":[{"type":"text","text":"bare prompt"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"reply"}]}}`)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	batch := Read(root, Options{Window: time.Hour})
	if len(batch.Spans) != 2 {
		t.Fatalf("spans = %d", len(batch.Spans))
	}
	for _, span := range batch.Spans {
		if !span.Start.Equal(info.ModTime()) {
			t.Errorf("%s starts %s, want the mtime %s", span.SpanID, span.Start, info.ModTime())
		}
	}
	if got := batch.Spans[0].Attrs["user_prompt"]; got != "bare prompt" {
		t.Errorf("prompt = %q, want the bare text kept as is", got)
	}
}

func TestReadOpensAStandInTurnForAReplyBeforeAnyPrompt(t *testing.T) {
	root := t.TempDir()
	write(t, TranscriptPath(root, "work-one", firstID),
		`{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"path":"/work/one/main.go"}}]}}`)
	batch := Read(root, Options{Window: time.Hour})
	if len(batch.Spans) != 2 {
		t.Fatalf("spans = %d, want a stand-in turn and its tool", len(batch.Spans))
	}
	stand := firstID + "/line-0/turn"
	if got := batch.Spans[0]; got.SpanID != stand || got.Name != "agent.turn" {
		t.Errorf("first span is %s %q", got.SpanID, got.Name)
	}
	if got := batch.Spans[1]; got.ParentID != stand || got.Attrs["traces.action"] != "read" {
		t.Errorf("tool under %q with action %q", got.ParentID, got.Attrs["traces.action"])
	}
}

func TestReadEmitsPromptAndReplyRecords(t *testing.T) {
	batch := Read(state(t), Options{Window: time.Hour, Session: firstID})
	events := map[string]int{}
	var replies []otlp.Record
	for _, record := range batch.Records {
		events[record.Event]++
		if record.Event == EventText {
			replies = append(replies, record)
		}
	}
	if events[EventPrompt] != 2 || events[EventText] != 2 {
		t.Fatalf("events = %v", events)
	}
	if got := replies[0]; got.Attrs["request_id"] != firstID+"/line-2" || !strings.Contains(got.Body, "Clarifying") {
		t.Errorf("first reply = %+v", got)
	}
	if got := batch.Records[0]; got.Event != EventPrompt || got.Body != "read probe.txt then run the check" || got.Attrs["cwd"] != "/work/one" {
		t.Errorf("first record = %+v", got)
	}
}

func TestDiscoverListsAWorkspaceNewestFirst(t *testing.T) {
	root := state(t)
	third := "33333333-3333-3333-3333-333333333333"
	write(t, TranscriptPath(root, "work-one", third), secondTranscript)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(TranscriptPath(root, "work-one", firstID), old, old); err != nil {
		t.Fatal(err)
	}
	if got := Discover(root, "/work/one"); !slices.Equal(got, []string{third, firstID}) {
		t.Errorf("discover = %v", got)
	}
	if got := Discover(root, "/work/two"); !slices.Equal(got, []string{secondID}) {
		t.Errorf("discover = %v", got)
	}
	if got := Discover(root, "/work/three"); len(got) != 0 {
		t.Errorf("discover = %v for a workspace nothing ran in", got)
	}
	if got := Discover(root, ""); len(got) != 0 {
		t.Errorf("discover = %v with no workspace", got)
	}
}

func TestProjectsToleratesAMissingTrustRecord(t *testing.T) {
	root := t.TempDir()
	write(t, TranscriptPath(root, "work-one", firstID), firstTranscript)
	write(t, filepath.Join(root, "work-two", ".workspace-trusted"), "{ half written")
	projects := Projects(root)
	if len(projects) != 2 {
		t.Fatalf("projects = %+v", projects)
	}
	for _, project := range projects {
		if project.Directory != "" {
			t.Errorf("project %s has directory %q", project.Slug, project.Directory)
		}
	}
	// A project with no trust record still answers to its slug, and its spans
	// carry no cwd rather than a guessed one.
	batch := Read(root, Options{Window: time.Hour, Directory: "/work/one"})
	if len(batch.Spans) == 0 {
		t.Fatal("slug match lost the project")
	}
	if got, ok := batch.Spans[0].Attrs["cwd"]; ok {
		t.Errorf("cwd = %q, want none", got)
	}
}

func TestActionOfNamesTheGenericActions(t *testing.T) {
	tests := map[string]string{
		"Shell":         "shell",
		"Read":          "read",
		"Grep":          "search",
		"Glob":          "search",
		"WebSearch":     "search",
		"Write":         "edit",
		"StrReplace":    "edit",
		"Delete":        "edit",
		"WebFetch":      "browse",
		"Task":          "delegate",
		"updateTodos":   "plan",
		"mcp_something": "",
	}
	for tool, want := range tests {
		if got := actionOf(tool); got != want {
			t.Errorf("actionOf(%q) = %q, want %q", tool, got, want)
		}
	}
}
