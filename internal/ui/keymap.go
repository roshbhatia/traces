package ui

import "strings"

type keyBinding struct {
	id          string
	keys        string
	short       string
	description string
	help        bool
	leader      bool
	dock        bool
}

var keyBindings = []keyBinding{
	{id: "focus-inspector", keys: "ctrl+j", short: "inspector", description: "focus the inspector", help: true},
	{id: "focus-trace", keys: "ctrl+k", short: "trace", description: "focus the trace", help: true},
	{id: "line", keys: "j / k", short: "line", description: "one line in the focused pane (the arrows do the same)", help: true},
	{id: "page", keys: "ctrl+d / ctrl+u", short: "page", description: "half page the focused pane (ctrl+f and ctrl+b page it whole)", help: true},
	{id: "inspect-page", keys: "d / u", short: "inspector", description: "half page the inspector without moving the focus", help: true},
	{id: "inspect-line", keys: "ctrl+e / ctrl+y", description: "scroll the inspector one line, cursor unmoved", help: true},
	{id: "ends", keys: "gg / G", short: "ends", description: "start or end of the focused pane (trace G resumes follow)", help: true},
	{id: "viewport", keys: "H / M / L", description: "cursor to the top, middle or bottom of the view", help: true},
	{id: "turn", keys: "{ / }", short: "turn", description: "previous or next turn ([t and ]t also work)", help: true},
	{id: "match", keys: "n / N", description: "next or previous row of the current filter", help: true},
	{id: "filter", keys: "/", short: "filter", description: "filter the tree by text (esc clears it)", help: true},
	{id: "fold-step", keys: "h / l", description: "collapse or step out, or expand", help: true},
	{id: "fold", keys: "za / zo / zc", description: "toggle, open, or close the fold under the cursor", help: true},
	{id: "fold-all", keys: "zR / zM", description: "open or close every fold", help: true},
	{id: "fold-path", keys: "zx", description: "close all folds, then open the path to the cursor", help: true},
	{id: "visual", keys: "v", short: "range", description: "select a range; enter or v keeps it, and esc cancels it", help: true},
	{id: "mark-turn", keys: "V / enter", short: "turn", description: "toggle the whole turn under the cursor", help: true},
	{id: "mark-subtree", keys: "m", short: "subtree", description: "toggle the row and its whole subtree", help: true},
	{id: "cancel", keys: "esc", description: "cancel a range, or clear every mark", help: true},
	{id: "yank", keys: "Y", description: "yank the row's whole text to the clipboard", help: true},
	{id: "edit", keys: "e", description: "open the row's whole text with its document provider", help: true},
	{id: "tab", keys: "tab / shift+tab", short: "pane", description: "next or previous inspector tab", help: true},
	{id: "resize", keys: "- / =", description: "move the divider (dragging it does the same)", help: true},
	{id: "mouse", keys: "click / wheel", description: "select, fold, choose a tab, or scroll a pane", help: true},
	{id: "leader-follow", keys: "<space> f", short: "follow", description: "toggle live cursor follow", help: true, leader: true},
	{id: "leader-anchor", keys: "<space> o", short: "anchor", description: "toggle the selected range anchor", help: true, leader: true},
	{id: "leader-timeline", keys: "<space> t", short: "timeline", description: "draw each row's span beside it", help: true, leader: true},
	{id: "leader-session", keys: "<space> s", short: "session", description: "choose another session", help: true, leader: true},
	{id: "leader-all", keys: "<space> a", short: "all", description: "toggle all rows", help: true, leader: true},
	{id: "leader-row", keys: "<space> m", short: "one row", description: "toggle only the current row", help: true, leader: true},
	{id: "leader-inspector", keys: "<space> i", short: "inspector", description: "toggle or dock the inspector", help: true, leader: true},
	{id: "leader-yank", keys: "<space> y", short: "yank raw", description: "yank the raw row text", help: true, leader: true},
	{id: "leader-edit", keys: "<space> e", short: "edit", description: "open the raw row text with its document provider", help: true, leader: true},
	{id: "dock-toggle", keys: "<space> i i", short: "toggle", description: "toggle the inspector", dock: true},
	{id: "dock-left", keys: "<space> i h", short: "left", description: "dock the inspector left", dock: true},
	{id: "dock-bottom", keys: "<space> i j", short: "bottom", description: "dock the inspector at the bottom", dock: true},
	{id: "dock-top", keys: "<space> i k", short: "top", description: "dock the inspector at the top", dock: true},
	{id: "dock-right", keys: "<space> i l", short: "right", description: "dock the inspector right", dock: true},
	{id: "command", keys: ":", short: "command", description: "open the command line", help: true},
	{id: "help", keys: "?", short: "help", description: "open this key list", help: true, leader: true},
	{id: "quit", keys: "ZZ / q", short: "quit", description: "leave traces", help: true},
}

func bindingByID(id string) keyBinding {
	for _, binding := range keyBindings {
		if binding.id == id {
			return binding
		}
	}
	panic("unknown key binding: " + id)
}

func bindingHint(id string) string {
	binding := bindingByID(id)
	return binding.keys + " " + binding.short
}

func bindingHints(ids ...string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, bindingHint(id))
	}
	return strings.Join(parts, "   ")
}

func leaderHints(dock bool) string {
	parts := []string{}
	for _, binding := range keyBindings {
		if binding.dock != dock || (!dock && !binding.leader) {
			continue
		}
		key := strings.TrimPrefix(binding.keys, "<space> ")
		parts = append(parts, key+" "+binding.short)
	}
	return strings.Join(parts, "   ")
}

func helpBindings() []keyBinding {
	out := []keyBinding{}
	for _, binding := range keyBindings {
		if binding.help {
			out = append(out, binding)
		}
	}
	return out
}
