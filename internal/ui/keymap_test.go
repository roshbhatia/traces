package ui

import (
	"strings"
	"testing"
)

func TestBindingRegistryGeneratesEveryHelpSurface(t *testing.T) {
	seen := map[string]bool{}
	for _, binding := range keyBindings {
		if binding.id == "" || binding.keys == "" || binding.description == "" {
			t.Fatalf("incomplete binding: %#v", binding)
		}
		if seen[binding.id] {
			t.Fatalf("duplicate binding id: %s", binding.id)
		}
		seen[binding.id] = true
	}

	help := strings.Join(helpLines(120), "\n")
	for _, binding := range helpBindings() {
		if !strings.Contains(help, binding.keys) {
			t.Errorf("help omits %s", binding.id)
		}
	}
	for _, id := range []string{"leader-follow", "leader-inspector", "leader-edit", "help"} {
		binding := bindingByID(id)
		if !strings.Contains(leaderHints(false), strings.TrimPrefix(binding.keys, "<space> ")) {
			t.Errorf("leader bar omits %s", id)
		}
	}
}
