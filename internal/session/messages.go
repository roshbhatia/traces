package session

import (
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/roshbhatia/traces/internal/otlp"
)

// Message is one user-visible assistant message from an activity source.
type Message struct {
	At   time.Time
	Text string
}

// AssistantMessages selects one exact native session and merges the assistant
// messages its providers reported. Provider prefixes are deliberately ignored:
// the normalized event and exact session identity are the shared contract.
func AssistantMessages(batch otlp.Batch, exactSession string) []Message {
	type candidate struct {
		Message
		identity string
		order    int
	}

	var candidates []candidate
	for order, record := range batch.Records {
		if record.Session != exactSession || !assistantEvent(record.Event) {
			continue
		}
		text := strings.TrimSpace(record.Body)
		if text == "" {
			continue
		}
		identity := first(record.Attrs["request_id"], record.SpanID)
		candidates = append(candidates, candidate{
			Message: Message{At: record.At, Text: text}, identity: identity, order: order,
		})
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].At.Equal(candidates[right].At) {
			return candidates[left].order < candidates[right].order
		}
		return candidates[left].At.Before(candidates[right].At)
	})

	seenIdentity := map[string]bool{}
	recentText := map[string]time.Time{}
	messages := make([]Message, 0, len(candidates))
	for _, one := range candidates {
		plain := canonicalMessage(one.Text)
		if one.identity != "" {
			key := one.identity + "\x00" + plain
			if seenIdentity[key] {
				continue
			}
			seenIdentity[key] = true
		}
		// Two providers may stamp the same native message a fraction apart and
		// assign different transport IDs. Keep a later, genuine repetition.
		if previous, ok := recentText[plain]; ok && one.At.Sub(previous) <= 2*time.Second {
			continue
		}
		recentText[plain] = one.At
		messages = append(messages, one.Message)
	}
	return messages
}

func assistantEvent(event string) bool {
	return event == "assistant" || strings.HasSuffix(event, ".assistant")
}

func canonicalMessage(text string) string {
	return strings.Join(strings.Fields(ansi.Strip(text)), " ")
}
