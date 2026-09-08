package ui

import (
	"strings"

	sharedkeymap "github.com/roshbhatia/go-utils/keymap"
)

var keyCatalog = sharedkeymap.Must(
	sharedkeymap.Binding{ID: "focus-inspector", Keys: []string{"ctrl+j"}, Short: "inspector", Description: "focus the inspector"},
	sharedkeymap.Binding{ID: "focus-trace", Keys: []string{"ctrl+k"}, Short: "trace", Description: "focus the trace"},
	sharedkeymap.Binding{ID: "line", Keys: []string{"j", "k", "down", "up"}, Display: "j / k", Short: "line", Description: "one line in the focused pane (the arrows do the same)"},
	sharedkeymap.Binding{ID: "page", Keys: []string{"ctrl+d", "ctrl+u", "ctrl+f", "ctrl+b"}, Display: "ctrl+d / ctrl+u", Short: "page", Description: "half page the focused pane (ctrl+f and ctrl+b page it whole)"},
	sharedkeymap.Binding{ID: "inspect-page", Keys: []string{"d", "u"}, Display: "d / u", Short: "inspector", Description: "half page the inspector without moving the focus"},
	sharedkeymap.Binding{ID: "inspect-line", Keys: []string{"ctrl+e", "ctrl+y"}, Display: "ctrl+e / ctrl+y", Description: "scroll the inspector one line, cursor unmoved"},
	sharedkeymap.Binding{ID: "ends", Keys: []string{"gg", "G"}, Display: "gg / G", Short: "ends", Description: "start or end of the focused pane (trace G resumes follow)"},
	sharedkeymap.Binding{ID: "viewport", Keys: []string{"H", "M", "L"}, Display: "H / M / L", Description: "cursor to the top, middle or bottom of the view"},
	sharedkeymap.Binding{ID: "turn", Keys: []string{"{", "}", "[t", "]t"}, Display: "{ / }", Short: "turn", Description: "previous or next turn ([t and ]t also work)"},
	sharedkeymap.Binding{ID: "match", Keys: []string{"n", "N"}, Display: "n / N", Description: "next or previous row of the current filter"},
	sharedkeymap.Binding{ID: "filter", Keys: []string{"/"}, Short: "filter", Description: "filter the tree by text (esc clears it)"},
	sharedkeymap.Binding{ID: "fold-step", Keys: []string{"h", "l"}, Display: "h / l", Description: "collapse or step out, or expand"},
	sharedkeymap.Binding{ID: "fold", Keys: []string{"za", "zo", "zc"}, Display: "za / zo / zc", Description: "toggle, open, or close the fold under the cursor"},
	sharedkeymap.Binding{ID: "fold-all", Keys: []string{"zR", "zM"}, Display: "zR / zM", Description: "open or close every fold"},
	sharedkeymap.Binding{ID: "fold-path", Keys: []string{"zx"}, Description: "close all folds, then open the path to the cursor"},
	sharedkeymap.Binding{ID: "visual", Keys: []string{"v"}, Short: "range", Description: "select a range; enter or v keeps it, and esc cancels it"},
	sharedkeymap.Binding{ID: "mark-turn", Keys: []string{"V", "enter"}, Display: "V / enter", Short: "turn", Description: "toggle the whole turn under the cursor"},
	sharedkeymap.Binding{ID: "mark-subtree", Keys: []string{"m"}, Short: "subtree", Description: "toggle the row and its whole subtree"},
	sharedkeymap.Binding{ID: "cancel", Keys: []string{"esc"}, Description: "cancel a range, or clear every mark"},
	sharedkeymap.Binding{ID: "yank", Keys: []string{"Y"}, Description: "yank the row's whole text to the clipboard"},
	sharedkeymap.Binding{ID: "edit", Keys: []string{"e"}, Description: "open the row's whole text with its document provider"},
	sharedkeymap.Binding{ID: "tab", Keys: []string{"tab", "shift+tab"}, Display: "tab / shift+tab", Short: "tabs", Description: "next or previous inspector tab"},
	sharedkeymap.Binding{ID: "resize", Keys: []string{"-", "_", "=", "+"}, Display: "- / =", Description: "move the divider (dragging it does the same)"},
	sharedkeymap.Binding{ID: "mouse", Keys: []string{"click", "wheel"}, Display: "click / wheel", Description: "select, fold, choose a tab, or scroll a pane"},
	sharedkeymap.Binding{ID: "leader-follow", Keys: []string{"<space> f"}, Short: "follow", Description: "toggle live cursor follow"},
	sharedkeymap.Binding{ID: "leader-anchor", Keys: []string{"<space> o"}, Short: "anchor", Description: "toggle the selected range anchor"},
	sharedkeymap.Binding{ID: "leader-timeline", Keys: []string{"<space> t"}, Short: "timeline", Description: "draw each row's span beside it"},
	sharedkeymap.Binding{ID: "leader-session", Keys: []string{"<space> s"}, Short: "session", Description: "choose another session"},
	sharedkeymap.Binding{ID: "leader-all", Keys: []string{"<space> a"}, Short: "all", Description: "toggle all rows"},
	sharedkeymap.Binding{ID: "leader-row", Keys: []string{"<space> m"}, Short: "one row", Description: "toggle only the current row"},
	sharedkeymap.Binding{ID: "leader-inspector", Keys: []string{"<space> i"}, Short: "inspector", Description: "toggle or dock the inspector"},
	sharedkeymap.Binding{ID: "leader-yank", Keys: []string{"<space> y"}, Short: "yank raw", Description: "yank the raw row text"},
	sharedkeymap.Binding{ID: "leader-edit", Keys: []string{"<space> e"}, Short: "edit", Description: "open the raw row text with its document provider"},
	sharedkeymap.Binding{ID: "dock-toggle", Keys: []string{"<space> i i"}, Short: "toggle", Description: "toggle the inspector", Hidden: true},
	sharedkeymap.Binding{ID: "dock-left", Keys: []string{"<space> i h"}, Short: "left", Description: "dock the inspector left", Hidden: true},
	sharedkeymap.Binding{ID: "dock-bottom", Keys: []string{"<space> i j"}, Short: "bottom", Description: "dock the inspector at the bottom", Hidden: true},
	sharedkeymap.Binding{ID: "dock-top", Keys: []string{"<space> i k"}, Short: "top", Description: "dock the inspector at the top", Hidden: true},
	sharedkeymap.Binding{ID: "dock-right", Keys: []string{"<space> i l"}, Short: "right", Description: "dock the inspector right", Hidden: true},
	sharedkeymap.Binding{ID: "command", Keys: []string{":"}, Short: "command", Description: "open the command line"},
	sharedkeymap.Binding{ID: "help", Keys: []string{"?"}, Short: "help", Description: "open this key list"},
	sharedkeymap.Binding{ID: "quit", Keys: []string{"ZZ", "q"}, Display: "ZZ / q", Short: "quit", Description: "leave traces"},
)

var leaderBindingIDs = []string{
	"leader-follow", "leader-anchor", "leader-timeline", "leader-session",
	"leader-all", "leader-row", "leader-inspector", "leader-yank", "leader-edit", "help",
}

var dockBindingIDs = []string{"dock-toggle", "dock-left", "dock-bottom", "dock-top", "dock-right"}

func bindingByID(id string) sharedkeymap.Binding {
	binding, ok := keyCatalog.Binding(id)
	if !ok {
		panic("unknown key binding: " + id)
	}
	return binding
}

func bindingHint(id string) string {
	hint, err := keyCatalog.Hint(id)
	if err != nil {
		panic(err)
	}
	return hint.String()
}

func bindingHints(ids ...string) string {
	line, err := keyCatalog.HintLine("   ", ids...)
	if err != nil {
		panic(err)
	}
	return line
}

func leaderHints(dock bool) string {
	ids := leaderBindingIDs
	if dock {
		ids = dockBindingIDs
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		binding := bindingByID(id)
		keys := binding.Display
		if keys == "" {
			keys = strings.Join(binding.Keys, " / ")
		}
		parts = append(parts, strings.TrimPrefix(keys, "<space> ")+" "+binding.Short)
	}
	return strings.Join(parts, "   ")
}

func helpBindings() []sharedkeymap.Binding {
	rows, err := keyCatalog.HelpRows(nil)
	if err != nil {
		panic(err)
	}
	out := make([]sharedkeymap.Binding, 0, len(rows))
	for _, row := range rows {
		out = append(out, bindingByID(row.ID))
	}
	return out
}
