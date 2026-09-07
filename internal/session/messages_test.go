package session

import (
	"slices"
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

func TestAssistantMessagesStableIdentityIgnoresProviderOrderAndControls(t *testing.T) {
	base := time.Unix(100, 123)
	records := []otlp.Record{
		{Event: "one.assistant", Session: "wanted", At: base, Body: "same\x1b]52;c;ignored\a", Attrs: map[string]string{"request_id": "one"}},
		{Event: "two.assistant", Session: "wanted", At: base, Body: "same", Attrs: map[string]string{"request_id": "copy"}},
		{Event: "one.assistant", Session: "wanted", At: base.Add(4 * time.Second), Body: "same", Attrs: map[string]string{"request_id": "repeat"}},
		{Event: "one.assistant", Session: "wanted-child", At: base, Body: "child", Attrs: map[string]string{"request_id": "child"}},
	}
	first := AssistantMessages(otlp.Batch{Records: records}, "wanted")
	slices.Reverse(records)
	second := AssistantMessages(otlp.Batch{Records: records}, "wanted")
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("messages = %#v / %#v", first, second)
	}
	for index := range first {
		if first[index].ID == "" || first[index].ID != second[index].ID {
			t.Fatalf("message IDs = %q / %q", first[index].ID, second[index].ID)
		}
		if first[index].Session != "wanted" || second[index].Session != "wanted" {
			t.Fatalf("session identity = %q / %q", first[index].Session, second[index].Session)
		}
	}
	if first[0].ID == first[1].ID {
		t.Fatalf("genuine repeated reply reused ID %q", first[0].ID)
	}
}

func TestAssistantMessagesIdentitySurvivesOverlappingWindows(t *testing.T) {
	base := time.Unix(100, 123)
	firstRecord := otlp.Record{
		Event: "source.assistant", Session: "wanted", At: base, Body: "same",
		Attrs: map[string]string{"request_id": "first-native-reply"},
	}
	first := AssistantMessages(otlp.Batch{Records: []otlp.Record{firstRecord}}, "wanted")
	overlap := AssistantMessages(otlp.Batch{Records: []otlp.Record{
		firstRecord,
		{
			Event: "source.assistant", Session: "wanted", At: base.Add(time.Minute), Body: "same",
			Attrs: map[string]string{"request_id": "later-native-reply"},
		},
	}}, "wanted")
	if len(first) != 1 || len(overlap) != 2 {
		t.Fatalf("messages = %#v / %#v", first, overlap)
	}
	if first[0].ID != overlap[0].ID {
		t.Fatalf("overlapping reply ID changed: %q / %q", first[0].ID, overlap[0].ID)
	}
	if overlap[0].ID == overlap[1].ID {
		t.Fatalf("separate native replies share ID %q", overlap[0].ID)
	}

	withoutNativeID := func(at time.Time) string {
		messages := AssistantMessages(otlp.Batch{Records: []otlp.Record{
			{Event: "source.assistant", Session: "wanted", At: at, Body: "same"},
		}}, "wanted")
		return messages[0].ID
	}
	if withoutNativeID(base) == withoutNativeID(base.Add(time.Minute)) {
		t.Fatal("timestamp fallback reused an ID across separate windows")
	}
}
