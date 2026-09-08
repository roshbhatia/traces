// Package trajectory reads what the Antigravity CLI writes to disk and builds
// the whole run from it: the turns, the model replies, the tool calls the reply
// asked for, and the output that came back.
//
// The CLI keeps one directory per conversation under brain/, and inside it a
// JSONL transcript of every step. Its own embedded documentation describes the
// pair:
//
//	transcript_full.jsonl  the complete, untruncated record
//	transcript.jsonl       the same lines with large text clipped
//
// The lines map one to one, so the full file is read first and the clipped file
// is the fallback. The clipped file also re-encodes every tool argument as a
// JSON string holding a JSON value ("2000", "\"ls -la\""), which is why args are
// decoded raw and unwrapped once.
//
// The shape on disk is a flat list, not a tree: a step carries an index and
// nothing that names its parent. The tree a reader wants is recovered from the
// order the CLI appends in,
//
//	turn        USER_INPUT
//	└─ model    PLANNER_RESPONSE that said something
//	   └─ tool  the calls that reply asked for
//
// and a tool's output arrives as the next MODEL/GENERIC step, so an open call
// takes the first GENERIC that follows it.
package trajectory

import (
	"bufio"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/roshbhatia/traces/internal/otlp"
)

// Service names the records and spans. The CLI exports no telemetry of its own,
// so the reader is the only source for a run and carries the harness's own name
// rather than borrowing another one.
const Service = "antigravity"

// The events this package emits. session.AddRecords matches on the suffix, so
// every harness reader shares one vocabulary.
const (
	EventText   = "antigravity.assistant"
	EventResult = "antigravity.tool_result"
	EventPrompt = "antigravity.user_prompt"
)

// A step holds a whole command's output on one line, so the scanner needs room
// past bufio's 64k default.
const maxLine = 32 << 20

// Root is the CLI's application data directory, which holds one conversation
// directory per run and the caches that bind them to a workspace.
func Root() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gemini", "antigravity-cli")
}

// Options is one read of the state directory.
type Options struct {
	// Window bounds the read by transcript age. Exact ignores it, because a
	// caller naming one conversation wants all of it however old it is.
	Window    time.Duration
	Session   string
	Exact     bool
	Directory string
}

// Read turns every matching conversation into spans and records. A state
// directory that is not there is an empty batch: the CLI may simply never have
// run here, and that is not a failure to report.
func Read(root string, options Options) otlp.Batch {
	out := otlp.Batch{}
	if root == "" {
		return out
	}
	entries, err := os.ReadDir(filepath.Join(root, "brain"))
	if err != nil {
		return out
	}
	meta := Summaries(root)
	cutoff := time.Now().Add(-options.Window)
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		if !matches(id, options.Session, options.Exact) {
			continue
		}
		if !inDirectory(meta[id], options.Directory) {
			continue
		}
		path := TranscriptPath(root, id)
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || (!options.Exact && info.ModTime().Before(cutoff)) {
			continue
		}
		one := ReadFile(path, id, meta[id])
		out.Spans = append(out.Spans, one.Spans...)
		out.Records = append(out.Records, one.Records...)
	}
	return out
}

// TranscriptPath prefers the untruncated transcript. The clipped one is read
// only when the full file is missing, which is how the CLI describes the pair.
func TranscriptPath(root, id string) string {
	logs := filepath.Join(root, "brain", id, ".system_generated", "logs")
	for _, name := range []string{"transcript_full.jsonl", "transcript.jsonl"} {
		path := filepath.Join(logs, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// Summary is what the CLI's own cache knows about a conversation. The transcript
// names no workspace, so the directory filter and the cwd attribute come from
// here.
type Summary struct {
	Title      string   `json:"Title"`
	Workspaces []string `json:"WorkspaceURIs"`
}

// Directory is the workspace the conversation ran in, with the file URI the
// cache stores reduced to a path.
func (s Summary) Directory() string {
	for _, uri := range s.Workspaces {
		if path := cleanURI(uri); path != "" {
			return path
		}
	}
	return ""
}

// Summaries reads the conversation cache. A missing or half written cache
// yields no entries rather than an error: every conversation is still readable
// from its own transcript, only without a title or a workspace.
func Summaries(root string) map[string]Summary {
	out := map[string]Summary{}
	blob, err := os.ReadFile(filepath.Join(root, "cache", "conversation_metadata.json"))
	if err != nil {
		return out
	}
	var file struct {
		Conversations map[string]struct {
			Summary Summary `json:"summary"`
		} `json:"conversations"`
	}
	if json.Unmarshal(blob, &file) != nil {
		return out
	}
	for id, one := range file.Conversations {
		out[id] = one.Summary
	}
	return out
}

// Current is the conversation the CLI last opened in this workspace. The CLI
// exports no session id into the environment it runs tools in, so this cache is
// the only record of which run a directory is attached to.
func Current(root, directory string) string {
	blob, err := os.ReadFile(filepath.Join(root, "cache", "last_conversations.json"))
	if err != nil {
		return ""
	}
	var last map[string]string
	if json.Unmarshal(blob, &last) != nil {
		return ""
	}
	if id := last[directory]; id != "" {
		return id
	}
	if absolute, err := filepath.Abs(directory); err == nil {
		return last[absolute]
	}
	return ""
}

// Discover lists the conversations bound to a workspace, newest transcript
// first, so a reader attaching to a directory lands on the run still going.
func Discover(root, directory string) []string {
	type candidate struct {
		id string
		at time.Time
	}
	var found []candidate
	for id, one := range Summaries(root) {
		if directory != "" && !sameDirectory(one.Directory(), directory) {
			continue
		}
		var at time.Time
		if info, err := os.Stat(TranscriptPath(root, id)); err == nil {
			at = info.ModTime()
		}
		found = append(found, candidate{id: id, at: at})
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].at.Equal(found[j].at) {
			return found[i].id < found[j].id
		}
		return found[i].at.After(found[j].at)
	})
	out := make([]string, 0, len(found))
	for _, one := range found {
		out = append(out, one.id)
	}
	return out
}

func matches(id, session string, exact bool) bool {
	if session == "" {
		return !exact
	}
	if exact {
		return id == session
	}
	return strings.Contains(id, session)
}

// inDirectory keeps a conversation the cache says nothing about. A run whose
// workspace was never recorded is still activity, and hiding it would make the
// filter lose runs rather than narrow them.
func inDirectory(one Summary, directory string) bool {
	if directory == "" || len(one.Workspaces) == 0 {
		return true
	}
	for _, uri := range one.Workspaces {
		if sameDirectory(cleanURI(uri), directory) {
			return true
		}
	}
	return false
}

func sameDirectory(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func cleanURI(uri string) string {
	return strings.TrimPrefix(uri, "file://")
}

// step is one line of the transcript. The CLI adds types between versions, so an
// unknown one decodes to a zero step and is skipped rather than failing the file.
type step struct {
	Index     int    `json:"step_index"`
	Source    string `json:"source"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	Content   string `json:"content"`
	Thinking  string `json:"thinking"`
	ToolCalls []call `json:"tool_calls"`

	at time.Time
}

type call struct {
	Name string                     `json:"name"`
	Args map[string]json.RawMessage `json:"args"`
}

type node struct {
	id         string
	parent     string
	start, end time.Time
	name       string
	attrs      map[string]string
}

func ReadFile(path, id string, meta Summary) otlp.Batch {
	steps := parse(path)
	if len(steps) == 0 {
		return otlp.Batch{}
	}
	return build(steps, id, meta)
}

func parse(path string) []step {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	out := []step{}
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 0, 64<<10), maxLine)
	for scan.Scan() {
		text := strings.TrimSpace(scan.Text())
		if text == "" {
			continue
		}
		var one step
		// A line the CLI half wrote, or wrote in a shape this reader predates,
		// is skipped rather than fatal: the rest of the run is still the truth.
		if json.Unmarshal([]byte(text), &one) != nil {
			continue
		}
		one.at, _ = time.Parse(time.RFC3339Nano, one.CreatedAt)
		out = append(out, one)
	}
	return out
}

func build(steps []step, id string, meta Summary) otlp.Batch {
	batch := otlp.Batch{}
	base := map[string]string{
		"service.name":  Service,
		"traces.view":   "activity",
		"traces.source": "antigravity-transcript",
	}
	if cwd := meta.Directory(); cwd != "" {
		base["cwd"] = cwd
	}
	if meta.Title != "" {
		base["session.title"] = meta.Title
	}

	order := []*node{}
	open := []*node{}
	var turn *node
	seq := 0

	for _, one := range steps {
		switch {
		case one.Type == "USER_INPUT":
			seq++
			prompt := one.prompt()
			turn = &node{
				id: spanID(id, one.Index), start: one.at, end: one.at, name: "agent.turn",
				attrs: with(base, map[string]string{
					"interaction.sequence": strconv.Itoa(seq),
					"user_prompt":          prompt,
					"user_prompt_length":   strconv.Itoa(len(prompt)),
				}),
			}
			order = append(order, turn)
			batch.Records = append(batch.Records, otlp.Record{
				Event: EventPrompt, Service: Service, Session: id, At: one.at,
				Body: prompt, Attrs: map[string]string{"prompt": prompt, "cwd": base["cwd"]},
			})

		case one.Type == "PLANNER_RESPONSE":
			if turn == nil {
				seq++
				turn = openTurn(&order, base, id, one, seq)
			}
			stretch(turn, one.at)
			// A model call is a row only when it said something. A reply that
			// only asked for a tool carried no text and no reasoning, so its
			// tools hang off the turn instead of under an empty row.
			under, request := turn, spanID(id, one.Index)
			if one.said() {
				model := &node{
					id: request, parent: turn.id, start: one.at, end: one.at, name: "agent.model",
					attrs: with(base, map[string]string{"request_id": request}),
				}
				order = append(order, model)
				under = model
				batch.Records = append(batch.Records, otlp.Record{
					Event: EventText, Service: Service, Session: id, At: one.at,
					Body:  one.Content,
					Attrs: map[string]string{"request_id": request, "thinking": one.Thinking},
				})
			}
			for index, made := range one.ToolCalls {
				tool := made.node(base, under, id, one, index, request)
				order = append(order, tool)
				open = append(open, tool)
			}

		case one.Type == "GENERIC" && len(open) > 0:
			tool := open[0]
			open = open[1:]
			stretch(tool, one.at)
			stretch(turn, one.at)
			failed := one.failed()
			if code, ok := exitCode(one.Content); ok {
				tool.attrs["exit_code"] = strconv.Itoa(code)
			}
			if failed {
				tool.attrs["success"] = "false"
			}
			batch.Records = append(batch.Records, otlp.Record{
				Event: EventResult, Service: Service, Session: id, At: one.at,
				Body: one.Content, Attrs: map[string]string{
					"tool_use_id": tool.id,
					"is_error":    boolText(failed),
				},
			})

		case one.Type == "SYSTEM_MESSAGE":
			if turn == nil {
				continue
			}
			stretch(turn, one.at)
			order = append(order, &node{
				id: spanID(id, one.Index), parent: turn.id, start: one.at, end: one.at,
				name: "agent.note",
				attrs: with(base, map[string]string{
					"note.kind": strings.ToLower(one.Source),
					"note.text": one.Content,
				}),
			})
		}
	}

	for _, one := range order {
		if one.end.Before(one.start) {
			one.end = one.start
		}
		batch.Spans = append(batch.Spans, otlp.Span{
			TraceID: id, SpanID: one.id, ParentID: one.parent, Name: one.name,
			Service: Service, Session: id, Start: one.start, End: one.end,
			Attrs: one.attrs, Failed: one.attrs["success"] == "false",
		})
	}
	sort.SliceStable(batch.Spans, func(a, b int) bool {
		return batch.Spans[a].Start.Before(batch.Spans[b].Start)
	})
	return batch
}

// A reply can arrive before any prompt: a resumed conversation opens mid turn.
// A stand-in turn keeps those replies in the tree instead of dropping them.
func openTurn(order *[]*node, base map[string]string, id string, one step, seq int) *node {
	stand := &node{
		id: spanID(id, one.Index) + "/turn", start: one.at, end: one.at, name: "agent.turn",
		attrs: with(base, map[string]string{
			"interaction.sequence": strconv.Itoa(seq),
			"user_prompt":          "",
		}),
	}
	*order = append(*order, stand)
	return stand
}

func (c call) node(base map[string]string, parent *node, id string, one step, index int, request string) *node {
	name := "agent.tool"
	if editors[c.Name] {
		name = "agent.edit"
	}
	attrs := with(base, map[string]string{
		"tool_name":   c.Name,
		"tool_use_id": spanID(id, one.Index) + "/tool-" + strconv.Itoa(index),
		"request_id":  request,
	})
	if action := actionOf(c.Name); action != "" {
		attrs["traces.action"] = action
	}
	maps.Copy(attrs, c.arguments(base["cwd"]))
	// The call is requested when the reply ends, and its output stamps the end.
	// Until that arrives the span is a point, which is what an open call is.
	return &node{
		id: attrs["tool_use_id"], parent: parent.id, start: one.at, end: one.at,
		name: name, attrs: attrs,
	}
}

// arguments lifts what a reader actually reads onto the span. full_command and
// file_path are the two keys the rest of traces already looks for.
func (c call) arguments(cwd string) map[string]string {
	out := map[string]string{}
	if len(c.Args) == 0 {
		return out
	}
	for _, key := range []string{"CommandLine", "Query", "query", "Prompt", "TaskDescription", "Url", "Action"} {
		if value := text(c.Args[key]); value != "" {
			out["full_command"] = value
			break
		}
	}
	if value := text(c.Args["TypeName"]); value != "" {
		out["subagent_type"] = value
	}
	for _, key := range []string{"AbsolutePath", "TargetFile", "SearchPath"} {
		if value := text(c.Args[key]); value != "" {
			out["file_path"] = relative(cwd, value)
			break
		}
	}
	if value := text(c.Args["Cwd"]); value != "" {
		out["cwd"] = value
	}
	if value := text(c.Args["toolSummary"]); value != "" {
		out["tool_summary"] = value
	}
	if out["full_command"] == "" {
		out["full_command"] = firstOf(out["file_path"], text(c.Args["toolAction"]))
	}
	raw, err := json.Marshal(c.Args)
	if err == nil {
		out["tool_input"] = string(raw)
		out["tool_input_size_bytes"] = strconv.Itoa(len(raw))
	}
	return out
}

// text reads one argument. transcript.jsonl stores every value as a JSON string
// holding the JSON encoding of the real value, so a string that parses again is
// unwrapped once; transcript_full.jsonl stores the value directly.
func text(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return strings.TrimSpace(string(raw))
	}
	var inner string
	if json.Unmarshal([]byte(value), &inner) == nil {
		return inner
	}
	return value
}

// The prompt is wrapped in a marker with the harness's own metadata appended
// after it. Without the unwrap every turn's title was the local time and the
// model the reader had selected.
var userRequest = regexp.MustCompile(`(?s)<USER_REQUEST>\n?(.*?)\n?</USER_REQUEST>`)

func (s step) prompt() string {
	if found := userRequest.FindStringSubmatch(s.Content); len(found) == 2 {
		return found[1]
	}
	return s.Content
}

func (s step) said() bool {
	return strings.TrimSpace(s.Content) != "" || strings.TrimSpace(s.Thinking) != ""
}

func (s step) failed() bool {
	if s.Status == "ERROR" {
		return true
	}
	code, ok := exitCode(s.Content)
	return ok && code != 0
}

// The CLI reports a command's status as one line of its output rather than as a
// field, so the exit code is read back out of the text it wrote.
var exitLine = regexp.MustCompile(`The command exited with code (-?\d+)`)

func exitCode(content string) (int, bool) {
	found := exitLine.FindStringSubmatch(content)
	if len(found) != 2 {
		return 0, false
	}
	code, err := strconv.Atoi(found[1])
	if err != nil {
		return 0, false
	}
	return code, true
}

// A write is drawn as a diff rather than as a blob of arguments, so it is named
// apart from the tools that only read.
var editors = map[string]bool{
	"create_file":          true,
	"edit_file":            true,
	"replace_file_content": true,
	"write_to_file":        true,
}

// actionOf translates this harness's tool vocabulary into the generic actions
// that Traces renders. An unknown tool keeps its own name.
func actionOf(tool string) string {
	if strings.HasPrefix(tool, "browser_") {
		return "browse"
	}
	switch tool {
	case "invoke_subagent":
		return "delegate"
	case "create_file", "edit_file", "replace_file_content", "write_to_file":
		return "edit"
	case "run_command", "command_status", "read_terminal":
		return "shell"
	case "codebase_search", "find_by_name", "grep_search", "list_dir", "search_web":
		return "search"
	case "read_url_content", "capture_browser_screenshot", "read_browser_page":
		return "browse"
	case "view_code_item", "view_content_chunk", "view_file":
		return "read"
	case "manage_task", "schedule":
		return "plan"
	default:
		return ""
	}
}

func spanID(id string, index int) string {
	return id + "/step-" + strconv.Itoa(index)
}

// relative trims the working directory off a path inside it. A path elsewhere is
// left absolute, because that is the fact worth seeing about it.
func relative(cwd, path string) string {
	if cwd == "" || !strings.HasPrefix(path, cwd) {
		return path
	}
	return strings.TrimPrefix(strings.TrimPrefix(path, cwd), "/")
}

func with(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	maps.Copy(out, base)
	maps.Copy(out, extra)
	return out
}

func stretch(one *node, at time.Time) {
	if one == nil || at.IsZero() {
		return
	}
	if one.start.IsZero() || at.Before(one.start) {
		one.start = at
	}
	if at.After(one.end) {
		one.end = at
	}
}

func firstOf(values ...string) string {
	for _, one := range values {
		if one != "" {
			return one
		}
	}
	return ""
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return ""
}
