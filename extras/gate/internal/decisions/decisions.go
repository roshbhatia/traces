// Package decisions reads the gate hook dispatcher's decision log.
//
// gate appends one JSON object per hook call: which providers answered, what
// they said, and what the harness was finally told. Each line becomes one span
// so a denied or blocked call is a row in the session it interrupted, beside
// the tool calls that did run.
//
// The record shape belongs to gate and is decoded into a local struct here.
// Importing gate would make every traces provider closure carry it, and the
// two tools are meant to know each other only through this file on disk.
package decisions

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/roshbhatia/go-utils/xdg"
	"github.com/roshbhatia/traces/internal/otlp"
)

// Name is the span traces draws for a hook verdict. agent.note is what the
// harness said outside the conversation, which is exactly what a decision is.
const Name = "agent.note"

// Source marks these spans as reader-supplied so the runtime exporter's own
// spans cannot bury them.
const Source = "gate-decisions"

// A decision line holds a clipped message and nothing larger, so the scanner
// needs no room beyond one comfortable line.
const maxLine = 1 << 20

// Options is one read of the log.
type Options struct {
	// Window bounds the read by decision time. Exact ignores it, because a
	// caller naming one session wants that whole session however old it is.
	Window    time.Duration
	Session   string
	Exact     bool
	Directory string
	// All keeps the decisions that changed nothing. A pass is the answer for
	// every call gate had no opinion about, which is nearly all of them.
	All bool
}

// Path resolves the decision log. gate writes under the state home, and a
// caller that moved it says so with --file.
func Path(override string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	state, err := xdg.StateHome()
	if err != nil {
		return ""
	}
	return filepath.Join(state, "gate", "decisions.jsonl")
}

// Read turns every matching line into a span. A missing log is an empty batch:
// gate may simply not have run yet, and that is not a failure to report.
func Read(path string, options Options) otlp.Batch {
	out := otlp.Batch{}
	if path == "" {
		return out
	}
	file, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = file.Close() }()

	cutoff := time.Now().Add(-options.Window)
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 0, 64<<10), maxLine)
	for scan.Scan() {
		text := strings.TrimSpace(scan.Text())
		if text == "" {
			continue
		}
		var one record
		// A line gate half wrote, or wrote in a shape this reader predates, is
		// skipped rather than fatal: the rest of the log is still the truth.
		if json.Unmarshal([]byte(text), &one) != nil {
			continue
		}
		if !one.keep(options, cutoff) {
			continue
		}
		out.Spans = append(out.Spans, one.span(text))
	}
	sort.SliceStable(out.Spans, func(a, b int) bool {
		return out.Spans[a].Start.Before(out.Spans[b].Start)
	})
	return out
}

// record is one hook call as gate writes it.
type record struct {
	Ts      time.Time `json:"ts"`
	Harness string    `json:"harness"`
	Event   string    `json:"event"`
	Session string    `json:"session"`
	// Fields carries whatever identity gate's config asked the line to hold.
	Fields    map[string]string `json:"fields"`
	Agent     string            `json:"agent"`
	Cwd       string            `json:"cwd"`
	Tool      string            `json:"tool"`
	Final     string            `json:"final"`
	Ms        int64             `json:"ms"`
	Decisions []decision        `json:"decisions"`
}

// decision is one provider's answer inside a call.
type decision struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
	Ms       int64  `json:"ms"`
	Message  string `json:"message"`
	Error    string `json:"error"`
}

func (r record) keep(options Options, cutoff time.Time) bool {
	if r.Final == "pass" && !options.All {
		return false
	}
	if !options.Exact && r.Ts.Before(cutoff) {
		return false
	}
	if options.Directory != "" && r.Cwd != options.Directory {
		return false
	}
	return r.matches(options.Session, options.Exact)
}

// matches compares against every identity the line carries. A caller may know
// the harness session or the orc one and cannot know which gate was told to
// log, so either identity answers for the line.
func (r record) matches(session string, exact bool) bool {
	if session == "" {
		return true
	}
	for _, id := range r.identities() {
		if exact && id == session {
			return true
		}
		if !exact && strings.HasPrefix(id, session) {
			return true
		}
	}
	return false
}

func (r record) identities() []string {
	out := []string{}
	for _, id := range []string{r.Session, r.Fields["orc_session"]} {
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func (r record) span(raw string) otlp.Span {
	// gate numbers no line, so the line's own content is its identity. Traces
	// deduplicates by span id, and a hash is what survives a re-read.
	sum := sha256.Sum256([]byte(raw))
	id := hex.EncodeToString(sum[:8])

	session := ""
	if ids := r.identities(); len(ids) > 0 {
		session = ids[0]
	}
	trace := session
	if trace == "" {
		trace = id
	}
	failed := r.Final == "deny" || r.Final == "block"
	reason := r.reason()

	// note.kind is the row label and note.text the line under it. The label
	// carries the summary because a root row prints no second line.
	attrs := map[string]string{
		"service.name":  r.service(),
		"traces.view":   "activity",
		"traces.source": Source,
		"note.kind":     r.summary(),
		"gate.event":    r.Event,
		"gate.final":    r.Final,
		"gate.harness":  r.Harness,
		"gate.duration": strconv.FormatInt(r.Ms, 10) + "ms",
	}
	if reason != "" {
		attrs["note.text"] = reason
	}
	if failed {
		attrs["note.level"] = "error"
	}
	if r.Cwd != "" {
		attrs["cwd"] = r.Cwd
	}
	if r.Tool != "" {
		attrs["tool_name"] = r.Tool
	}
	if r.Agent != "" {
		attrs["agent.name"] = r.Agent
	}
	if one, ok := r.deciding(); ok {
		attrs["gate.provider"] = one.Provider
		if one.Error != "" {
			attrs["gate.error"] = one.Error
		}
	}
	for key, value := range r.Fields {
		attrs["gate.fields."+key] = value
	}

	// A failed span shows Error in place of the note, so both carry the reason
	// and the row reads the same whichever branch draws it.
	fault := ""
	if failed {
		fault = reason
	}
	return otlp.Span{
		TraceID: trace,
		SpanID:  id,
		Name:    Name,
		Service: r.service(),
		Session: session,
		Start:   r.Ts,
		End:     r.Ts.Add(time.Duration(r.Ms) * time.Millisecond),
		Attrs:   attrs,
		Failed:  failed,
		Error:   fault,
	}
}

// summary names the event, the tool, and the verdict.
func (r record) summary() string {
	parts := []string{r.Event}
	if r.Tool != "" {
		parts = append(parts, r.Tool)
	}
	return strings.Join(parts, " ") + " -> " + r.Final
}

// reason is why a stopped call was stopped. The deciding provider's message is
// what the model was told, so a deny is unreadable without it. A call that ran
// has nothing to explain.
func (r record) reason() string {
	if r.Final != "deny" && r.Final != "block" {
		return ""
	}
	one, ok := r.deciding()
	if !ok {
		return ""
	}
	return strings.TrimSpace(first(one.Message, one.Error))
}

// deciding is the provider whose answer became the final one. gate runs the
// chain in order and the first matching kind is the one that ended it.
func (r record) deciding() (decision, bool) {
	for _, one := range r.Decisions {
		if one.Kind == r.Final {
			return one, true
		}
	}
	return decision{}, false
}

// service is the name the harness's own reader uses, so a decision joins that
// harness's session instead of opening one beside it. An unmapped harness keeps
// its own name.
func (r record) service() string {
	switch r.Harness {
	case "":
		return "gate"
	case "claude":
		return "claude-code"
	case "codex":
		return "codex_cli_rs"
	default:
		return r.Harness
	}
}

func first(values ...string) string {
	for _, one := range values {
		if one != "" {
			return one
		}
	}
	return ""
}
