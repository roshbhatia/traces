// Package source reads spans from the collector and external provider commands.
//
// A provider is any executable that prints newline delimited JSON on stdout and
// exits. One line is one span:
//
//	{"traceId":"…","spanId":"…","parentId":"…","name":"…",
//	 "startUnixNano":"…","endUnixNano":"…",
//	 "attrs":{"service.name":"…","session.id":"…"}}
//
// Only traceId, spanId, name and the two stamps are required. service and
// session come off the attributes when the line omits them, so a provider that
// forwards the attributes it already has needs no extra fields.
//
// A line carrying an event is a log record instead, which is how a harness
// reports what it cannot put on a span:
//
//	{"event":"user_prompt","startUnixNano":"…",
//	 "attrs":{"service.name":"…","session.id":"…","prompt":"…"}}
//
// A line traces cannot parse is skipped rather than fatal, because a provider
// that prints a warning should not take the view down with it.
//
// traces runs the provider once per poll with a --since window, and deduplicates
// by span id. A provider is therefore stateless: it answers "which spans ended
// in the last N", and traces decides what is new. That suits a source that has to
// be queried, which is the case this exists for.
//
// TRACES_PROVIDER takes a comma-separated list. The provider output is one
// shared contract, so any source can add spans, messages, edits, or events
// without changing the UI.
package source

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/roshbhatia/traces/internal/otlp"
)

// Env names the provider without a flag, so a machine can carry the choice in
// its own configuration rather than in every command line.
const Env = "TRACES_PROVIDER"

// A name resolves to this prefix on PATH. A value holding a separator is taken
// as the path itself, which is what a provider outside PATH needs.
const prefix = "traces-"

type Provider struct {
	// Command preserves the configured executable and argument boundaries.
	Command []string
	// Name is what the caller asked for, for error text and the header.
	Name string
	// Session narrows the read when the provider can do it. traces resolves a
	// prefix itself, so a provider may ignore this.
	Session string
	// Directory lets providers find workspace-scoped sources such as Git commits.
	Directory string
	// harnesses are the services this source answers for. It is empty on a
	// source the caller named directly, which is a deliberate escape hatch and
	// is never scoped.
	harnesses []string
}

// Resolve picks the sources to read. An explicit ask, from the flag or from the
// environment, is taken as given and never scoped: it is the escape hatch for a
// one-off read. With no ask, the per-harness table decides, and a source whose
// harness the reader filtered out is not fetched at all.
//
// The collector file is read either way. Only one harness on a machine usually
// needs a source beyond it.
func Resolve(ask, service string, settings Settings) ([]*Provider, error) {
	if ask == "" {
		ask = strings.TrimSpace(os.Getenv(Env))
	}
	if ask == "" {
		return declared(service, settings)
	}
	out := []*Provider{}
	seen := map[string]bool{}
	for _, one := range strings.Split(ask, ",") {
		one = strings.TrimSpace(one)
		// A trailing comma, or a variable set to the empty string, is a list of
		// nothing rather than a provider named "".
		if one == "" || seen[one] {
			continue
		}
		seen[one] = true
		found, err := resolveOne(one, settings)
		if err != nil {
			return nil, err
		}
		out = append(out, found)
	}
	return out, nil
}

// declared resolves the config table. A source the table names but the machine
// does not carry is dropped with a warning rather than failing the read: the
// table is shared configuration, and a harness that is not installed here is
// the normal case rather than an error.
func declared(service string, settings Settings) ([]*Provider, error) {
	out := []*Provider{}
	for _, one := range wanted(settings.Sources, service) {
		found, err := resolveOne(one.Name, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "traces: skipped %v\n", err)
			continue
		}
		found.harnesses = one.harnesses
		out = append(out, found)
	}
	return out, nil
}

func resolveOne(ask string, settings Settings) (*Provider, error) {
	manifest, configured := settings.Providers[ask]
	if configured && !slices.Contains(manifest.Capabilities, "activity") {
		return nil, fmt.Errorf("provider %q does not advertise activity", ask)
	}
	command := manifest.Command
	if len(command) == 0 {
		binary := ask
		if !strings.ContainsRune(ask, filepath.Separator) {
			binary = prefix + ask
		}
		command = []string{binary}
	}
	found, err := exec.LookPath(command[0])
	if err != nil {
		return nil, fmt.Errorf("provider %q: %w", ask, err)
	}
	command = append([]string{found}, command[1:]...)
	return &Provider{Command: command, Name: ask}, nil
}

// Fetch runs the provider once over the window ending now.
func (p Provider) Fetch(ctx context.Context, window time.Duration) (otlp.Batch, error) {
	args := append([]string{}, p.Command[1:]...)
	args = append(args, "--since", window.Round(time.Second).String())
	if p.Session != "" {
		args = append(args, "--session", p.Session)
	}
	cmd := exec.CommandContext(ctx, p.Command[0], args...)
	// Environment variables add context without breaking existing provider flags.
	cmd.Env = append(os.Environ(), "TRACES_SESSION="+p.Session, "TRACES_DIRECTORY="+p.Directory)
	stderr := &strings.Builder{}
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if err != nil {
		return otlp.Batch{}, fmt.Errorf("%s: %w: %s", p.Name, err, strings.TrimSpace(stderr.String()))
	}
	return Decode(out), nil
}

// line is the shape a provider prints. It is deliberately flatter than the OTLP
// wire format, because a provider is often a shell script and the keyValue
// lists are what makes that painful to emit.
type line struct {
	// event marks the line as a log record rather than a span. A harness puts
	// on a log what it cannot put on a span, and the prompt is the one traces
	// needs.
	Event    string            `json:"event,omitempty"`
	TraceID  string            `json:"traceId,omitempty"`
	SpanID   string            `json:"spanId,omitempty"`
	ParentID string            `json:"parentId,omitempty"`
	Name     string            `json:"name,omitempty"`
	Service  string            `json:"service,omitempty"`
	Session  string            `json:"session,omitempty"`
	Start    string            `json:"startUnixNano,omitempty"`
	End      string            `json:"endUnixNano,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	Failed   bool              `json:"failed,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// Encode writes a batch in the same shape a provider prints, so traces's own
// output is valid provider input. `traces --json | jq ... | traces` is the loop
// that makes traces a pipe stage rather than an application.
func Encode(w io.Writer, batch otlp.Batch) error {
	enc := json.NewEncoder(w)
	for _, one := range batch.Spans {
		if err := enc.Encode(line{
			TraceID:  one.TraceID,
			SpanID:   one.SpanID,
			ParentID: one.ParentID,
			Name:     one.Name,
			Service:  one.Service,
			Session:  one.Session,
			Start:    stampOf(one.Start),
			End:      stampOf(one.End),
			Attrs:    one.Attrs,
			Failed:   one.Failed,
			Error:    one.Error,
		}); err != nil {
			return err
		}
	}
	for _, one := range batch.Records {
		if err := enc.Encode(line{
			TraceID: one.TraceID,
			SpanID:  one.SpanID,
			Event:   one.Event,
			Name:    one.Body,
			Service: one.Service,
			Session: one.Session,
			Start:   stampOf(one.At),
			Attrs:   one.Attrs,
		}); err != nil {
			return err
		}
	}
	return nil
}

func stampOf(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return strconv.FormatInt(at.UnixNano(), 10)
}

// DecodeAny reads either shape. A line naming resourceSpans is one OTLP export
// request, which is what the collector's file holds; anything else is one flat
// span or record. Sniffing beats a flag: a reader piping a file in should not
// have to say which file it is.
func DecodeAny(blob []byte) otlp.Batch {
	out := otlp.Batch{}
	rows := bufio.NewScanner(bytes.NewReader(blob))
	rows.Buffer(make([]byte, 1<<20), 1<<26)
	for rows.Scan() {
		text := strings.TrimSpace(rows.Text())
		if text == "" || text[0] != '{' {
			continue
		}
		var one otlp.Batch
		if strings.Contains(text, `"resourceSpans"`) || strings.Contains(text, `"resourceLogs"`) {
			one = otlp.Decode([]byte(text))
		} else {
			one = Decode([]byte(text))
		}
		out.Spans = append(out.Spans, one.Spans...)
		out.Records = append(out.Records, one.Records...)
	}
	return out
}

func Decode(blob []byte) otlp.Batch {
	out := otlp.Batch{}
	rows := bufio.NewScanner(bytes.NewReader(blob))
	rows.Buffer(make([]byte, 1<<20), 1<<26)
	for rows.Scan() {
		text := strings.TrimSpace(rows.Text())
		if text == "" || text[0] != '{' {
			continue
		}
		var one line
		if json.Unmarshal([]byte(text), &one) != nil {
			continue
		}
		attrs := one.Attrs
		if attrs == nil {
			attrs = map[string]string{}
		}
		if one.Event != "" {
			out.Records = append(out.Records, otlp.Record{
				TraceID: one.TraceID,
				SpanID:  one.SpanID,
				Event:   one.Event,
				Body:    one.Name,
				Service: orAttr(one.Service, attrs, "service.name"),
				Session: sessionOf(one.Session, attrs),
				At:      otlp.Stamp(one.Start),
				Attrs:   attrs,
			})
			continue
		}
		if one.SpanID == "" {
			continue
		}
		span := otlp.Span{
			TraceID:  one.TraceID,
			SpanID:   one.SpanID,
			ParentID: one.ParentID,
			Name:     one.Name,
			Service:  orAttr(one.Service, attrs, "service.name"),
			Session:  sessionOf(one.Session, attrs),
			Start:    otlp.Stamp(one.Start),
			End:      otlp.Stamp(one.End),
			Attrs:    attrs,
			Failed:   one.Failed,
			Error:    one.Error,
		}
		if attrs["success"] == "false" || attrs["error"] != "" {
			span.Failed = true
			if span.Error == "" {
				span.Error = attrs["error"]
			}
		}
		out.Spans = append(out.Spans, span)
	}
	return out
}

func orAttr(given string, attrs map[string]string, key string) string {
	if given != "" {
		return given
	}
	return attrs[key]
}

func sessionOf(given string, attrs map[string]string) string {
	if given != "" {
		return given
	}
	for _, key := range []string{"session.id", "conversation.id", "thread_id", "ai.telemetry.metadata.sessionId"} {
		if attrs[key] != "" {
			return attrs[key]
		}
	}
	return ""
}

// Follow polls the provider and sends only the spans it has not sent before.
// The window overlaps every poll by lag, because a span is written when it ends
// and a queried source indexes it a moment later; the span id set is what keeps
// the overlap from arriving twice.
func Follow(p Provider, every, back, lag time.Duration, out chan<- otlp.Batch, stop <-chan struct{}) {
	defer close(out)

	seen := map[string]bool{}
	window := back
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		read, err := p.Fetch(ctx, window)
		cancel()

		if err == nil {
			batch := otlp.Batch{}
			for _, one := range read.Spans {
				if seen[one.SpanID] {
					continue
				}
				seen[one.SpanID] = true
				batch.Spans = append(batch.Spans, one)
			}
			// Join IDs separate tool results that share one millisecond.
			for _, one := range read.Records {
				joinID := first(one.SpanID, one.Attrs["request_id"], one.Attrs["tool_use_id"])
				key := one.Session + "/" + one.Event + "/" + joinID + "/" + one.At.Format(time.RFC3339Nano)
				if seen[key] {
					continue
				}
				seen[key] = true
				batch.Records = append(batch.Records, one)
			}
			if !batch.Empty() {
				select {
				case out <- batch:
				case <-stop:
					return
				}
			}
			// The first read covers the whole history the caller asked for.
			// Every read after it only has to cover the poll plus the lag.
			window = every + lag
		}

		select {
		case <-stop:
			return
		case <-time.After(every):
		}
	}
}

func first(parts ...string) string {
	for _, one := range parts {
		if one != "" {
			return one
		}
	}
	return ""
}
