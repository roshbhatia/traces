// Package worklog reduces one finished session to one line of JSON. The line
// is what a report over many sessions reads later: which repositories moved
// and by how much, what was asked first and last, which model answered, and
// how long it took.
//
// The line is schema v2. Two readers parse it by field name, so the field set,
// its order, and the nulls are fixed. A test holds a record the previous
// writer produced and checks this one matches it byte for byte.
//
// The session's prompts, turns, and model come from the transcript reader the
// claude provider already has. This package parses no transcript of its own.
package worklog

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/roshbhatia/go-utils/git"
	"github.com/roshbhatia/traces/extras/internal/claude/transcript"
	"github.com/roshbhatia/traces/internal/otlp"
)

const schemaVersion = 2

const (
	maxCommits  = 30
	maxFiles    = 50
	promptChars = 200

	// A hook that outlives its session is a leak. Git on a stalled network
	// mount is the one thing here that can take that long.
	gitTimeout = 15 * time.Second
)

// Event is the SessionEnd payload a harness writes to the hook's stdin.
type Event struct {
	SessionID      string `json:"session_id"`
	CWD            string `json:"cwd"`
	Reason         string `json:"reason"`
	TranscriptPath string `json:"transcript_path"`
}

// Options is what the caller knows about the machine and this package does
// not: where multi-repository sessions live.
type Options struct {
	// SessionsRoot holds one directory per session, each holding one worktree
	// per repository. A cwd under it is recorded as the whole session. Empty
	// records the cwd's own repository only.
	SessionsRoot string
}

type Commit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

type File struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

type Repo struct {
	Name         string   `json:"name"`
	Branch       string   `json:"branch"`
	Head         string   `json:"head"`
	Base         string   `json:"base"`
	URL          string   `json:"url"`
	CommitsAhead int      `json:"commits_ahead"`
	Commits      []Commit `json:"commits"`
	Files        []File   `json:"files"`
	Insertions   int      `json:"insertions"`
	Deletions    int      `json:"deletions"`
	Diffstat     string   `json:"diffstat"`
	Dirty        string   `json:"dirty"`
}

// Record is one schema v2 line. Summary is always null here: the writer
// records pointers and cheap facts, and a reader fills the summary in later.
type Record struct {
	V              int     `json:"v"`
	TS             string  `json:"ts"`
	TSStart        *string `json:"ts_start"`
	DurationMin    *int    `json:"duration_min"`
	SessionID      string  `json:"session_id"`
	Kind           string  `json:"kind"`
	SessionName    string  `json:"session_name"`
	Model          *string `json:"model"`
	UserTurns      int     `json:"user_turns"`
	Repos          []Repo  `json:"repos"`
	CWD            string  `json:"cwd"`
	FirstPrompt    string  `json:"first_prompt"`
	LastPrompt     string  `json:"last_prompt"`
	TranscriptPath string  `json:"transcript_path"`
	EndReason      string  `json:"end_reason"`
	Summary        *string `json:"summary"`
}

// Build reduces one event to its record. The second result is false when the
// session left nothing worth a line: no id, a resume, or neither a
// repository nor a prompt.
func Build(ev Event, opts Options, now time.Time) (Record, bool) {
	if ev.SessionID == "" || ev.Reason == "resume" {
		return Record{}, false
	}

	kind, sessionName := "dir", ""
	repos := []Repo{}
	if name, ok := sessionOf(opts.SessionsRoot, ev.CWD); ok {
		kind, sessionName = "seshy-session", name
		repos = describeAll(filepath.Join(opts.SessionsRoot, name))
	} else if ev.CWD != "" && isRepo(ev.CWD) {
		kind = "repo"
		if repo, ok := describe(ev.CWD); ok {
			repos = append(repos, repo)
		}
	}

	// The payload names the transcript; there is no lookup. A path that is
	// not a regular file is recorded as none.
	path := ev.TranscriptPath
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		path = ""
	}
	found := intent{}
	if path != "" {
		found = intentOf(transcript.ReadFile(path))
	}

	if len(repos) == 0 && found.FirstPrompt == "" {
		return Record{}, false
	}

	ts := now.UTC().Format("2006-01-02T15:04:05Z")
	record := Record{
		V:              schemaVersion,
		TS:             ts,
		SessionID:      ev.SessionID,
		Kind:           kind,
		SessionName:    sessionName,
		UserTurns:      found.UserTurns,
		Repos:          repos,
		CWD:            ev.CWD,
		FirstPrompt:    found.FirstPrompt,
		LastPrompt:     found.LastPrompt,
		TranscriptPath: path,
		EndReason:      ev.Reason,
	}
	if !found.Start.IsZero() {
		// The harness writes its timestamps the way JavaScript does, and the
		// previous writer copied the string, so the record keeps that shape.
		start := found.Start.UTC().Format("2006-01-02T15:04:05.000Z")
		record.TSStart = &start
		if end := now.UTC().Truncate(time.Second); !end.Before(found.Start) {
			minutes := int(end.Sub(found.Start).Minutes())
			record.DurationMin = &minutes
		}
	}
	if found.Model != "" {
		record.Model = &found.Model
	}
	return record, true
}

// Append writes one record as one line, creating the file and its directory.
func Append(path string, record Record) error {
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// intent is what the transcript says about the session, as opposed to what
// git says about the repositories.
type intent struct {
	Start       time.Time
	Model       string
	FirstPrompt string
	LastPrompt  string
	UserTurns   int
}

// intentOf folds the reader's batch down to five facts. The start is the
// first turn; the model is the one on the latest call that named one; the
// turns are the prompt records.
func intentOf(batch otlp.Batch) intent {
	var found intent
	for _, span := range batch.Spans {
		if !span.Start.IsZero() && (found.Start.IsZero() || span.Start.Before(found.Start)) {
			found.Start = span.Start
		}
		if model := span.Attrs["gen_ai.request.model"]; model != "" {
			found.Model = model
		}
	}
	var prompts []string
	for _, record := range batch.Records {
		if record.Event != transcript.EventPrompt {
			continue
		}
		if !record.At.IsZero() && (found.Start.IsZero() || record.At.Before(found.Start)) {
			found.Start = record.At
		}
		if cleaned := strings.Join(strings.Fields(record.Body), " "); cleaned != "" {
			prompts = append(prompts, cleaned)
		}
	}
	found.UserTurns = len(prompts)
	if len(prompts) > 0 {
		found.FirstPrompt = truncate(prompts[0], promptChars)
		found.LastPrompt = truncate(prompts[len(prompts)-1], promptChars)
	}
	return found
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// sessionOf names the session dir sits under, when root is set and dir is
// inside it.
func sessionOf(root, dir string) (string, bool) {
	if root == "" || dir == "" {
		return "", false
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	name := strings.Split(rel, string(os.PathSeparator))[0]
	if name == "" {
		return "", false
	}
	return name, true
}

// describeAll reads every repository that is a direct child of a session
// directory, in name order. A session holds its worktrees flat, and a deeper
// walk would count a vendored checkout as work.
func describeAll(dir string) []Repo {
	repos := []Repo{}
	children, err := os.ReadDir(dir)
	if err != nil {
		return repos
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
	for _, child := range children {
		if !child.IsDir() {
			continue
		}
		path := filepath.Join(dir, child.Name())
		if !isRepo(path) {
			continue
		}
		if repo, ok := describe(path); ok {
			repos = append(repos, repo)
		}
	}
	return repos
}

// run is git.Output with a deadline. GIT_OPTIONAL_LOCKS=0 keeps a read from
// touching the index, so a hook never races the editor that is still open.
func run(dir string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(git.CleanEnv(), "GIT_OPTIONAL_LOCKS=0")
	cmd.WaitDelay = gitTimeout
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func isRepo(path string) bool {
	out, ok := run(path, "rev-parse", "--is-inside-work-tree")
	return ok && out == "true"
}

func normalizeRemote(url string) string {
	u := strings.TrimSuffix(url, ".git")
	if rest, found := strings.CutPrefix(u, "git@"); found {
		host, path, _ := strings.Cut(rest, ":")
		return "https://" + host + "/" + path
	}
	if rest, found := strings.CutPrefix(u, "ssh://"); found {
		if _, after, hasUser := strings.Cut(rest, "@"); hasUser {
			rest = after
		}
		return "https://" + rest
	}
	return u
}

// comparisonRef is what the branch is measured against: the upstream of the
// default branch, which is also the upstream of the branch when it is the
// default.
func comparisonRef(repo, branch, base string) (string, bool) {
	if branch == "" || base == "" {
		return "", false
	}
	ref := "origin/" + base
	if branch == base {
		ref = "origin/" + branch
	}
	if _, ok := run(repo, "rev-parse", "--verify", ref); !ok {
		return "", false
	}
	return ref, true
}

func describe(path string) (Repo, bool) {
	toplevel, ok := run(path, "rev-parse", "--show-toplevel")
	if !ok || toplevel == "" {
		return Repo{}, false
	}

	repo := Repo{
		Name:    filepath.Base(toplevel),
		Commits: []Commit{},
		Files:   []File{},
	}
	repo.Branch, _ = run(path, "branch", "--show-current")
	repo.Head, _ = run(path, "rev-parse", "--short", "HEAD")
	repo.Dirty, _ = run(path, "diff", "--shortstat")

	remote, ok := run(path, "remote", "get-url", "origin")
	if !ok || remote == "" {
		if names, listed := run(path, "remote"); listed && names != "" {
			first := strings.SplitN(names, "\n", 2)[0]
			remote, _ = run(path, "remote", "get-url", first)
		}
	}
	if remote != "" {
		repo.URL = normalizeRemote(remote)
		if repo.Branch != "" {
			repo.URL += "/tree/" + repo.Branch
		}
	}

	if head, found := run(path, "symbolic-ref", "refs/remotes/origin/HEAD"); found && head != "" {
		repo.Base = head[strings.LastIndex(head, "/")+1:]
	}

	ref, hasRef := comparisonRef(path, repo.Branch, repo.Base)
	if !hasRef {
		return repo, true
	}

	if count, found := run(path, "rev-list", "--count", ref+"..HEAD"); found {
		if parsed, err := strconv.Atoi(count); err == nil {
			repo.CommitsAhead = parsed
		}
	}
	repo.Diffstat, _ = run(path, "diff", "--shortstat", ref+"...HEAD")

	if log, found := run(path, "log", "--format=%h%x09%s", ref+"..HEAD"); found {
		for _, line := range lines(log, maxCommits) {
			sha, subject, _ := strings.Cut(line, "\t")
			repo.Commits = append(repo.Commits, Commit{SHA: sha, Subject: subject})
		}
	}
	if names, found := run(path, "diff", "--name-status", ref+"...HEAD"); found {
		for _, line := range lines(names, maxFiles) {
			fields := strings.Split(line, "\t")
			// A rename carries two paths; the record shows the move.
			repo.Files = append(repo.Files, File{
				Status: fields[0],
				Path:   strings.Join(fields[1:], " -> "),
			})
		}
	}
	if numstat, found := run(path, "diff", "--numstat", ref+"...HEAD"); found {
		for _, line := range lines(numstat, 0) {
			cols := strings.Split(line, "\t")
			if len(cols) < 2 {
				continue
			}
			// A binary file reports "-" and is counted as nothing.
			if n, err := strconv.Atoi(cols[0]); err == nil {
				repo.Insertions += n
			}
			if n, err := strconv.Atoi(cols[1]); err == nil {
				repo.Deletions += n
			}
		}
	}
	return repo, true
}

func lines(text string, limit int) []string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		kept = append(kept, line)
		if limit > 0 && len(kept) == limit {
			break
		}
	}
	return kept
}
