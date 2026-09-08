package ui

import (
	"strings"
	"testing"

	"github.com/roshbhatia/go-utils/cell"
)

func TestBindingRegistryGeneratesEveryHelpSurface(t *testing.T) {
	seen := map[string]bool{}
	for _, binding := range keyCatalog.Bindings() {
		if binding.ID == "" || len(binding.Keys) == 0 || binding.Description == "" {
			t.Fatalf("incomplete binding: %#v", binding)
		}
		if seen[binding.ID] {
			t.Fatalf("duplicate binding id: %s", binding.ID)
		}
		seen[binding.ID] = true
	}

	help := strings.Join(helpLines(120), "\n")
	for _, binding := range helpBindings() {
		keys := binding.Display
		if keys == "" {
			keys = strings.Join(binding.Keys, " / ")
		}
		if !strings.Contains(help, keys) {
			t.Errorf("help omits %s", binding.ID)
		}
	}
	for _, id := range []string{"leader-follow", "leader-inspector", "leader-edit", "help"} {
		binding := bindingByID(id)
		keys := binding.Display
		if keys == "" {
			keys = strings.Join(binding.Keys, " / ")
		}
		if !strings.Contains(leaderHints(false), strings.TrimPrefix(keys, "<space> ")) {
			t.Errorf("leader bar omits %s", id)
		}
	}
	if got := bindingHint("tab"); got != "tab / shift+tab tabs" {
		t.Fatalf("tab hint = %q", got)
	}
	if _, matched, err := keyCatalog.Match("zR", nil); err != nil || !matched {
		t.Fatalf("case-sensitive key did not match: matched=%v err=%v", matched, err)
	}
	if _, matched, err := keyCatalog.Match("zr", nil); err != nil || matched {
		t.Fatalf("lowercase key matched zR: matched=%v err=%v", matched, err)
	}
}

func TestTerminalControlRepliesNeverBecomeKeys(t *testing.T) {
	for _, reply := range []string{
		"11;rgb:2424/2727/3a3a",
		"]10;rgb:ffff/ffff/ffff",
		">|WezTerm 0-unstable;OK",
		"alt+]",
	} {
		if !isTerminalReply(reply) {
			t.Errorf("terminal reply was treated as a key: %q", reply)
		}
	}
	if isTerminalReply("G") {
		t.Fatal("vim motion was treated as a terminal reply")
	}
}

func TestASCIIModeKeepsItsThreeCellEllipsis(t *testing.T) {
	previous := gl
	gl = asciiGlyphs
	t.Cleanup(func() { gl = previous })

	for name, got := range map[string]string{
		"fit":       fit("abcdefgh", 6),
		"right fit": rightFit("abcdefgh", 6),
		"word clip": clipWord("alpha beta", 8),
	} {
		if strings.Contains(got, cell.Ellipsis) {
			t.Errorf("%s used Unicode ellipsis: %q", name, got)
		}
		if !strings.HasSuffix(got, asciiGlyphs.ell) {
			t.Errorf("%s omitted ASCII ellipsis: %q", name, got)
		}
	}
	if got := cell.Width(fit("abcdefgh", 6)); got != 6 {
		t.Fatalf("fit width = %d, want 6", got)
	}
	if got := cell.Width(rightFit("abcdefgh", 6)); got != 6 {
		t.Fatalf("right-fit width = %d, want 6", got)
	}
	if got := cell.Width(clipWord("alpha beta", 8)); got != 8 {
		t.Fatalf("word-clip width = %d, want 8", got)
	}
}
