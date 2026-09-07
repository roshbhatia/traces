package session

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/roshbhatia/traces/internal/otlp"
)

// Message is one user-visible assistant message from an activity source.
type Message struct {
	ID      string
	Session string
	At      time.Time
	Text    string
}

// AssistantMessages selects one exact native session and merges the assistant
// messages its providers reported. Provider prefixes are deliberately ignored:
// the normalized event and exact session identity are the shared contract.
func AssistantMessages(batch otlp.Batch, exactSession string) []Message {
	type candidate struct {
		Message
		identity  string
		canonical string
		order     int
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
		canonical := canonicalMessage(text)
		candidates = append(candidates, candidate{
			Message:  Message{Session: exactSession, At: record.At, Text: text},
			identity: identity, canonical: canonical, order: order,
		})
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].At.Equal(candidates[right].At) {
			if candidates[left].canonical != candidates[right].canonical {
				return candidates[left].canonical < candidates[right].canonical
			}
			if candidates[left].identity != candidates[right].identity {
				return candidates[left].identity < candidates[right].identity
			}
			if candidates[left].Text != candidates[right].Text {
				return candidates[left].Text < candidates[right].Text
			}
			return candidates[left].order < candidates[right].order
		}
		return candidates[left].At.Before(candidates[right].At)
	})

	seenIdentity := map[string]bool{}
	recentText := map[string]time.Time{}
	messages := make([]Message, 0, len(candidates))
	for _, one := range candidates {
		plain := one.canonical
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
		one.ID = messageID(exactSession, one.identity, one.At, plain)
		messages = append(messages, one.Message)
	}
	return messages
}

func messageID(exactSession, nativeIdentity string, at time.Time, canonical string) string {
	identity := nativeIdentity
	if identity == "" {
		identity = at.UTC().Format(time.RFC3339Nano)
	}
	digest := sha256.Sum256([]byte(exactSession + "\x00" + identity + "\x00" + canonical))
	return "msg_" + hex.EncodeToString(digest[:])
}

func assistantEvent(event string) bool {
	return event == "assistant" || strings.HasSuffix(event, ".assistant")
}

func canonicalMessage(text string) string {
	plain := strings.Map(func(value rune) rune {
		switch {
		case value == '\n' || value == '\t':
			return value
		case value < 0x20 || value == 0x7f || (value >= 0x80 && value <= 0x9f):
			return -1
		default:
			return value
		}
	}, ansi.Strip(text))
	return strings.Join(strings.Fields(plain), " ")
}
