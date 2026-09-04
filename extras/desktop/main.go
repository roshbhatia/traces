// traces-provider-desktop adapts optional host clipboard and document actions.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type check struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type report struct {
	Checks []check `json:"checks"`
}

type command struct {
	name string
	args []string
}

func main() {
	action := flag.String("action", "", "provider action")
	path := flag.String("path", "", "input file")
	flag.Parse()

	switch *action {
	case "validate":
		writeReport()
	case "clipboard":
		runClipboard(*path)
	case "document":
		runDocument(*path)
	default:
		fail("unknown action %q", *action)
	}
}

func writeReport() {
	checks := []check{
		capabilityCheck("clipboard.write", clipboardCommand()),
		capabilityCheck("document.open", documentCommand()),
	}
	if err := json.NewEncoder(os.Stdout).Encode(report{Checks: checks}); err != nil {
		fail("encode validation report: %v", err)
	}
}

func capabilityCheck(name string, candidate command) check {
	if candidate.name == "" {
		return check{Kind: "capability", Name: name, Status: "failed", Message: "no host command found"}
	}
	return check{Kind: "capability", Name: name, Status: "ok", Message: candidate.name}
}

func runClipboard(path string) {
	file, err := os.Open(path)
	if err != nil {
		fail("open clipboard input: %v", err)
	}
	defer func() { _ = file.Close() }()
	candidate := clipboardCommand()
	if candidate.name == "" {
		fail("no clipboard command found")
	}
	process := exec.Command(candidate.name, candidate.args...)
	process.Stdin = file
	process.Stdout = io.Discard
	process.Stderr = os.Stderr
	if err := process.Run(); err != nil {
		fail("clipboard command: %v", err)
	}
}

func runDocument(path string) {
	candidate := documentCommand()
	if candidate.name == "" {
		fail("no document command found")
	}
	process := exec.Command(candidate.name, append(candidate.args, path)...)
	process.Stdin = os.Stdin
	process.Stdout = os.Stdout
	process.Stderr = os.Stderr
	if err := process.Run(); err != nil {
		fail("document command: %v", err)
	}
}

func clipboardCommand() command {
	return clipboardCommandFor(runtime.GOOS, os.Getenv, resolve)

}

func clipboardCommandFor(platform string, getenv func(string) string, lookup func(string) string) command {
	if platform == "darwin" {
		return firstCommandWith(lookup, command{name: "/usr/bin/pbcopy"}, command{name: "pbcopy"})
	}
	if getenv("WAYLAND_DISPLAY") != "" {
		if candidate := firstCommandWith(lookup, command{name: "wl-copy"}); candidate.name != "" {
			return candidate
		}
	}
	if getenv("DISPLAY") != "" {
		if candidate := firstCommandWith(
			lookup,
			command{name: "xclip", args: []string{"-selection", "clipboard"}},
			command{name: "xsel", args: []string{"-ib"}},
		); candidate.name != "" {
			return candidate
		}
	}
	return firstCommandWith(
		lookup,
		command{name: "wl-copy"},
		command{name: "xclip", args: []string{"-selection", "clipboard"}},
		command{name: "xsel", args: []string{"-ib"}},
	)
}

func documentCommand() command {
	for _, variable := range []string{"VISUAL", "EDITOR"} {
		if fields := strings.Fields(os.Getenv(variable)); len(fields) > 0 {
			if found := resolve(fields[0]); found != "" {
				return command{name: found, args: fields[1:]}
			}
		}
	}
	if runtime.GOOS == "darwin" {
		return firstCommand(command{name: "/usr/bin/open"}, command{name: "open"})
	}
	return firstCommand(command{name: "xdg-open"})
}

func firstCommand(candidates ...command) command {
	return firstCommandWith(resolve, candidates...)
}

func firstCommandWith(lookup func(string) string, candidates ...command) command {
	for _, candidate := range candidates {
		if found := lookup(candidate.name); found != "" {
			candidate.name = found
			return candidate
		}
	}
	return command{}
}

func resolve(name string) string {
	found, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return found
}

func fail(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
