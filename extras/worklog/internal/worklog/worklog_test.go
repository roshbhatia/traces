package worklog

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var stamp = time.Date(2026, 8, 13, 19, 4, 0, 0, time.UTC)

// fixture builds testdata/fixture.sh under a fresh root and returns the root
// with symlinks resolved, because git answers --show-toplevel physically and
// the golden was generated against a physical path.
func fixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join("testdata", "fixture.sh"), root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture.sh: %v\n%s", err, out)
	}
	return root
}

// The golden lines were written by the worklog that lived in sysinit, against
// the same fixture, with its temporary root replaced by $ROOT. Each line is
// rebuilt here at the wall clock that line carries and must match exactly.
func TestARecordMatchesWhatThePreviousWriterProduced(t *testing.T) {
	root := fixture(t)
	data, err := os.ReadFile(filepath.Join("testdata", "golden-v2.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		want := strings.ReplaceAll(line, "$ROOT", root)
		var golden struct {
			TS             string `json:"ts"`
			SessionID      string `json:"session_id"`
			CWD            string `json:"cwd"`
			TranscriptPath string `json:"transcript_path"`
			EndReason      string `json:"end_reason"`
		}
		if err := json.Unmarshal([]byte(want), &golden); err != nil {
			t.Fatal(err)
		}
		now, err := time.Parse(time.RFC3339, golden.TS)
		if err != nil {
			t.Fatal(err)
		}
		record, ok := Build(Event{
			SessionID:      golden.SessionID,
			CWD:            golden.CWD,
			Reason:         golden.EndReason,
			TranscriptPath: golden.TranscriptPath,
		}, Options{SessionsRoot: filepath.Join(root, "sessions")}, now)
		if !ok {
			t.Fatalf("%s: nothing was recorded", golden.SessionID)
		}
		got, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s:\n got %s\nwant %s", golden.SessionID, got, want)
		}
	}
}

func TestARepoSessionRecordsTheBranchesWorkAndTheIntent(t *testing.T) {
	root := fixture(t)
	record, ok := Build(Event{
		SessionID: "s1", CWD: filepath.Join(root, "work"), Reason: "clear",
		TranscriptPath: filepath.Join(root, "transcripts", "s1.jsonl"),
	}, Options{}, stamp)
	if !ok {
		t.Fatal("nothing was recorded for a repo session")
	}
	if record.V != 2 || record.Kind != "repo" || record.SessionName != "" {
		t.Errorf("shape = v%d kind=%q name=%q", record.V, record.Kind, record.SessionName)
	}
	if len(record.Repos) != 1 {
		t.Fatalf("repos = %d, want 1", len(record.Repos))
	}
	repo := record.Repos[0]
	if repo.Branch != "main" || repo.Base != "main" {
		t.Errorf("branch=%q base=%q", repo.Branch, repo.Base)
	}
	if repo.CommitsAhead != 1 || len(repo.Commits) != 1 || repo.Commits[0].Subject != "add a file" {
		t.Errorf("ahead=%d commits=%+v", repo.CommitsAhead, repo.Commits)
	}
	if repo.Insertions != 2 || repo.Deletions != 0 {
		t.Errorf("insertions=%d deletions=%d, want 2 and 0", repo.Insertions, repo.Deletions)
	}
	if len(repo.Files) != 1 || repo.Files[0].Status != "A" || repo.Files[0].Path != "added.txt" {
		t.Errorf("files = %+v", repo.Files)
	}
	if repo.Dirty == "" {
		t.Error("a dirty worktree reported no shortstat")
	}
	if record.UserTurns != 2 {
		t.Errorf("user_turns = %d, want 2", record.UserTurns)
	}
	if record.FirstPrompt != "first prompt with breaks" || record.LastPrompt != "second prompt" {
		t.Errorf("prompts = %q, %q", record.FirstPrompt, record.LastPrompt)
	}
	if record.Model == nil || *record.Model != "claude-sonnet-5" {
		t.Errorf("model = %v, want the last model that answered", record.Model)
	}
	if record.TSStart == nil || *record.TSStart != "2026-08-13T18:34:00.512Z" {
		t.Errorf("ts_start = %v", record.TSStart)
	}
	if record.DurationMin == nil || *record.DurationMin != 29 {
		t.Errorf("duration_min = %v, want 29", record.DurationMin)
	}
}

func TestASessionRootRecordsEveryWorktreeUnderIt(t *testing.T) {
	root := fixture(t)
	sessions := filepath.Join(root, "sessions")
	record, ok := Build(Event{
		SessionID: "s2", CWD: filepath.Join(sessions, "a-session", "alpha"),
	}, Options{SessionsRoot: sessions}, stamp)
	if !ok {
		t.Fatal("nothing was recorded for a session")
	}
	if record.Kind != "seshy-session" || record.SessionName != "a-session" {
		t.Errorf("kind=%q name=%q", record.Kind, record.SessionName)
	}
	if len(record.Repos) != 2 || record.Repos[0].Name != "alpha" || record.Repos[1].Name != "zeta" {
		t.Errorf("repos = %+v", record.Repos)
	}

	// Without the root the same cwd is one repository.
	alone, _ := Build(Event{
		SessionID: "s2", CWD: filepath.Join(sessions, "a-session", "alpha"),
	}, Options{}, stamp)
	if alone.Kind != "repo" || len(alone.Repos) != 1 {
		t.Errorf("without a root: kind=%q repos=%d", alone.Kind, len(alone.Repos))
	}
}

func TestNothingWorthALineIsNotRecorded(t *testing.T) {
	plain := t.TempDir()
	for _, probe := range []struct {
		name string
		ev   Event
	}{
		{"no session id", Event{CWD: plain}},
		{"a resume has no finished work", Event{SessionID: "s3", CWD: plain, Reason: "resume"}},
		{"no repository and no prompt", Event{SessionID: "s4", CWD: plain}},
		{"a transcript that is not a file", Event{SessionID: "s5", CWD: plain, TranscriptPath: plain}},
	} {
		if _, ok := Build(probe.ev, Options{}, stamp); ok {
			t.Errorf("%s: a record was built anyway", probe.name)
		}
	}
}

func TestADirectorySessionWithAPromptIsRecorded(t *testing.T) {
	root := fixture(t)
	record, ok := Build(Event{
		SessionID: "s5", CWD: t.TempDir(),
		TranscriptPath: filepath.Join(root, "transcripts", "s1.jsonl"),
	}, Options{}, stamp)
	if !ok {
		t.Fatal("a prompt with no repository was dropped")
	}
	if record.Kind != "dir" || len(record.Repos) != 0 {
		t.Errorf("kind=%q repos=%d", record.Kind, len(record.Repos))
	}
}

func TestARemoteBecomesABrowsableURL(t *testing.T) {
	for _, probe := range []struct{ in, want string }{
		{"git@github.com:roshbhatia/sysinit.git", "https://github.com/roshbhatia/sysinit"},
		{"ssh://git@github.com/roshbhatia/sysinit.git", "https://github.com/roshbhatia/sysinit"},
		{"https://github.com/roshbhatia/sysinit.git", "https://github.com/roshbhatia/sysinit"},
		{"https://github.com/roshbhatia/sysinit", "https://github.com/roshbhatia/sysinit"},
	} {
		if got := normalizeRemote(probe.in); got != probe.want {
			t.Errorf("normalizeRemote(%q) = %q, want %q", probe.in, got, probe.want)
		}
	}
}

func TestAPromptIsCutByCharacter(t *testing.T) {
	if got := truncate(strings.Repeat("é", 300), promptChars); len([]rune(got)) != promptChars {
		t.Errorf("cut to %d runes, want %d", len([]rune(got)), promptChars)
	}
	if got := truncate("short", promptChars); got != "short" {
		t.Errorf("a short prompt was changed to %q", got)
	}
}

func TestTheWrittenLineCarriesEveryFieldTheReaderExpects(t *testing.T) {
	root := fixture(t)
	log := filepath.Join(t.TempDir(), "nested", "worklog.jsonl")
	record, ok := Build(Event{
		SessionID: "s6", CWD: t.TempDir(), Reason: "other",
		TranscriptPath: filepath.Join(root, "transcripts", "s1.jsonl"),
	}, Options{}, stamp)
	if !ok {
		t.Fatal("nothing to write")
	}
	for range 2 {
		if err := Append(log, record); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	written := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(written) != 2 {
		t.Fatalf("lines = %d, want 2", len(written))
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(written[0]), &got); err != nil {
		t.Fatalf("the line does not parse: %v", err)
	}
	for _, key := range []string{
		"v", "ts", "ts_start", "duration_min", "session_id", "kind", "session_name",
		"model", "user_turns", "repos", "cwd", "first_prompt", "last_prompt",
		"transcript_path", "end_reason", "summary",
	} {
		if _, present := got[key]; !present {
			t.Errorf("the record is missing %q", key)
		}
	}
	if got["summary"] != nil {
		t.Errorf("summary = %v, want null", got["summary"])
	}
	if got["ts"] != "2026-08-13T19:04:00Z" {
		t.Errorf("ts = %v", got["ts"])
	}
}
