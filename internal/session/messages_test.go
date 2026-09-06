package session

import (
	"testing"
	"time"

	"github.com/roshbhatia/traces/internal/otlp"
)

func TestAssistantMessagesSelectMergeAndOrder(t *testing.T) {
	base := time.Unix(100, 0)
	batch := otlp.Batch{Records: []otlp.Record{
		{Event: "source-one.user_prompt", Session: "wanted", At: base, Body: "do the work"},
		{Event: "source-one.assistant", Session: "other", At: base, Body: "wrong session"},
		{Event: "source-one.tool_result", Session: "wanted", At: base, Body: "tool output"},
		{Event: "source-one.assistant", Session: "wanted", At: base.Add(2 * time.Second), Body: "second", Attrs: map[string]string{"request_id": "two"}},
		{Event: "source-two.assistant", Session: "wanted", At: base.Add(time.Second), Body: "\x1b[32mfirst\x1b[0m", Attrs: map[string]string{"request_id": "one"}},
		{Event: "source-three.assistant", Session: "wanted", At: base.Add(1500 * time.Millisecond), Body: "first", Attrs: map[string]string{"request_id": "provider-copy"}},
		{Event: "assistant", Session: "wanted", At: base.Add(5 * time.Second), Body: "first", Attrs: map[string]string{"request_id": "real-repeat"}},
		{Event: "source-one.assistant", Session: "wanted", At: base.Add(6 * time.Second), Body: "", Attrs: map[string]string{"thinking": "private reasoning"}},
	}}

	got := AssistantMessages(batch, "wanted")
	if len(got) != 3 {
		t.Fatalf("messages = %#v", got)
	}
	if got[0].Text != "\x1b[32mfirst\x1b[0m" || got[1].Text != "second" || got[2].Text != "first" {
		t.Fatalf("messages = %#v", got)
	}
}

func TestAssistantMessagesDeduplicatesStableNativeIdentity(t *testing.T) {
	base := time.Unix(100, 0)
	batch := otlp.Batch{Records: []otlp.Record{
		{Event: "one.assistant", Session: "wanted", At: base, Body: "same", Attrs: map[string]string{"request_id": "request"}},
		{Event: "two.assistant", Session: "wanted", At: base.Add(time.Minute), Body: "same", Attrs: map[string]string{"request_id": "request"}},
	}}
	if got := AssistantMessages(batch, "wanted"); len(got) != 1 {
		t.Fatalf("messages = %#v", got)
	}
}
