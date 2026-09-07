package ui

import (
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/roshbhatia/traces/internal/session"
)

// MessageOutputLimit bounds the non-interactive message stream consumed by
// terminal panes and provider commands.
const MessageOutputLimit = 64 << 10

const messageRecordVersion = "traces.message/v1"

type messageRecord struct {
	Version   string `json:"version"`
	ID        string `json:"id"`
	Session   string `json:"session"`
	Timestamp string `json:"timestamp"`
	Body      string `json:"body"`
	Truncated bool   `json:"truncated,omitempty"`
}

// PrintMessages writes the newest assistant messages in chronological order.
// It adds no metadata, prompts, reasoning, or tool output. A boundary message
// is tailed with an omission marker when it alone exceeds the output budget.
func PrintMessages(out io.Writer, messages []session.Message, color string) {
	parts := make([]string, 0, len(messages))
	used := 0
	contentLimit := MessageOutputLimit - 1 // Reserve the final newline.
	for index := len(messages) - 1; index >= 0; index-- {
		text := messageColor(messages[index].Text, color)
		text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
		if text == "" {
			continue
		}
		separator := 0
		if len(parts) > 0 {
			separator = 2
		}
		remaining := contentLimit - used - separator
		if remaining <= 0 {
			break
		}
		if len(text) > remaining {
			text = truncateMessage(text, remaining, color == "always")
		}
		if text == "" {
			break
		}
		parts = append(parts, text)
		used += separator + len(text)
	}
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	_, _ = io.WriteString(out, strings.Join(parts, "\n\n"))
	if len(parts) > 0 {
		_, _ = io.WriteString(out, "\n")
	}
}

// PrintMessagesJSONL writes complete, bounded message records for another
// process. Bodies are plain text: terminal controls never cross the protocol.
func PrintMessagesJSONL(out io.Writer, messages []session.Message) error {
	lines := make([][]byte, 0, len(messages))
	used := 0
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		record := messageRecord{
			Version:   messageRecordVersion,
			ID:        message.ID,
			Session:   message.Session,
			Timestamp: message.At.UTC().Format(time.RFC3339Nano),
			Body:      strings.TrimSpace(strings.ReplaceAll(messageColor(message.Text, "never"), "\r\n", "\n")),
		}
		if record.Body == "" {
			continue
		}
		remaining := MessageOutputLimit - used
		line, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if len(line)+1 > remaining {
			if len(lines) > 0 {
				break
			}
			line, err = fitMessageRecord(record, remaining)
			if err != nil {
				return err
			}
			if len(line) == 0 {
				break
			}
		}
		lines = append(lines, line)
		used += len(line) + 1
	}
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}
	for _, line := range lines {
		line = append(line, '\n')
		written, err := out.Write(line)
		if err != nil {
			return err
		}
		if written != len(line) {
			return io.ErrShortWrite
		}
	}
	return nil
}

func fitMessageRecord(record messageRecord, limit int) ([]byte, error) {
	const marker = "… earlier assistant output omitted …\n"
	record.Truncated = true
	runes := []rune(record.Body)
	low, high := 0, len(runes)
	var fitted []byte
	for low <= high {
		keep := low + (high-low)/2
		record.Body = marker + strings.TrimSpace(string(runes[len(runes)-keep:]))
		line, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		if len(line)+1 <= limit {
			fitted = line
			low = keep + 1
		} else {
			high = keep - 1
		}
	}
	return fitted, nil
}

func messageColor(text, color string) string {
	if color != "always" {
		return safeControls(ansi.Strip(text), false)
	}
	colored := safeControls(sgrOnly(text), true)
	if strings.Contains(colored, "\x1b[") && !strings.HasSuffix(strings.TrimSpace(colored), "\x1b[0m") {
		colored += "\x1b[0m"
	}
	return colored
}

func safeControls(text string, allowEscape bool) string {
	return strings.Map(func(value rune) rune {
		switch {
		case value == '\n' || value == '\t':
			return value
		case allowEscape && value == 0x1b:
			return value
		case value < 0x20 || value == 0x7f || (value >= 0x80 && value <= 0x9f):
			return -1
		default:
			return value
		}
	}, text)
}

// sgrOnly keeps terminal palette colors and drops control sequences that can
// rename a window, query the terminal, or write to the clipboard.
func sgrOnly(text string) string {
	var out strings.Builder
	for at := 0; at < len(text); {
		if text[at] != 0x1b {
			out.WriteByte(text[at])
			at++
			continue
		}
		start := at
		at++
		if at >= len(text) {
			continue
		}
		if text[at] == ']' {
			at++
			for at < len(text) && text[at] != '\a' && !(text[at] == 0x1b && at+1 < len(text) && text[at+1] == '\\') {
				at++
			}
			if at < len(text) && text[at] == '\a' {
				at++
			} else if at+1 < len(text) {
				at += 2
			}
			continue
		}
		if text[at] != '[' {
			at++
			continue
		}
		at++
		for at < len(text) && (text[at] < 0x40 || text[at] > 0x7e) {
			at++
		}
		if at < len(text) {
			at++
			if text[at-1] == 'm' && validSGR(text[start+2:at-1]) {
				out.WriteString(text[start:at])
			}
		}
	}
	return out.String()
}

func validSGR(parameters string) bool {
	for _, value := range parameters {
		if (value < '0' || value > '9') && value != ';' && value != ':' {
			return false
		}
	}
	return true
}

func truncateMessage(text string, limit int, colored bool) string {
	const marker = "… earlier assistant output omitted …\n"
	if limit <= len(marker) {
		return ""
	}
	if colored {
		text = ansi.Strip(text)
	}
	cut := len(text) - (limit - len(marker))
	for cut < len(text) && !utf8.RuneStart(text[cut]) {
		cut++
	}
	return marker + strings.TrimSpace(text[cut:])
}
