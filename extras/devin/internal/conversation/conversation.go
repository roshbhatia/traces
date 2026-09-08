// Package conversation reads what the Devin CLI writes to disk and rebuilds a
// run from it: the turns, the model replies, the tool calls those replies asked
// for, and the output that came back.
//
// The CLI keeps every session in one SQLite database, sessions.db, under its
// data directory. A session is a forest of message nodes rather than a flat
// list: sessions.main_chain_id names the leaf of the path that is the live
// conversation, and each message_nodes row names its parent. The linear
// transcript a reader wants is the walk from that leaf up to a root, reversed.
//
// A node's chat_message is the serialized message. The roles that carry a run
// are user (a prompt), assistant (a reply, optionally with tool_calls), and
// tool (a call's output, keyed back by tool_call_id). The many system nodes are
// the prompt context the CLI rebuilds each turn and are not activity.
//
// The database is read through the sqlite3 command rather than a driver, so the
// provider closure carries sqlite3 and the core does not. The reader is
// stateless: it answers which sessions ended in the window, and Traces decides
// what is new.
package conversation

import (
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
const Service = "devin"

// The events this package emits. session.AddRecords matches on the suffix, so
// every harness reader shares one vocabulary.
const (
	EventText   = "devin.assistant"
	EventResult = "devin.tool_result"
	EventPrompt = "devin.user_prompt"
)

// database is the file the CLI keeps every session in.
const database = "sessions.db"

// Root is the CLI's data directory, which holds the session database. The CLI
// exports the database path into every tool shell it runs, so that env var wins
// when it is set; otherwise the reader honors XDG_DATA_HOME and falls back to
// the CLI's fixed ~/.local/share path, which it uses on macOS as well.
func Root() string {
	if db := os.Getenv("CHISEL_SESSION_DB"); db != "" {
		return filepath.Dir(db)
	}
	if data := os.Getenv("XDG_DATA_HOME"); data != "" {
		return filepath.Join(data, "devin", "cli")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "devin", "cli")
}

func dbPath(root string) string {
	return filepath.Join(root, database)
}

// Options is one read of the session database.
type Options struct {
	// Window bounds the read by session age. Exact ignores it, because a caller
	// naming one session wants all of it however old it is.
	Window    time.Duration
	Session   string
	Exact     bool
	Directory string
}

// sessionRow is what the sessions table records about one run.
type sessionRow struct {
	ID           string
	Model        string
	Directory    string
	Title        string
	LastActivity int64
	MainChain    int64
	HasMainChain bool
}

// nodeRow is one message node in a session's forest.
type nodeRow struct {
	NodeID  int64
	Parent  int64
	HasPar  bool
	Message message
}

// message is the serialized chat message a node carries.
type message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls"`
	ToolCallID string     `json:"tool_call_id"`
	Thinking   thinking   `json:"thinking"`
	Metadata   metadata   `json:"metadata"`
}

type toolCall struct {
	ID        string                     `json:"id"`
	Name      string                     `json:"name"`
	Arguments map[string]json.RawMessage `json:"arguments"`
	Index     int                        `json:"index"`
}

// thinking is the reply's private reasoning. Only its text is activity; the
// signature is a provider token.
type thinking struct {
	Thinking string `json:"thinking"`
}

type metadata struct {
	IsUserInput  *bool  `json:"is_user_input"`
	RequestID    string `json:"request_id"`
	FinishReason string `json:"finish_reason"`
	CreatedAt    string `json:"created_at"`
	Model        string `json:"generation_model"`
}

func (m message) at() time.Time {
	stamp, _ := time.Parse(time.RFC3339Nano, m.Metadata.CreatedAt)
	return stamp
}

func (m message) said() bool {
	return strings.TrimSpace(m.Content) != "" || strings.TrimSpace(m.Thinking.Thinking) != ""
}

// Read turns every matching session into spans and records. A database that is
// not there is an empty batch: the CLI may simply never have run on this
// machine, and that is not a failure to report.
func Read(root string, options Options) otlp.Batch {
	out := otlp.Batch{}
	if root == "" {
		return out
	}
	sessions, err := loadSessions(dbPath(root))
	if err != nil {
		return out
	}
	cutoff := time.Now().Add(-options.Window)
	for _, one := range sessions {
		if !matches(one.ID, options.Session, options.Exact) {
			continue
		}
		if !inDirectory(one.Directory, options.Directory) {
			continue
		}
		if !options.Exact && time.Unix(one.LastActivity, 0).Before(cutoff) {
			continue
		}
		nodes, err := loadNodes(dbPath(root), one.ID)
		if err != nil {
			continue
		}
		batch := build(one, chain(nodes, one))
		out.Spans = append(out.Spans, batch.Spans...)
		out.Records = append(out.Records, batch.Records...)
	}
	return out
}

// Current is the most recently active session bound to a workspace. The CLI
// exports no session id into the shells it runs tools in, so the newest session
// for the directory is the best answer to which run a directory is attached to.
func Current(root, directory string) string {
	ids := Discover(root, directory)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// Discover lists the sessions bound to a workspace, newest first, so a reader
// attaching to a directory lands on the run still going. An empty directory
// lists every session.
func Discover(root, directory string) []string {
	sessions, err := loadSessions(dbPath(root))
	if err != nil {
		return nil
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].LastActivity > sessions[j].LastActivity
	})
	out := make([]string, 0, len(sessions))
	for _, one := range sessions {
		if directory != "" && !sameDirectory(one.Directory, directory) {
			continue
		}
		out = append(out, one.ID)
	}
	return out
}

// chain is the live conversation: the walk from the session's main-chain leaf up
// to a root, reversed into time order. A session without a recorded leaf falls
// back to the highest node id, which is the last one the CLI appended.
func chain(nodes []nodeRow, one sessionRow) []nodeRow {
	byID := make(map[int64]nodeRow, len(nodes))
	var top int64
	var haveTop bool
	for _, node := range nodes {
		byID[node.NodeID] = node
		if !haveTop || node.NodeID > top {
			top, haveTop = node.NodeID, true
		}
	}
	leaf := one.MainChain
	if !one.HasMainChain {
		leaf = top
	}
	var walk []nodeRow
	seen := map[int64]bool{}
	for id := leaf; ; {
		node, ok := byID[id]
		if !ok || seen[id] {
			break
		}
		seen[id] = true
		walk = append(walk, node)
		if !node.HasPar {
			break
		}
		id = node.Parent
	}
	for i, j := 0, len(walk)-1; i < j; i, j = i+1, j-1 {
		walk[i], walk[j] = walk[j], walk[i]
	}
	return walk
}

type node struct {
	id         string
	parent     string
	start, end time.Time
	name       string
	attrs      map[string]string
}

// build maps one session's live chain onto spans and records.
func build(one sessionRow, walk []nodeRow) otlp.Batch {
	batch := otlp.Batch{}
	base := map[string]string{
		"service.name":  Service,
		"traces.view":   "activity",
		"traces.source": "devin-session",
	}
	if one.Directory != "" {
		base["cwd"] = one.Directory
	}
	if one.Title != "" {
		base["session.title"] = one.Title
	}
	if one.Model != "" {
		base["model"] = one.Model
	}

	order := []*node{}
	open := map[string]*node{}
	var turn *node
	seq := 0
	var clock time.Time

	for _, row := range walk {
		m := row.Message
		at := m.at()
		if at.IsZero() {
			at = clock
		} else {
			clock = at
		}
		switch m.Role {
		case "user":
			// A user role that is not a prompt is a synthesized message the CLI
			// threads back to the model, not a turn the operator opened.
			if m.Metadata.IsUserInput != nil && !*m.Metadata.IsUserInput {
				continue
			}
			seq++
			prompt := m.Content
			turn = &node{
				id: spanID(one.ID, row.NodeID), start: at, end: at, name: "agent.turn",
				attrs: with(base, map[string]string{
					"interaction.sequence": strconv.Itoa(seq),
					"user_prompt":          prompt,
					"user_prompt_length":   strconv.Itoa(len(prompt)),
				}),
			}
			order = append(order, turn)
			batch.Records = append(batch.Records, otlp.Record{
				Event: EventPrompt, Service: Service, Session: one.ID, At: at,
				Body: prompt, Attrs: map[string]string{"prompt": prompt, "cwd": base["cwd"]},
			})

		case "assistant":
			if turn == nil {
				seq++
				turn = openTurn(&order, base, one.ID, row, seq, at)
			}
			stretch(turn, at)
			under, request := turn, m.Metadata.RequestID
			if request == "" {
				request = spanID(one.ID, row.NodeID)
			}
			// A reply is a row only when it said something. A reply that only
			// asked for a tool carried no text, so its tools hang off the turn.
			if m.said() {
				model := &node{
					id: spanID(one.ID, row.NodeID), parent: turn.id, start: at, end: at,
					name: "agent.model", attrs: with(base, map[string]string{"request_id": request}),
				}
				order = append(order, model)
				under = model
				batch.Records = append(batch.Records, otlp.Record{
					Event: EventText, Service: Service, Session: one.ID, At: at,
					Body:  m.Content,
					Attrs: map[string]string{"request_id": request, "thinking": m.Thinking.Thinking},
				})
			}
			for _, made := range m.ToolCalls {
				tool := made.node(base, under, one.ID, row, request, at)
				order = append(order, tool)
				if made.ID != "" {
					open[made.ID] = tool
				}
			}

		case "tool":
			tool := open[m.ToolCallID]
			useID := m.ToolCallID
			if tool != nil {
				delete(open, m.ToolCallID)
				useID = tool.id
				stretch(tool, at)
				stretch(turn, at)
				if code, ok := exitCode(m.Content); ok {
					tool.attrs["exit_code"] = strconv.Itoa(code)
				}
				if toolFailed(m.Content) {
					tool.attrs["success"] = "false"
				}
			}
			batch.Records = append(batch.Records, otlp.Record{
				Event: EventResult, Service: Service, Session: one.ID, At: at,
				Body: m.Content, Attrs: map[string]string{
					"tool_use_id": useID,
					"is_error":    boolText(toolFailed(m.Content)),
				},
			})
		}
	}

	for _, span := range order {
		if span.end.Before(span.start) {
			span.end = span.start
		}
		batch.Spans = append(batch.Spans, otlp.Span{
			TraceID: one.ID, SpanID: span.id, ParentID: span.parent, Name: span.name,
			Service: Service, Session: one.ID, Start: span.start, End: span.end,
			Attrs: span.attrs, Failed: span.attrs["success"] == "false",
		})
	}
	sort.SliceStable(batch.Spans, func(a, b int) bool {
		return batch.Spans[a].Start.Before(batch.Spans[b].Start)
	})
	return batch
}

// A reply can arrive before any prompt: a resumed conversation opens mid turn.
// A stand-in turn keeps those replies in the tree instead of dropping them.
func openTurn(order *[]*node, base map[string]string, id string, row nodeRow, seq int, at time.Time) *node {
	stand := &node{
		id: spanID(id, row.NodeID) + "/turn", start: at, end: at, name: "agent.turn",
		attrs: with(base, map[string]string{
			"interaction.sequence": strconv.Itoa(seq),
			"user_prompt":          "",
		}),
	}
	*order = append(*order, stand)
	return stand
}

func (c toolCall) node(base map[string]string, parent *node, id string, row nodeRow, request string, at time.Time) *node {
	name := "agent.tool"
	if editors[c.Name] {
		name = "agent.edit"
	}
	attrs := with(base, map[string]string{
		"tool_name":   c.Name,
		"tool_use_id": spanID(id, row.NodeID) + "/tool-" + strconv.Itoa(c.Index),
		"request_id":  request,
	})
	if action := actionOf(c.Name); action != "" {
		attrs["traces.action"] = action
	}
	maps.Copy(attrs, c.arguments(base["cwd"]))
	// The call is requested when the reply ends, and its output stamps the end.
	// Until that arrives the span is a point, which is what an open call is.
	return &node{
		id: attrs["tool_use_id"], parent: parent.id, start: at, end: at, name: name, attrs: attrs,
	}
}

// arguments lifts what a reader actually reads onto the span. full_command and
// file_path are the two keys the rest of traces already looks for.
func (c toolCall) arguments(cwd string) map[string]string {
	out := map[string]string{}
	if len(c.Arguments) == 0 {
		return out
	}
	for _, key := range []string{"command", "CommandLine", "query", "prompt", "url", "task"} {
		if value := text(c.Arguments[key]); value != "" {
			out["full_command"] = value
			break
		}
	}
	for _, key := range []string{"path", "file_path", "abs_path", "AbsolutePath", "target_file"} {
		if value := text(c.Arguments[key]); value != "" {
			out["file_path"] = relative(cwd, value)
			break
		}
	}
	if out["full_command"] == "" && out["file_path"] != "" {
		out["full_command"] = out["file_path"]
	}
	if raw, err := json.Marshal(c.Arguments); err == nil {
		out["tool_input"] = string(raw)
		out["tool_input_size_bytes"] = strconv.Itoa(len(raw))
	}
	return out
}

// text reads one argument, unwrapping a value the CLI stored as a JSON string.
func text(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return strings.TrimSpace(string(raw))
	}
	return value
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

// inDirectory keeps a session the database records no directory for. A run whose
// workspace was never stored is still activity, and hiding it would make the
// filter lose runs rather than narrow them.
func inDirectory(directory, want string) bool {
	if want == "" || directory == "" {
		return true
	}
	return sameDirectory(directory, want)
}

func sameDirectory(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

// editors are the tools drawn as a diff rather than a blob of arguments, so they
// are named apart from the tools that only read.
var editors = map[string]bool{
	"create_file":  true,
	"edit_file":    true,
	"str_replace":  true,
	"write_file":   true,
	"multi_edit":   true,
	"apply_patch":  true,
	"insert":       true,
	"replace_file": true,
}

// actionOf translates this harness's tool vocabulary into the generic actions
// that Traces renders. An unknown tool keeps its own name.
func actionOf(tool string) string {
	if strings.HasPrefix(tool, "browser_") {
		return "browse"
	}
	switch tool {
	case "exec", "run_command", "shell":
		return "shell"
	case "create_file", "edit_file", "str_replace", "write_file", "multi_edit", "apply_patch", "insert", "replace_file":
		return "edit"
	case "read_file", "view", "view_file", "read", "cat":
		return "read"
	case "grep", "glob", "search", "codebase_search", "find", "ls", "list_dir":
		return "search"
	case "fetch", "read_url", "web_search":
		return "browse"
	case "run_subagent", "spawn_subagent":
		return "delegate"
	case "plan", "update_plan", "todo":
		return "plan"
	default:
		return ""
	}
}

// toolFailed reads a command's status back out of the text it wrote, since the
// tool result carries no separate status field.
func toolFailed(content string) bool {
	code, ok := exitCode(content)
	return ok && code != 0
}

var exitLine = regexp.MustCompile(`(?i)Exit code:\s*(-?\d+)`)

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

func spanID(id string, node int64) string {
	return id + "/node-" + strconv.FormatInt(node, 10)
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

func boolText(value bool) string {
	if value {
		return "true"
	}
	return ""
}
