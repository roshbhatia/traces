// Package transcript reads what the Cursor CLI writes to disk and builds the
// whole run from it: the turns, the model replies, and the tool calls each
// reply asked for.
//
// The CLI keeps one directory per workspace under ~/.cursor/projects, named by
// the workspace path with every run of non-alphanumerics folded to one dash,
// and inside it one transcript per conversation:
//
//	projects/<slug>/agent-transcripts/<id>/<id>.jsonl
//
// Its transcript writer emits three line shapes and nothing else. A user or
// assistant message is one line holding text and tool_use blocks, and a turn
// closes with a status line:
//
//	{"role":"user","message":{"content":[{"type":"text","text":"…"}]}}
//	{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Shell","input":{…}}]}}
//	{"type":"turn_ended","status":"success"}
//
// What the writer leaves out shapes this reader. No line carries a timestamp,
// so the turn's time is read out of the <timestamp> tag the CLI prepends to
// the prompt, and the file's mtime closes the last turn. No tool result is
// written, so a call is a point at the turn's start and its output is not on
// disk. Reasoning is folded into the text block ahead of the reply, so a model
// row carries both and the reader does not split them.
package transcript

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

// Service names the records and spans. The CLI exports no telemetry of its
// own, so the reader is the only source for a run and carries the harness's
// own name rather than borrowing another one.
const Service = "cursor"

// The events this package emits. session.AddRecords matches on the suffix, so
// every harness reader shares one vocabulary.
const (
	EventText   = "cursor.assistant"
	EventPrompt = "cursor.user_prompt"
)

// Transcripts is the directory under a project that holds one conversation
// directory per run, which the CLI also hands to tool shells as
// AGENT_TRANSCRIPTS.
const Transcripts = "agent-transcripts"

// A line holds a whole message, and a Write call carries the file it writes, so
// the scanner needs room past bufio's 64k default.
const maxLine = 32 << 20

// Root is the CLI's project directory, which holds one directory per workspace.
func Root() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cursor", "projects")
}

// Slug is the project directory name the CLI derives from a workspace path:
// every run of characters outside [a-zA-Z0-9] becomes one dash, and the dashes
// at either end are dropped. /work/one is work-one.
func Slug(directory string) string {
	out := notAlphanumeric.ReplaceAllString(directory, "-")
	return strings.Trim(out, "-")
}

var notAlphanumeric = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// Options is one read of the project directory.
type Options struct {
	// Window bounds the read by transcript age. Exact ignores it, because a
	// caller naming one conversation wants all of it however old it is.
	Window    time.Duration
	Session   string
	Exact     bool
	Directory string
}

// Project is one workspace directory the CLI has run in. The transcript names
// no workspace, so the cwd attribute comes from the trust record the CLI
// writes beside it, and the slug is the fallback when that record is absent.
type Project struct {
	Slug      string
	Directory string
}

// Projects lists the workspaces under the root. A root that is not there is an
// empty list: the CLI may simply never have run here.
func Projects(root string) []Project {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := make([]Project, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		out = append(out, Project{
			Slug:      entry.Name(),
			Directory: trustedDirectory(filepath.Join(root, entry.Name())),
		})
	}
	return out
}

// trustedDirectory reads the workspace path off the CLI's trust record. A
// project the user never trusted, or a half written record, yields no path
// rather than an error: the transcripts under it are still readable.
func trustedDirectory(project string) string {
	blob, err := os.ReadFile(filepath.Join(project, ".workspace-trusted"))
	if err != nil {
		return ""
	}
	var trust struct {
		WorkspacePath string `json:"workspacePath"`
	}
	if json.Unmarshal(blob, &trust) != nil {
		return ""
	}
	return trust.WorkspacePath
}

// matchesDirectory keeps a project reached by either route: the slug the CLI
// would derive from the directory, or the path its trust record names.
func (p Project) matchesDirectory(directory string) bool {
	if directory == "" {
		return true
	}
	if p.Slug == Slug(directory) {
		return true
	}
	return p.Directory != "" && filepath.Clean(p.Directory) == filepath.Clean(directory)
}

// Read turns every matching conversation into spans and records.
func Read(root string, options Options) otlp.Batch {
	out := otlp.Batch{}
	cutoff := time.Now().Add(-options.Window)
	for _, project := range Projects(root) {
		if !project.matchesDirectory(options.Directory) {
			continue
		}
		for _, id := range Conversations(root, project.Slug) {
			if !matches(id, options.Session, options.Exact) {
				continue
			}
			path := TranscriptPath(root, project.Slug, id)
			info, err := os.Stat(path)
			if err != nil || (!options.Exact && info.ModTime().Before(cutoff)) {
				continue
			}
			one := ReadFile(path, id, project)
			out.Spans = append(out.Spans, one.Spans...)
			out.Records = append(out.Records, one.Records...)
		}
	}
	return out
}

// Conversations lists the conversation directories under a project, sorted so
// two reads of the same disk emit the same order.
func Conversations(root, slug string) []string {
	entries, err := os.ReadDir(filepath.Join(root, slug, Transcripts))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out
}

// TranscriptPath is the primary transcript of a conversation. Subagent runs
// land beside it under subagents/, and this reader leaves those alone.
func TranscriptPath(root, slug, id string) string {
	return filepath.Join(root, slug, Transcripts, id, id+".jsonl")
}

// Discover lists the conversations bound to one workspace directory, newest
// transcript first, so a reader attaching to a directory lands on the run
// still going. The directory is reduced to its slug the way the CLI does it.
func Discover(root, directory string) []string {
	if root == "" || directory == "" {
		return nil
	}
	type candidate struct {
		id string
		at time.Time
	}
	slug := Slug(directory)
	var found []candidate
	for _, id := range Conversations(root, slug) {
		info, err := os.Stat(TranscriptPath(root, slug, id))
		if err != nil {
			continue
		}
		found = append(found, candidate{id: id, at: info.ModTime()})
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

// line is one transcript line. A message line has a role, a status line has a
// type, and a line in a shape this reader predates decodes to neither.
type line struct {
	Role    string `json:"role"`
	Message struct {
		Content []block `json:"content"`
	} `json:"message"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

type block struct {
	Type  string                     `json:"type"`
	Text  string                     `json:"text"`
	Name  string                     `json:"name"`
	Input map[string]json.RawMessage `json:"input"`
}

type node struct {
	id         string
	parent     string
	start, end time.Time
	name       string
	attrs      map[string]string
	err        string
}

// ReadFile reads one transcript. The file's mtime is the only clock the CLI
// leaves for the end of the run, so it closes the last turn.
func ReadFile(path, id string, project Project) otlp.Batch {
	lines := parse(path)
	if len(lines) == 0 {
		return otlp.Batch{}
	}
	var closed time.Time
	if info, err := os.Stat(path); err == nil {
		closed = info.ModTime()
	}
	return build(lines, id, project, closed)
}

func parse(path string) []line {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	out := []line{}
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 0, 64<<10), maxLine)
	for scan.Scan() {
		text := strings.TrimSpace(scan.Text())
		if text == "" {
			continue
		}
		var one line
		// A line the CLI half wrote, or wrote in a shape this reader predates,
		// is skipped rather than fatal: the rest of the run is still the truth.
		if json.Unmarshal([]byte(text), &one) != nil {
			continue
		}
		out = append(out, one)
	}
	return out
}

func build(lines []line, id string, project Project, closed time.Time) otlp.Batch {
	batch := otlp.Batch{}
	base := map[string]string{
		"service.name":  Service,
		"traces.view":   "activity",
		"traces.source": "cursor-transcript",
	}
	cwd := project.Directory
	if cwd != "" {
		base["cwd"] = cwd
	}

	order := []*node{}
	turns := []*node{}
	var turn *node
	// A turn with no readable timestamp inherits the last one seen, and the
	// first turn without one takes the file's mtime rather than the zero time.
	at := closed
	seq := 0

	for index, one := range lines {
		switch {
		case one.Role == "user":
			seq++
			prompt, stamp := one.prompt()
			if !stamp.IsZero() {
				at = stamp
			}
			turn = &node{
				id: lineID(id, index), start: at, end: at, name: "agent.turn",
				attrs: with(base, map[string]string{
					"interaction.sequence": strconv.Itoa(seq),
					"user_prompt":          prompt,
					"user_prompt_length":   strconv.Itoa(len(prompt)),
				}),
			}
			order = append(order, turn)
			turns = append(turns, turn)
			batch.Records = append(batch.Records, otlp.Record{
				Event: EventPrompt, Service: Service, Session: id, At: at,
				Body: prompt, Attrs: map[string]string{"prompt": prompt, "cwd": cwd},
			})

		case one.Role == "assistant":
			if turn == nil {
				seq++
				turn = openTurn(&order, base, id, index, at, seq)
				turns = append(turns, turn)
			}
			// A model call is a row only when it said something. A reply that
			// only asked for a tool carried no text, so its tools hang off the
			// turn instead of under an empty row.
			under, request := turn, lineID(id, index)
			if said := one.said(); said != "" {
				model := &node{
					id: request, parent: turn.id, start: at, end: at, name: "agent.model",
					attrs: with(base, map[string]string{"request_id": request}),
				}
				order = append(order, model)
				under = model
				batch.Records = append(batch.Records, otlp.Record{
					Event: EventText, Service: Service, Session: id, At: at,
					Body: said, Attrs: map[string]string{"request_id": request},
				})
			}
			count := 0
			for _, made := range one.Message.Content {
				if made.Type != "tool_use" {
					continue
				}
				order = append(order, made.node(base, under, id, index, count, request, at))
				count++
			}

		case one.Type == "turn_ended" && turn != nil:
			turn.attrs["turn.status"] = one.Status
			if one.Status == "error" {
				turn.attrs["success"] = "false"
				turn.err = one.Error
			}
		}
	}

	// A turn runs until the next one opens, and the last one until the file was
	// last written, which is the closest the transcript comes to an end stamp.
	for index, one := range turns {
		end := closed
		if index+1 < len(turns) {
			end = turns[index+1].start
		}
		if end.After(one.start) {
			one.end = end
		}
	}

	for _, one := range order {
		if one.end.Before(one.start) {
			one.end = one.start
		}
		batch.Spans = append(batch.Spans, otlp.Span{
			TraceID: id, SpanID: one.id, ParentID: one.parent, Name: one.name,
			Service: Service, Session: id, Start: one.start, End: one.end,
			Attrs: one.attrs, Failed: one.attrs["success"] == "false", Error: one.err,
		})
	}
	sort.SliceStable(batch.Spans, func(a, b int) bool {
		return batch.Spans[a].Start.Before(batch.Spans[b].Start)
	})
	return batch
}

// A reply can arrive before any prompt: a resumed conversation opens mid turn.
// A stand-in turn keeps those replies in the tree instead of dropping them.
func openTurn(order *[]*node, base map[string]string, id string, index int, at time.Time, seq int) *node {
	stand := &node{
		id: lineID(id, index) + "/turn", start: at, end: at, name: "agent.turn",
		attrs: with(base, map[string]string{
			"interaction.sequence": strconv.Itoa(seq),
			"user_prompt":          "",
		}),
	}
	*order = append(*order, stand)
	return stand
}

func (b block) node(base map[string]string, parent *node, id string, index, count int, request string, at time.Time) *node {
	name := "agent.tool"
	if editors[b.Name] {
		name = "agent.edit"
	}
	attrs := with(base, map[string]string{
		"tool_name":   b.Name,
		"tool_use_id": lineID(id, index) + "/tool-" + strconv.Itoa(count),
		"request_id":  request,
	})
	if action := actionOf(b.Name); action != "" {
		attrs["traces.action"] = action
	}
	maps.Copy(attrs, b.arguments(base["cwd"]))
	// The transcript stamps neither the call nor its output, so the call is a
	// point at the turn's clock.
	return &node{
		id: attrs["tool_use_id"], parent: parent.id, start: at, end: at,
		name: name, attrs: attrs,
	}
}

// arguments lifts what a reader actually reads onto the span. full_command and
// file_path are the two keys the rest of traces already looks for.
func (b block) arguments(cwd string) map[string]string {
	out := map[string]string{}
	if len(b.Input) == 0 {
		return out
	}
	for _, key := range []string{"command", "pattern", "glob_pattern", "url", "query", "prompt", "question"} {
		if value := text(b.Input[key]); value != "" {
			out["full_command"] = value
			break
		}
	}
	for _, key := range []string{"path", "target_directory"} {
		if value := text(b.Input[key]); value != "" {
			out["file_path"] = relative(cwd, value)
			break
		}
	}
	if value := text(b.Input["description"]); value != "" {
		out["tool_summary"] = value
	}
	if value := text(b.Input["subagent_type"]); value != "" {
		out["subagent_type"] = value
	}
	if out["full_command"] == "" {
		out["full_command"] = out["file_path"]
	}
	raw, err := json.Marshal(b.Input)
	if err == nil {
		out["tool_input"] = string(raw)
		out["tool_input_size_bytes"] = strconv.Itoa(len(raw))
	}
	return out
}

// text reads one string argument. A value of another type is left to
// tool_input, which carries the whole call.
func text(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}

// The CLI wraps the prompt in a marker and puts the wall clock ahead of it:
//
//	<timestamp>Tuesday, Sep 8, 2026, 1:31 PM (UTC-7)</timestamp>
//	<user_query>
//	…
//	</user_query>
//
// The tag is the only clock in the file, so it sets the turn's time.
var (
	userQuery = regexp.MustCompile(`(?s)<user_query>\n?(.*?)\n?</user_query>`)
	timestamp = regexp.MustCompile(`<timestamp>(.*?) \(UTC([+-]\d{1,2})(?::(\d{2}))?\)</timestamp>`)
)

const stampLayout = "Monday, Jan 2, 2006, 3:04 PM"

func (l line) prompt() (string, time.Time) {
	var texts []string
	for _, one := range l.Message.Content {
		if one.Type == "text" && one.Text != "" {
			texts = append(texts, one.Text)
		}
	}
	raw := strings.Join(texts, "\n")
	prompt := raw
	if found := userQuery.FindStringSubmatch(raw); len(found) == 2 {
		prompt = found[1]
	}
	return prompt, stampOf(raw)
}

// stampOf reads the wall clock off the prompt. The offset is written as whole
// hours with an optional minute part, which no Go layout spells, so the zone is
// built by hand and the date parsed inside it.
func stampOf(raw string) time.Time {
	found := timestamp.FindStringSubmatch(raw)
	if len(found) != 4 {
		return time.Time{}
	}
	hours, err := strconv.Atoi(found[2])
	if err != nil {
		return time.Time{}
	}
	offset := hours * 3600
	if found[3] != "" {
		minutes, err := strconv.Atoi(found[3])
		if err != nil {
			return time.Time{}
		}
		if hours < 0 {
			minutes = -minutes
		}
		offset += minutes * 60
	}
	at, err := time.ParseInLocation(stampLayout, found[1], time.FixedZone("UTC"+found[2], offset))
	if err != nil {
		return time.Time{}
	}
	return at
}

// said is the text of a reply. The writer folds reasoning into the same block
// ahead of the answer, so the two are not told apart here.
func (l line) said() string {
	var texts []string
	for _, one := range l.Message.Content {
		if one.Type == "text" && strings.TrimSpace(one.Text) != "" {
			texts = append(texts, one.Text)
		}
	}
	return strings.TrimSpace(strings.Join(texts, "\n"))
}

// A write is drawn as a diff rather than as a blob of arguments, so it is named
// apart from the tools that only read. Delete carries no content to draw and
// stays a tool.
var editors = map[string]bool{
	"Write":      true,
	"StrReplace": true,
	"MultiEdit":  true,
}

// actionOf translates this harness's tool vocabulary into the generic actions
// that Traces renders. An unknown tool, such as an MCP call, keeps its own name.
func actionOf(tool string) string {
	switch tool {
	case "Task", "task":
		return "delegate"
	case "Write", "StrReplace", "MultiEdit", "Delete":
		return "edit"
	case "Shell", "WriteShellStdin", "ReadShellOutput", "KillShell":
		return "shell"
	case "Glob", "Grep", "List", "Ls", "SemanticSearch", "WebSearch", "ListMcpResources":
		return "search"
	case "WebFetch", "Fetch", "FetchMcpResource", "ComputerUse", "RecordScreen":
		return "browse"
	case "Read", "ReadLints":
		return "read"
	case "createPlan", "updateTodos", "TodoWrite":
		return "plan"
	default:
		return ""
	}
}

func lineID(id string, index int) string {
	return id + "/line-" + strconv.Itoa(index)
}

// relative trims the working directory off a path inside it. A path elsewhere is
// left absolute, because that is the fact worth seeing about it, and the
// directory itself, which a search over the whole workspace names, is ".".
func relative(cwd, path string) string {
	if cwd == "" || !strings.HasPrefix(path, cwd) {
		return path
	}
	if trimmed := strings.TrimPrefix(strings.TrimPrefix(path, cwd), "/"); trimmed != "" {
		return trimmed
	}
	return "."
}

func with(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	maps.Copy(out, base)
	maps.Copy(out, extra)
	return out
}
