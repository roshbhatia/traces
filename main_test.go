package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/roshbhatia/traces/internal/otlp"
)

func TestRenderProviderList(t *testing.T) {
	got, err := renderProviderList(providerListView{
		Name: "local", Description: "Read local activity", Source: "/providers/local.yaml",
		Provides: "activity.read, session.current", Command: "local-provider --json",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `local
  Read local activity
  source    /providers/local.yaml
  provides  activity.read, session.current
  command   local-provider --json
`
	if got != want {
		t.Fatalf("provider list:\n%s\nwant:\n%s", got, want)
	}
}

func TestSelectedJSONKeepsJoinedRuntimeFacets(t *testing.T) {
	now := time.Unix(1, 0)
	batch := otlp.Batch{Spans: []otlp.Span{
		{
			SpanID: "turn", Name: "agent.turn", Service: "example-agent", Session: "run-123",
			Start: now, End: now, Attrs: map[string]string{"traces.view": "activity"},
		},
		{
			SpanID: "tool", ParentID: "turn", Name: "agent.tool", Service: "example-agent", Session: "run-123",
			Start: now, End: now,
			Attrs: map[string]string{"traces.view": "activity", "tool_use_id": "call-1"},
		},
		{
			SpanID: "runtime", Name: "runtime.tool", Service: "example-agent", Session: "run-123",
			Start: now, End: now, Failed: true, Attrs: map[string]string{"tool_use_id": "call-1"},
		},
	}}

	selected := only(batch, "run-123", nil, "")
	if len(selected.Spans) != 3 {
		t.Fatalf("selected spans = %+v", selected.Spans)
	}
}

func TestFileInputSessionAliasesCanBeSelected(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "flat",
			body: compactJSON(t, `{
				"traceId": "flat-trace",
				"spanId": "flat-root",
				"name": "runtime.interaction",
				"startUnixNano": "1788278400000000000",
				"endUnixNano": "1788278401000000000",
				"attrs": {"thread_id": "run-123"}
			}`),
		},
		{
			name: "otlp",
			body: compactJSON(t, `{
				"resourceSpans": [{
					"resource": {"attributes": [{
						"key": "conversation.id",
						"value": {"stringValue": "run-123"}
					}]},
					"scopeSpans": [{"spans": [{
						"traceId": "otlp-trace",
						"spanId": "otlp-root",
						"name": "runtime.interaction",
						"startTimeUnixNano": "1788278400000000000",
						"endTimeUnixNano": "1788278401000000000"
					}]}]
				}]
			}`),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "activity.jsonl")
			if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			batch, err := (sources{path: path}).read()
			if err != nil {
				t.Fatal(err)
			}
			selected := only(batch, "run-123", nil, "")
			if len(selected.Spans) != 1 || selected.Spans[0].Session != "run-123" {
				t.Fatalf("selected batch = %+v", selected)
			}
		})
	}
}

func compactJSON(t *testing.T, value string) string {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(value)); err != nil {
		t.Fatal(err)
	}
	return compact.String()
}
