package main

import "testing"

func TestClipboardCommandFollowsTheLinuxDisplay(t *testing.T) {
	available := map[string]string{
		"wl-copy": "/bin/wl-copy",
		"xclip":   "/bin/xclip",
		"xsel":    "/bin/xsel",
	}
	lookup := func(name string) string { return available[name] }
	tests := []struct {
		name        string
		environment map[string]string
		want        string
	}{
		{name: "wayland", environment: map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, want: "/bin/wl-copy"},
		{name: "x11", environment: map[string]string{"DISPLAY": ":0"}, want: "/bin/xclip"},
		{name: "headless fallback", environment: map[string]string{}, want: "/bin/wl-copy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			getenv := func(name string) string { return test.environment[name] }
			if got := clipboardCommandFor("linux", getenv, lookup); got.name != test.want {
				t.Fatalf("clipboard command = %q, want %q", got.name, test.want)
			}
		})
	}
}
