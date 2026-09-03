package session

import (
	"testing"
	"time"

	"github.com/roshbhatia/traces/internal/otlp"
)

func TestActivityProviderReplacesRawRuntimeTree(t *testing.T) {
	now := time.Now()
	store := NewStore()
	store.Add([]otlp.Span{
		{SpanID: "raw", Name: "runtime.persist", Service: "example-agent", Session: "one", Start: now, End: now, Attrs: map[string]string{}},
		{SpanID: "turn", Name: "agent.turn", Service: "example-agent", Session: "one", Start: now, End: now, Attrs: map[string]string{"traces.view": "activity"}},
		{SpanID: "tool", ParentID: "turn", Name: "agent.tool", Service: "example-agent", Session: "one", Start: now, End: now, Attrs: map[string]string{"traces.view": "activity", "tool_name": "Shell"}},
	})

	found := store.Session("one")
	if found == nil {
		t.Fatal("session not found")
	}
	if got, want := found.ViewCount(), 2; got != want {
		t.Fatalf("ViewCount() = %d, want %d", got, want)
	}
	if len(found.Roots) != 1 || found.Roots[0].Span.SpanID != "turn" {
		t.Fatalf("roots = %#v", found.Roots)
	}
}

func TestActivityProviderAggregatesRuntimeOutcome(t *testing.T) {
	now := time.Now()
	activity := []otlp.Span{
		{
			SpanID: "turn", Name: "agent.turn", Service: "example-agent", Session: "one",
			Start: now, End: now.Add(time.Second), Attrs: map[string]string{"traces.view": "activity"},
		},
		{
			SpanID: "tool", ParentID: "turn", Name: "agent.tool", Service: "example-agent", Session: "one",
			Start: now.Add(time.Second), End: now.Add(2 * time.Second),
			Attrs: map[string]string{"traces.view": "activity", "tool_use_id": "call-1", "tool_name": "Shell"},
		},
	}
	runtime := []otlp.Span{
		{
			SpanID: "metrics", Name: "runtime.tool", Service: "example-agent", Session: "one",
			Start: now.Add(500 * time.Millisecond), End: now.Add(3 * time.Second),
			Attrs: map[string]string{"tool_use_id": "call-1", "ttft_ms": "350"},
		},
		{
			SpanID: "outcome", Name: "runtime.tool.result", Service: "example-agent", Session: "one",
			Start: now.Add(2 * time.Second), End: now.Add(4 * time.Second), Failed: true, Error: "permission denied",
			Attrs: map[string]string{"tool_use_id": "call-1", "decision": "deny"},
		},
	}
	orders := [][]otlp.Span{runtime, {runtime[1], runtime[0]}}
	for at, order := range orders {
		store := NewStore()
		store.Add(append(append([]otlp.Span{}, activity...), order...))

		found := store.Session("one")
		if found == nil || len(found.Roots) != 1 || len(found.Roots[0].Children) != 1 {
			t.Fatalf("order %d session tree = %+v", at, found)
		}
		tool := found.Roots[0].Children[0]
		if !tool.Span.Failed || tool.Span.Error != "permission denied" || tool.Note != "permission denied" {
			t.Fatalf("order %d tool outcome = %+v", at, tool)
		}
		if tool.Span.Attrs["ttft_ms"] != "350" || tool.Span.Attrs["decision"] != "deny" {
			t.Fatalf("order %d tool attrs = %v", at, tool.Span.Attrs)
		}
		if !tool.Span.Start.Equal(now.Add(500*time.Millisecond)) || !tool.Span.End.Equal(now.Add(4*time.Second)) {
			t.Fatalf("order %d tool interval = %s..%s", at, tool.Span.Start, tool.Span.End)
		}
		if len(tool.Facets) != 2 || tool.Facets[0].SpanID != "metrics" || tool.Facets[1].SpanID != "outcome" {
			t.Fatalf("order %d facets = %+v", at, tool.Facets)
		}
	}
}

func TestProviderActionsControlToolLabels(t *testing.T) {
	tests := []struct {
		name   string
		action string
		want   string
	}{
		{name: "private-edit", action: "edit", want: "Edit"},
		{name: "private-shell", action: "shell", want: "Shell"},
		{name: "private-search", action: "search", want: "Search"},
		{name: "custom_tool", action: "browse", want: "Browse"},
		{name: "unmapped", want: "unmapped"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := describe(otlp.Span{
				Name:  "agent.tool",
				Attrs: map[string]string{"tool_name": test.name, "traces.action": test.action},
			})
			if node.Label != test.want {
				t.Errorf("label = %q, want %q", node.Label, test.want)
			}
		})
	}
}

func TestNativeOperationSuffixesAssignGenericRoles(t *testing.T) {
	tests := []struct {
		name string
		want Role
	}{
		{name: "runtime.interaction", want: RoleTurn},
		{name: "runtime.llm_request", want: RoleModel},
		{name: "runtime.tool", want: RoleTool},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := describe(otlp.Span{Name: test.name, Attrs: map[string]string{}}).Role; got != test.want {
				t.Fatalf("role = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSessionsReturnsStableSnapshots(t *testing.T) {
	now := time.Now()
	store := NewStore()
	store.Add([]otlp.Span{{
		SpanID: "one", Name: "agent.turn", Service: "example-agent", Session: "run",
		Start: now, End: now, Attrs: map[string]string{"traces.view": "activity"},
	}})
	first := store.Sessions()[0]
	store.Add([]otlp.Span{{
		SpanID: "two", ParentID: "one", Name: "agent.tool", Service: "example-agent", Session: "run",
		Start: now, End: now, Attrs: map[string]string{"traces.view": "activity"},
	}})
	second := store.Sessions()[0]
	if first == second || first.Count != 1 || second.Count != 2 {
		t.Fatalf("snapshots share state: same %v, counts %d and %d", first == second, first.Count, second.Count)
	}
}
